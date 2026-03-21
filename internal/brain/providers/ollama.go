package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/config"
	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

const (
	MaxScannerBufferSize = 10 * 1024 * 1024
	MaxImagesPerMessage  = 5
	MaxImageSize         = 10 * 1024 * 1024
)

type OllamaProvider struct {
	BaseURL     string
	Model       string
	Temperature float64
	NumCtx      int
	APIKey      string
	Client      *http.Client
}

func NewOllama(url, model string, temp float64, numCtx int, apiKey string) *OllamaProvider {
	if numCtx == 0 {
		numCtx = 8192
	}

	logger.Debug("🔧 [Ollama] Provider oluşturuluyor: Model=%s, URL=%s, NumCtx=%d", model, url, numCtx)

	return &OllamaProvider{
		BaseURL:     strings.TrimSuffix(url, "/"),
		Model:       model,
		Temperature: temp,
		NumCtx:      numCtx,
		APIKey:      apiKey,
		Client: &http.Client{
			Timeout: config.LLMStreamTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

type ollamaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Parameters  map[string]interface{} `json:"parameters"`
	} `json:"function"`
}

type ollamaRequest struct {
	Model     string                 `json:"model"`
	Messages  []ollamaMessage        `json:"messages"`
	Stream    bool                   `json:"stream"`
	Tools     []ollamaTool           `json:"tools,omitempty"`
	Options   map[string]interface{} `json:"options,omitempty"`
	KeepAlive int                    `json:"keep_alive,omitempty"`
	Think     bool                   `json:"think"`
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	Images    []string         `json:"images,omitempty"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaToolCall struct {
	Function struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	} `json:"function"`
}

type ollamaResponse struct {
	Message   ollamaMessage `json:"message"`
	Done      bool          `json:"done"`
	EvalCount int           `json:"eval_count,omitempty"`
}

func (o *OllamaProvider) Chat(ctx context.Context, history []kernel.Message, tools []kernel.Tool) (*kernel.BrainResponse, error) {
	if o == nil {
		return nil, fmt.Errorf("ollama provider nil")
	}

	if o.BaseURL == "" {
		return nil, fmt.Errorf("ollama base URL boş")
	}

	logger.Debug("🧠 [Ollama] Chat başlatılıyor: Model=%s, History=%d mesaj, Tools=%d adet", o.Model, len(history), len(tools))

	totalChars := 0
	for _, msg := range history {
		totalChars += len(msg.Content)
	}
	if totalChars > config.LLMMaxContentLength*2 {
		logger.Warn("⚠️ [Ollama] History çok büyük (%d karakter), ilk %d mesaj kullanılıyor", totalChars, len(history)/2)
		history = history[len(history)/2:]
	}

	var finalMessages []ollamaMessage
	imagePathRegex := regexp.MustCompile(`[a-zA-Z0-9_\\\/\-\.\:]+\.(png|jpg|jpeg)`)

	var combinedSystemPrompt string

	for _, msg := range history {
		if msg.Role == "system" {
			combinedSystemPrompt += msg.Content + "\n\n"
		}
	}

	lastUserMsgIndex := -1

	for _, msg := range history {
		if msg.Role == "system" {
			continue
		}

		var cleanedImages []string
		imageCount := 0
		for _, img := range msg.Images {
			if imageCount >= MaxImagesPerMessage {
				logger.Warn("⚠️ [Ollama] Görsel limiti aşıldı (%d), geri kalanlar atlandı", MaxImagesPerMessage)
				break
			}

			if idx := strings.Index(img, "base64,"); idx != -1 {
				cleanedImages = append(cleanedImages, img[idx+7:])
			} else {
				cleanedImages = append(cleanedImages, img)
			}
			imageCount++
		}

		om := ollamaMessage{
			Role:    msg.Role,
			Content: msg.Content,
			Images:  cleanedImages,
		}

		if msg.Content != "" {
			matches := imagePathRegex.FindAllString(msg.Content, -1)
			for _, match := range matches {
				if len(om.Images) >= MaxImagesPerMessage {
					break
				}

				resolvedPath := resolveMediaPath(match)

				if info, err := os.Stat(resolvedPath); err == nil && info.Size() > MaxImageSize {
					logger.Warn("⚠️ [Ollama] Görsel çok büyük (%d byte): %s", info.Size(), resolvedPath)
					continue
				}

				imgData, err := os.ReadFile(resolvedPath)
				if err == nil {
					b64Data := base64.StdEncoding.EncodeToString(imgData)
					om.Images = append(om.Images, b64Data)
					logger.Debug("🖼️ [Ollama] Görsel eklendi: %s -> %d bytes", match, len(imgData))
				}
			}
		}

		if msg.Role == "tool" {
			om.Role = "tool"
			var responseData map[string]interface{}
			if err := json.Unmarshal([]byte(msg.Content), &responseData); err != nil {
				om.Content = msg.Content
			} else {
				jsonStr, _ := json.Marshal(responseData)
				om.Content = string(jsonStr)
			}
		}

		if len(msg.ToolCalls) > 0 {
			om.Role = "assistant"
			for _, tc := range msg.ToolCalls {
				om.ToolCalls = append(om.ToolCalls, ollamaToolCall{
					Function: struct {
						Name      string                 `json:"name"`
						Arguments map[string]interface{} `json:"arguments"`
					}{Name: tc.Function, Arguments: tc.Arguments},
				})
			}
			logger.Debug("🛠️ [Ollama] Tool call eklendi: %d adet", len(msg.ToolCalls))
		}

		finalMessages = append(finalMessages, om)

		if om.Role == "user" {
			lastUserMsgIndex = len(finalMessages) - 1
		}
	}

	if combinedSystemPrompt != "" {
		systemHeader := "[SİSTEM ANAYASASI VE KİMLİĞİN - BUNA KESİNLİKLE UYACAKSIN]:\n" + strings.TrimSpace(combinedSystemPrompt) + "\n\n[GÜNCEL KULLANICI TALEBİ]:\n"

		if lastUserMsgIndex != -1 {
			finalMessages[lastUserMsgIndex].Content = systemHeader + finalMessages[lastUserMsgIndex].Content
		} else {
			finalMessages = append([]ollamaMessage{{Role: "user", Content: systemHeader + "Hazırım, komutlarını bekliyorum."}}, finalMessages...)
		}
	}

	reqBody := ollamaRequest{
		Model:     o.Model,
		Messages:  finalMessages,
		Stream:    true,
		KeepAlive: -1,
		Think:     false,
		Options: map[string]interface{}{
			"temperature": o.Temperature,
			"num_ctx":     o.NumCtx,
		},
	}

	logger.Debug("📦 [Ollama] Tool schema hazırlanıyor: %d tool", len(tools))
	for _, t := range tools {
		ot := ollamaTool{Type: "function"}
		ot.Function.Name = t.Name()
		ot.Function.Description = t.Description()
		ot.Function.Parameters = t.Parameters()
		reqBody.Tools = append(reqBody.Tools, ot)
		logger.Debug("🛠️ [Ollama] Tool eklendi: %s", t.Name())
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("JSON paketleme hatası: %v", err)
	}

	logger.Debug("📦 [Ollama] Request JSON boyutu: %d bytes", len(jsonData))

	url := fmt.Sprintf("%s/api/chat", o.BaseURL)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("HTTP request oluşturulamadı: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}

	logger.Debug("📡 [Ollama] POST request gönderiliyor: URL=%s", url)

	resp, err := o.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama bağlantı hatası: %v", err)
	}
	defer resp.Body.Close()

	logger.Debug("📥 [Ollama] Response alındı: Status=%d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		logger.Error("❌ [Ollama] API hatası: Status=%d, Body=%s", resp.StatusCode, string(bodyBytes))
		return nil, fmt.Errorf("ollama API hatası (Durum: %d) - Detay: %s", resp.StatusCode, string(bodyBytes))
	}

	brainResp := &kernel.BrainResponse{}
	streamChan, hasStream := ctx.Value("stream_chan").(chan string)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), MaxScannerBufferSize)

	contentLength := 0
	tokenCount := 0
	toolCallCount := 0

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			logger.Warn("⚠️ [Ollama] Context iptal edildi, streaming durduruluyor")
			return brainResp, ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var chunk ollamaResponse
		if err := json.Unmarshal(line, &chunk); err != nil {
			logger.Debug("⚠️ [Ollama] JSON parse hatası: %v", err)
			continue
		}

		if chunk.Message.Content != "" {
			contentLength += len(chunk.Message.Content)
			tokenCount++

			if contentLength > config.LLMMaxContentLength {
				logger.Warn("⚠️ [Ollama] Response çok büyük, streaming durduruluyor")
				break
			}

			brainResp.Content += chunk.Message.Content

			if hasStream && streamChan != nil {
				select {
				case streamChan <- chunk.Message.Content:
				default:
					logger.Debug("⚠️ [Ollama] Stream channel dolu, token atlandı")
				}
			}
		}

		if len(chunk.Message.ToolCalls) > 0 {
			toolCallCount += len(chunk.Message.ToolCalls)
			for _, tc := range chunk.Message.ToolCalls {
				logger.Debug("🛠️ [Ollama] Tool call parse edildi: Function=%s, Args=%v", tc.Function.Name, tc.Function.Arguments)
				brainResp.ToolCalls = append(brainResp.ToolCalls, kernel.ToolCall{
					Function:  tc.Function.Name,
					Arguments: tc.Function.Arguments,
				})
			}
		}

		if chunk.Done {
			brainResp.Usage = map[string]int{"completion_tokens": chunk.EvalCount}
			break
		}
	}

	if err := scanner.Err(); err != nil {
		logger.Error("❌ [Ollama] Scanner hatası: %v", err)
		return nil, fmt.Errorf("ollama canlı akış okuma hatası: %v", err)
	}

	logger.Info("✅ [Ollama] Response tamamlandı: Content=%d karakter, Tokens=%d, ToolCalls=%d", len(brainResp.Content), tokenCount, toolCallCount)

	if brainResp.Content == "" && len(brainResp.ToolCalls) == 0 {
		logger.Warn("⚠️ [Ollama] Yanıt boş geldi - Model tool call üretmedi")
		return &kernel.BrainResponse{
			Content: "[OLLAMA UYARISI]: Yanıt boş geldi.",
		}, nil
	}

	return brainResp, nil
}

func (o *OllamaProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if o == nil {
		return nil, fmt.Errorf("ollama provider nil")
	}

	if text == "" {
		return nil, fmt.Errorf("embed için metin boş olamaz")
	}

	if len(text) > config.LLMMaxContentLength {
		text = text[:config.LLMMaxContentLength]
		logger.Warn("⚠️ [Ollama] Embed text kırpıldı (%d karakter)", config.LLMMaxContentLength)
	}

	logger.Debug("🧠 [Ollama] Embed başlatılıyor: Model=%s, Text=%d karakter", o.Model, len(text))

	reqBody := map[string]interface{}{
		"model":      o.Model,
		"prompt":     text,
		"keep_alive": -1,
		"think":      false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("JSON paketleme hatası: %v", err)
	}

	url := fmt.Sprintf("%s/api/embeddings", o.BaseURL)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("HTTP request oluşturulamadı: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}

	resp, err := o.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama bağlantı hatası: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed hatası (Durum: %d) - Detay: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Embedding []float32 `json:"embedding"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("embed yanıtı çözülemedi: %v", err)
	}

	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("embedding vektörü boş")
	}

	logger.Debug("✅ [Ollama] Embedding oluşturuldu: %d boyut", len(result.Embedding))

	return result.Embedding, nil
}


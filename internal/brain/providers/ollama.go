// internal/brain/providers/ollama.go
// 🚀 DÜZELTMELER: Memory leak fix, Buffer optimization, Error handling, Validation
// ⚠️ DİKKAT: kernel.BrainResponse'ın thread-safe metodlarını kullanır

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

	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

// 🚨 YENİ: Buffer ve Limit Sabitleri
const (
	MaxScannerBufferSize = 10 * 1024 * 1024 // 10 MB (Büyük tool response'ları için)
	MaxContentLength     = 500 * 1024       // 500 KB (LLM response limiti)
	MaxImagesPerMessage  = 5                // Görsel sayısı limiti
	MaxImageSize         = 10 * 1024 * 1024 // 10 MB (Görsel boyut limiti)
	HTTPTimeout          = 600 * time.Second
)

// OllamaProvider: Ollama API için istemci
type OllamaProvider struct {
	BaseURL     string
	Model       string
	Temperature float64
	NumCtx      int
	APIKey      string
	Client      *http.Client
}

// NewOllama: Yeni Ollama sağlayıcı oluşturur
func NewOllama(url, model string, temp float64, numCtx int, apiKey string) *OllamaProvider {
	if numCtx == 0 {
		numCtx = 8192 // Varsayılan context window
	}

	// 🚨 DÜZELTME #1: HTTP Client timeout yapılandırması
	return &OllamaProvider{
		BaseURL:     strings.TrimSuffix(url, "/"),
		Model:       model,
		Temperature: temp,
		NumCtx:      numCtx,
		APIKey:      apiKey,
		Client: &http.Client{
			Timeout: HTTPTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// -- Ollama Spesifik Araç Şeması --
type ollamaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Parameters  map[string]interface{} `json:"parameters"`
	} `json:"function"`
}

// -- İstek Yapıları --
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

// Chat: LLM ile konuşur ve görselleri Ollama'nın sevdiği formata sokar
func (o *OllamaProvider) Chat(ctx context.Context, history []kernel.Message, tools []kernel.Tool) (*kernel.BrainResponse, error) {
	// 🚨 DÜZELTME #2: Nil ve input validation
	if o == nil {
		return nil, fmt.Errorf("ollama provider nil")
	}

	if o.BaseURL == "" {
		return nil, fmt.Errorf("ollama base URL boş")
	}

	// 🚨 DÜZELTME #3: Content length limiti (Memory bloat önleme)
	totalChars := 0
	for _, msg := range history {
		totalChars += len(msg.Content)
	}
	if totalChars > MaxContentLength*2 {
		logger.Warn("⚠️ [Ollama] History çok büyük (%d karakter), ilk %d mesaj kullanılıyor", totalChars, len(history)/2)
		history = history[len(history)/2:]
	}

	var finalMessages []ollamaMessage
	imagePathRegex := regexp.MustCompile(`[a-zA-Z0-9_\\\/\-\.\:]+\.(png|jpg|jpeg)`)

	var combinedSystemPrompt string

	// 1. SİSTEM MESAJLARINI TOPLA
	for _, msg := range history {
		if msg.Role == "system" {
			combinedSystemPrompt += msg.Content + "\n\n"
		}
	}

	lastUserMsgIndex := -1

	// 2. MESAJLARI İŞLE
	for _, msg := range history {
		if msg.Role == "system" {
			continue
		}

		// 🚨 DÜZELTME #4: Görsel sayısı limiti
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

		// 🚨 DÜZELTME #5: Görsel dosya yolu yakalayıcı + boyut kontrolü
		if msg.Content != "" {
			matches := imagePathRegex.FindAllString(msg.Content, -1)
			for _, match := range matches {
				if len(om.Images) >= MaxImagesPerMessage {
					break
				}

				resolvedPath := resolveMediaPath(match)

				// 🚨 DÜZELTME #6: Dosya boyutu kontrolü
				if info, err := os.Stat(resolvedPath); err == nil && info.Size() > MaxImageSize {
					logger.Warn("⚠️ [Ollama] Görsel çok büyük (%d byte): %s", info.Size(), resolvedPath)
					continue
				}

				imgData, err := os.ReadFile(resolvedPath)
				if err == nil {
					b64Data := base64.StdEncoding.EncodeToString(imgData)
					om.Images = append(om.Images, b64Data)
				}
			}
		}

		// Tool Output İşleme
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

		// Tool Call İşleme
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
		}

		finalMessages = append(finalMessages, om)

		if om.Role == "user" {
			lastUserMsgIndex = len(finalMessages) - 1
		}
	}

	// 🚀 SİSTEM PROMPTUNU EN SON USER MESAJINA SABİTLE
	if combinedSystemPrompt != "" {
		systemHeader := "[SİSTEM ANAYASASI VE KİMLİĞİN - BUNA KESİNLİKLE UYACAKSIN]:\n" + strings.TrimSpace(combinedSystemPrompt) + "\n\n[GÜNCEL KULLANICI TALEBİ]:\n"

		if lastUserMsgIndex != -1 {
			finalMessages[lastUserMsgIndex].Content = systemHeader + finalMessages[lastUserMsgIndex].Content
		} else {
			finalMessages = append([]ollamaMessage{{Role: "user", Content: systemHeader + "Hazırım, komutlarını bekliyorum."}}, finalMessages...)
		}
	}

	// 3. İSTEĞİ HAZIRLA
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

	for _, t := range tools {
		ot := ollamaTool{Type: "function"}
		ot.Function.Name = t.Name()
		ot.Function.Description = t.Description()
		ot.Function.Parameters = t.Parameters()
		reqBody.Tools = append(reqBody.Tools, ot)
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("JSON paketleme hatası: %v", err)
	}

	// URL oluşturma (405 Hatasını önlemek için token URL'ye eklenmez, sadece Header'da gönderilir)
	url := fmt.Sprintf("%s/api/chat", o.BaseURL)

	// 🚨 DÜZELTME #7: HTTP request hatasını kontrol et
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("HTTP request oluşturulamadı: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	
	// SADECE HEADER ÜZERİNDEN YETKİLENDİRME
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}

	// 4. GÖNDER
	resp, err := o.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama bağlantı hatası: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama API hatası (Durum: %d) - Detay: %s", resp.StatusCode, string(bodyBytes))
	}

	// =========================================================================
	// 5. CANLI AKIŞ (NDJSON) VERİ SİNDİRME MOTORU
	// =========================================================================
	brainResp := &kernel.BrainResponse{}
	streamChan, hasStream := ctx.Value("stream_chan").(chan string)

	// 🚨 DÜZELTME #8: Scanner buffer boyutunu artır (büyük response'lar için)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), MaxScannerBufferSize)

	// 🚨 DÜZELTME #9: Content length tracking (memory bloat önleme)
	contentLength := 0

	for scanner.Scan() {
		// 🚨 DÜZELTME #10: Context cancellation kontrolü (her iterasyonda)
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
			// 🚨 DÜZELTME #11: Content length limiti
			contentLength += len(chunk.Message.Content)
			if contentLength > MaxContentLength {
				logger.Warn("⚠️ [Ollama] Response çok büyük, streaming durduruluyor")
				break
			}

			brainResp.Content += chunk.Message.Content

			// 🚨 DÜZELTME #12: Stream channel blocking önleme (non-blocking send)
			if hasStream && streamChan != nil {
				select {
				case streamChan <- chunk.Message.Content:
				default:
					logger.Debug("⚠️ [Ollama] Stream channel dolu, token atlandı")
				}
			}
		}

		if len(chunk.Message.ToolCalls) > 0 {
			for _, tc := range chunk.Message.ToolCalls {
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

	// 🚨 DÜZELTME #13: Scanner error handling
	if err := scanner.Err(); err != nil {
		logger.Error("❌ [Ollama] Scanner hatası: %v", err)
		return nil, fmt.Errorf("ollama canlı akış okuma hatası: %v", err)
	}

	// 🚨 DÜZELTME #14: Boş response kontrolü
	if brainResp.Content == "" && len(brainResp.ToolCalls) == 0 {
		logger.Warn("⚠️ [Ollama] Yanıt boş geldi")
		return &kernel.BrainResponse{
			Content: "[OLLAMA UYARISI]: Yanıt boş geldi.",
		}, nil
	}

	logger.Debug("✅ [Ollama] Response alındı: %d karakter, %d tool call", len(brainResp.Content), len(brainResp.ToolCalls))

	return brainResp, nil
}

// Embed: Metni vektöre çevirir
func (o *OllamaProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	// 🚨 DÜZELTME #15: Input validation
	if o == nil {
		return nil, fmt.Errorf("ollama provider nil")
	}

	if text == "" {
		return nil, fmt.Errorf("embed için metin boş olamaz")
	}

	// 🚨 DÜZELTME #16: Text length limiti
	if len(text) > MaxContentLength {
		text = text[:MaxContentLength]
		logger.Warn("⚠️ [Ollama] Embed text kırpıldı (%d karakter)", MaxContentLength)
	}

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

	// URL oluşturma (405 Hatasını önlemek için token URL'ye eklenmez)
	url := fmt.Sprintf("%s/api/embeddings", o.BaseURL)

	// 🚨 DÜZELTME #17: HTTP request hatasını kontrol et
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("HTTP request oluşturulamadı: %v", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	
	// SADECE HEADER ÜZERİNDEN YETKİLENDİRME
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

	// 🚨 DÜZELTME #18: Embedding validation
	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("embedding vektörü boş")
	}

	return result.Embedding, nil
}
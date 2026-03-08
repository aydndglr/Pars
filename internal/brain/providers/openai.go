// internal/brain/providers/openai.go
// 🚀 DÜZELTMELER: HTTP timeout, Validation, Error handling, Logging, Tool support
// ⚠️ DİKKAT: kernel.BrainResponse'ın thread-safe metodlarını kullanır

package providers

import (
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
	OpenAIMaxScannerBuffer = 10 * 1024 * 1024 // 10 MB
	OpenAIMaxContentLength = 500 * 1024       // 500 KB
	OpenAIMaxImagesPerMsg  = 5                // Görsel sayısı limiti
	OpenAIMaxImageSize     = 10 * 1024 * 1024 // 10 MB
	OpenAIHTTPTimeout      = 120 * time.Second
)

// OpenAIProvider: OpenAI uyumlu tüm API'ler (GPT-4, DeepSeek, LocalAI) için istemci.
type OpenAIProvider struct {
	BaseURL string
	APIKey  string
	Model   string
	Client  *http.Client
}

// NewOpenAI: Yeni OpenAI sağlayıcı oluşturur
func NewOpenAI(url, key, model string) *OpenAIProvider {
	if url == "" {
		url = "https://api.openai.com/v1"
	}

	// 🚨 DÜZELTME #1: HTTP Client timeout + connection pooling yapılandırması
	return &OpenAIProvider{
		BaseURL: strings.TrimSuffix(url, "/"),
		APIKey:  key,
		Model:   model,
		Client: &http.Client{
			Timeout: OpenAIHTTPTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}


// Chat: OpenAI API ile konuşur, görselleri ve tool'ları işler
func (o *OpenAIProvider) Chat(ctx context.Context, history []kernel.Message, tools []kernel.Tool) (*kernel.BrainResponse, error) {
	// 🚨 DÜZELTME #2: Nil ve input validation
	if o == nil {
		return nil, fmt.Errorf("openai provider nil")
	}

	if o.BaseURL == "" {
		return nil, fmt.Errorf("openai base URL boş")
	}

	if o.APIKey == "" {
		logger.Warn("⚠️ [OpenAI] API key eksik, anonymous request gönderiliyor")
	}

	// 🚨 DÜZELTME #3: Content length limiti (Memory bloat önleme)
	totalChars := 0
	for _, msg := range history {
		totalChars += len(msg.Content)
	}
	if totalChars > OpenAIMaxContentLength*2 {
		logger.Warn("⚠️ [OpenAI] History çok büyük (%d karakter), ilk %d mesaj kullanılıyor", totalChars, len(history)/2)
		history = history[len(history)/2:]
	}

	// API yapıları
	type imagePart struct {
		Type     string `json:"type"`
		ImageURL *struct {
			URL string `json:"url"`
		} `json:"image_url,omitempty"`
		Text string `json:"text,omitempty"`
	}
	type msg struct {
		Role    string      `json:"role"`
		Content interface{} `json:"content"` // String veya Multi-modal dizi
	}
	type toolDef struct {
		Type     string `json:"type"`
		Function struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		} `json:"function"`
	}
	type payload struct {
		Model    string    `json:"model"`
		Messages []msg     `json:"messages"`
		Tools    []toolDef `json:"tools,omitempty"`
		Stream   bool      `json:"stream"`
	}

	var messages []msg
	imagePathRegex := regexp.MustCompile(`[a-zA-Z0-9_\\\/\-\.\:]+\.(png|jpg|jpeg)`)

	for _, h := range history {
		// 🚨 DÜZELTME #4: Görsel sayısı limiti
		imageCount := 0
		var content interface{}

		// Görsel varsa Multi-modal yapı kur
		if len(h.Images) > 0 || h.Content != "" {
			var parts []imagePart

			// Metin içeriği ekle
			if h.Content != "" {
				parts = append(parts, imagePart{Type: "text", Text: h.Content})

				// 🚨 DÜZELTME #5: Content'teki dosya yollarını otomatik yükle
				matches := imagePathRegex.FindAllString(h.Content, -1)
				for _, match := range matches {
					if imageCount >= OpenAIMaxImagesPerMsg {
						logger.Warn("⚠️ [OpenAI] Görsel limiti aşıldı (%d), geri kalanlar atlandı", OpenAIMaxImagesPerMsg)
						break
					}

					resolvedPath := resolveMediaPath(match)

					// 🚨 DÜZELTME #6: Dosya boyutu kontrolü
					if info, err := os.Stat(resolvedPath); err == nil && info.Size() > OpenAIMaxImageSize {
						logger.Warn("⚠️ [OpenAI] Görsel çok büyük (%d byte): %s", info.Size(), resolvedPath)
						continue
					}

					imgData, err := os.ReadFile(resolvedPath)
					if err == nil {
						b64 := base64.StdEncoding.EncodeToString(imgData)
						parts = append(parts, imagePart{
							Type: "image_url",
							ImageURL: &struct {
								URL string `json:"url"`
							}{URL: "data:image/jpeg;base64," + b64},
						})
						imageCount++
					}
				}
			}

			// Images array'den gelen görselleri ekle
			for _, img := range h.Images {
				if imageCount >= OpenAIMaxImagesPerMsg {
					break
				}

				imgUrl := img
				if !strings.HasPrefix(img, "data:image") {
					imgUrl = "data:image/jpeg;base64," + img
				}
				parts = append(parts, imagePart{
					Type: "image_url",
					ImageURL: &struct {
						URL string `json:"url"`
					}{URL: imgUrl},
				})
				imageCount++
			}

			content = parts
		} else {
			// Sadece metin
			content = h.Content
		}

		messages = append(messages, msg{Role: h.Role, Content: content})
	}

	// 🚨 DÜZELTME #7: Tool'ları OpenAI formatına çevir
	var toolDefs []toolDef
	for _, t := range tools {
		var td toolDef
		td.Type = "function"
		td.Function.Name = t.Name()
		td.Function.Description = t.Description()
		td.Function.Parameters = t.Parameters()
		toolDefs = append(toolDefs, td)
	}

	reqBody := payload{
		Model:    o.Model,
		Messages: messages,
		Tools:    toolDefs,
		Stream:   false, // 🆕 Şimdilik non-streaming (daha stabil)
	}

	// 🚨 DÜZELTME #8: JSON marshal hatasını kontrol et
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("JSON paketleme hatası: %v", err)
	}

	// 🚨 DÜZELTME #9: HTTP request hatasını kontrol et
	req, err := http.NewRequestWithContext(ctx, "POST", o.BaseURL+"/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("HTTP request oluşturulamadı: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}

	resp, err := o.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenAI bağlantı hatası: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		logger.Error("❌ [OpenAI] API hatası (%d): %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("OpenAI API hatası (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls,omitempty"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			TotalTokens      int `json:"total_tokens"`
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("JSON decode hatası: %v", err)
	}

	if len(result.Choices) == 0 {
		logger.Warn("⚠️ [OpenAI] Boş cevap döndü")
		return &kernel.BrainResponse{
			Content: "[OPENAI UYARISI]: Yanıt boş geldi.",
		}, nil
	}

	// 🚨 DÜZELTME #10: BrainResponse oluştur ve thread-safe metodları kullan
	brainResp := &kernel.BrainResponse{}
	brainResp.SetContent(result.Choices[0].Message.Content)

	// Tool calls'ları parse et
	for _, tc := range result.Choices[0].Message.ToolCalls {
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			logger.Debug("⚠️ [OpenAI] Tool argüman parse hatası: %v", err)
			args = make(map[string]interface{})
		}

		toolCall := kernel.ToolCall{
			ID:        tc.ID,
			Function:  tc.Function.Name,
			Arguments: args,
		}

		if err := brainResp.AddToolCall(toolCall); err != nil {
			logger.Warn("⚠️ [OpenAI] ToolCall eklenemedi: %v", err)
		}
	}

	// Usage bilgilerini ekle
	brainResp.Usage = map[string]int{
		"total_tokens":      result.Usage.TotalTokens,
		"prompt_tokens":     result.Usage.PromptTokens,
		"completion_tokens": result.Usage.CompletionTokens,
	}

	logger.Debug("✅ [OpenAI] Response alındı: %d karakter, %d tool call, %d token",
		len(brainResp.GetContent()), len(brainResp.GetToolCalls()), result.Usage.TotalTokens)

	return brainResp, nil
}

// Embed: Metni vektöre çevirir
func (o *OpenAIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	// 🚨 DÜZELTME #11: Input validation
	if o == nil {
		return nil, fmt.Errorf("openai provider nil")
	}

	if text == "" {
		return nil, fmt.Errorf("embed için metin boş olamaz")
	}

	// 🚨 DÜZELTME #12: Text length limiti
	if len(text) > OpenAIMaxContentLength {
		text = text[:OpenAIMaxContentLength]
		logger.Warn("⚠️ [OpenAI] Embed text kırpıldı (%d karakter)", OpenAIMaxContentLength)
	}

	reqBody := map[string]interface{}{
		"model": "text-embedding-3-small", // 🆕 Modern embedding modeli
		"input": text,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("JSON paketleme hatası: %v", err)
	}

	// 🚨 DÜZELTME #13: HTTP request hatasını kontrol et
	req, err := http.NewRequestWithContext(ctx, "POST", o.BaseURL+"/v1/embeddings", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("HTTP request oluşturulamadı: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.APIKey)
	}

	resp, err := o.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenAI bağlantı hatası: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		logger.Error("❌ [OpenAI] Embed API hatası (%d): %s", resp.StatusCode, string(bodyBytes))
		return nil, fmt.Errorf("OpenAI embed hatası (%d): %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("JSON decode hatası: %v", err)
	}

	// 🚨 DÜZELTME #14: Embedding validation
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embedding vektörü boş")
	}

	logger.Debug("✅ [OpenAI] Embedding oluşturuldu: %d boyut", len(result.Data[0].Embedding))

	return result.Data[0].Embedding, nil
}
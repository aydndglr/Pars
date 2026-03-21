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

	"github.com/aydndglr/pars-agent-v3/internal/core/config"
	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)


const (
	OpenAIMaxScannerBuffer = 10 * 1024 * 1024 
	OpenAIMaxImagesPerMsg  = 5                
	OpenAIMaxImageSize     = 10 * 1024 * 1024 
)

type OpenAIProvider struct {
	BaseURL string
	APIKey  string
	Model   string
	Client  *http.Client
}

func NewOpenAI(url, key, model string) *OpenAIProvider {
	if url == "" {
		url = "https://api.openai.com/v1"
	}
	return &OpenAIProvider{
		BaseURL: strings.TrimSuffix(url, "/"),
		APIKey:  key,
		Model:   model,
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

func (o *OpenAIProvider) Chat(ctx context.Context, history []kernel.Message, tools []kernel.Tool) (*kernel.BrainResponse, error) {
	if o == nil {
		return nil, fmt.Errorf("openai provider nil")
	}

	if o.BaseURL == "" {
		return nil, fmt.Errorf("openai base URL boş")
	}

	if o.APIKey == "" {
		logger.Warn("⚠️ [OpenAI] API key eksik, anonymous request gönderiliyor")
	}

	totalChars := 0
	for _, msg := range history {
		totalChars += len(msg.Content)
	}
	if totalChars > config.LLMMaxContentLength*2 {
		logger.Warn("⚠️ [OpenAI] History çok büyük (%d karakter), ilk %d mesaj kullanılıyor", totalChars, len(history)/2)
		history = history[len(history)/2:]
	}

	type imagePart struct {
		Type     string `json:"type"`
		ImageURL *struct {
			URL string `json:"url"`
		} `json:"image_url,omitempty"`
		Text string `json:"text,omitempty"`
	}
	type msg struct {
		Role    string      `json:"role"`
		Content interface{} `json:"content"` 
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
		imageCount := 0
		var content interface{}
		if len(h.Images) > 0 || h.Content != "" {
			var parts []imagePart
			if h.Content != "" {
				parts = append(parts, imagePart{Type: "text", Text: h.Content})
				matches := imagePathRegex.FindAllString(h.Content, -1)
				for _, match := range matches {
					if imageCount >= OpenAIMaxImagesPerMsg {
						logger.Warn("⚠️ [OpenAI] Görsel limiti aşıldı (%d), geri kalanlar atlandı", OpenAIMaxImagesPerMsg)
						break
					}

					resolvedPath := resolveMediaPath(match)
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
			content = h.Content
		}

		messages = append(messages, msg{Role: h.Role, Content: content})
	}

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
		Stream:   false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("JSON paketleme hatası: %v", err)
	}

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

	brainResp := &kernel.BrainResponse{}
	brainResp.SetContent(result.Choices[0].Message.Content)

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

	brainResp.Usage = map[string]int{
		"total_tokens":      result.Usage.TotalTokens,
		"prompt_tokens":     result.Usage.PromptTokens,
		"completion_tokens": result.Usage.CompletionTokens,
	}

	logger.Debug("✅ [OpenAI] Response alındı: %d karakter, %d tool call, %d token",
		len(brainResp.GetContent()), len(brainResp.GetToolCalls()), result.Usage.TotalTokens)

	return brainResp, nil
}

func (o *OpenAIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if o == nil {
		return nil, fmt.Errorf("openai provider nil")
	}

	if text == "" {
		return nil, fmt.Errorf("embed için metin boş olamaz")
	}

	if len(text) > config.LLMMaxContentLength {
		text = text[:config.LLMMaxContentLength]
		logger.Warn("⚠️ [OpenAI] Embed text kırpıldı (%d karakter)", config.LLMMaxContentLength)
	}

	reqBody := map[string]interface{}{
		"model": "text-embedding-3-small", 
		"input": text,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("JSON paketleme hatası: %v", err)
	}

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

	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embedding vektörü boş")
	}

	logger.Debug("✅ [OpenAI] Embedding oluşturuldu: %d boyut", len(result.Data[0].Embedding))

	return result.Data[0].Embedding, nil
}
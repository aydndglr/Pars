// internal/brain/providers/gemini.go
// 🚀 DÜZELTMELER: Memory leak fix, HTTP timeout, Validation, Error handling, Logging fix
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
	GeminiMaxScannerBuffer = 10 * 1024 * 1024 // 10 MB
	GeminiMaxContentLength = 500 * 1024       // 500 KB
	GeminiMaxImagesPerMsg  = 5                // Görsel sayısı limiti
	GeminiMaxImageSize     = 10 * 1024 * 1024 // 10 MB
	GeminiHTTPTimeout      = 120 * time.Second
)

// GeminiProvider: Google Gemini API için istemci
type GeminiProvider struct {
	BaseURL string
	APIKey  string
	Model   string
	Client  *http.Client
}

// NewGemini: Yeni Gemini sağlayıcı oluşturur
func NewGemini(url, key, model string) *GeminiProvider {
	if url == "" {
		// 🚨 DÜZELTME: Fazladan boşlukları kaldır
		url = "https://generativelanguage.googleapis.com"
	}

	// 🚨 DÜZELTME #1: HTTP Client timeout yapılandırması
	return &GeminiProvider{
		BaseURL: strings.TrimSuffix(url, "/"),
		APIKey:  key,
		Model:   model,
		Client: &http.Client{
			Timeout: GeminiHTTPTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// Chat: Gemini API ile konuşur, görselleri ve tool'ları işler
func (g *GeminiProvider) Chat(ctx context.Context, history []kernel.Message, tools []kernel.Tool) (*kernel.BrainResponse, error) {
	// 🚨 DÜZELTME #2: Nil ve input validation
	if g == nil {
		return nil, fmt.Errorf("gemini provider nil")
	}

	if g.BaseURL == "" {
		return nil, fmt.Errorf("gemini base URL boş")
	}

	if g.APIKey == "" {
		return nil, fmt.Errorf("gemini API key eksik")
	}

	// 🚨 LOG EKLE: Giriş validasyonu sonrası
	logger.Debug("🔍 [Gemini] Chat başlatılıyor: Model=%s, History=%d mesaj, Tools=%d", 
		g.Model, len(history), len(tools))

	// 🚨 DÜZELTME #3: Content length limiti (Memory bloat önleme)
	totalChars := 0
	for _, msg := range history {
		totalChars += len(msg.Content)
	}
	if totalChars > GeminiMaxContentLength*2 {
		logger.Warn("⚠️ [Gemini] History çok büyük (%d karakter), ilk %d mesaj kullanılıyor", totalChars, len(history)/2)
		history = history[len(history)/2:]
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse", g.BaseURL, g.Model)

	// --- GEMINI API YAPILARI ---
	type inlineData struct {
		MimeType string `json:"mimeType"`
		Data     string `json:"data"`
	}
	type functionCall struct {
		Name string                 `json:"name"`
		Args map[string]interface{} `json:"args"`
	}
	type functionResponse struct {
		Name     string                 `json:"name"`
		Response map[string]interface{} `json:"response"`
	}
	type part struct {
		Text             string            `json:"text,omitempty"`
		InlineData       *inlineData       `json:"inlineData,omitempty"`
		FunctionCall     *functionCall     `json:"functionCall,omitempty"`
		FunctionResponse *functionResponse `json:"functionResponse,omitempty"`
	}
	type content struct {
		Role  string `json:"role"`
		Parts []part `json:"parts"`
	}
	type systemInstruction struct {
		Parts []part `json:"parts"`
	}
	type functionDeclaration struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Parameters  map[string]interface{} `json:"parameters"`
	}
	type toolWrapper struct {
		FunctionDeclarations []functionDeclaration `json:"functionDeclarations"`
	}
	type geminiReq struct {
		SystemInstruction *systemInstruction `json:"systemInstruction,omitempty"`
		Contents          []content          `json:"contents"`
		Tools             []toolWrapper      `json:"tools,omitempty"`
	}

	reqBody := geminiReq{}

	// 1. ARAÇLARI (TOOLS) YÜKLE
	if len(tools) > 0 {
		var funcs []functionDeclaration
		for _, t := range tools {
			funcs = append(funcs, functionDeclaration{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Parameters(),
			})
		}
		// 🚨 LOG EKLE: Tools yüklendikten sonra
		funcNames := make([]string, len(funcs))
		for i, f := range funcs { funcNames[i] = f.Name }
		logger.Debug("🛠️ [Gemini] %d tool API'ye eklendi: %v", len(funcs), funcNames)
		reqBody.Tools = append(reqBody.Tools, toolWrapper{FunctionDeclarations: funcs})
	}

	imagePathRegex := regexp.MustCompile(`[a-zA-Z0-9_\\\/\-\.\:]+\.(png|jpg|jpeg)`)

	// 2. MESAJ GEÇMİŞİNİ İŞLE
	var rawContents []content
	for _, h := range history {
		// 🚨 LOG EKLE: Her mesaj işlenirken
		logger.Debug("📝 [Gemini] Mesaj işleniyor: Role=%s, Content=%d chars, Images=%d, ToolCalls=%d", 
			h.Role, len(h.Content), len(h.Images), len(h.ToolCalls))
			
		if h.Role == "system" {
			reqBody.SystemInstruction = &systemInstruction{
				Parts: []part{{Text: h.Content}},
			}
			continue
		}

		// 🚨 HATA ÇÖZÜMÜ: Gemini sadece 'user' ve 'model' anlar!
		role := h.Role
		if role == "assistant" {
			role = "model"
		} else {
			role = "user"
		}

		var parts []part

		// 🚨 DÜZELTME #4: Görsel sayısı limiti
		imageCount := 0

		if h.Role == "tool" {
			var responseData map[string]interface{}
			if err := json.Unmarshal([]byte(h.Content), &responseData); err != nil {
				responseData = map[string]interface{}{"result": h.Content}
			}

			// 🚨 DÜZELTME #5: Görsel dosya yolu yakalayıcı + boyut kontrolü
			matches := imagePathRegex.FindAllString(h.Content, -1)
			for _, match := range matches {
				if imageCount >= GeminiMaxImagesPerMsg {
					logger.Warn("⚠️ [Gemini] Görsel limiti aşıldı (%d), geri kalanlar atlandı", GeminiMaxImagesPerMsg)
					break
				}

				resolvedPath := resolveMediaPath(match)

				// 🚨 DÜZELTME #6: Dosya boyutu kontrolü
				if info, err := os.Stat(resolvedPath); err == nil && info.Size() > GeminiMaxImageSize {
					logger.Warn("⚠️ [Gemini] Görsel çok büyük (%d byte): %s", info.Size(), resolvedPath)
					continue
				}

				imgData, err := os.ReadFile(resolvedPath)
				if err == nil {
					mimeType := "image/jpeg"
					if strings.HasSuffix(strings.ToLower(resolvedPath), ".png") {
						mimeType = "image/png"
					}
					b64Data := base64.StdEncoding.EncodeToString(imgData)
					
					// 🚨 LOG EKLE: Görsel eklendiğinde (struct initializer DIŞINDA)
					logger.Debug("🖼️ [Gemini] Görsel eklendi: %s -> %d bytes, MIME=%s", 
						resolvedPath, len(imgData), mimeType)
					
					parts = append(parts, part{
						InlineData: &inlineData{MimeType: mimeType, Data: b64Data},
					})
					imageCount++
				}
			}

			parts = append(parts, part{
				FunctionResponse: &functionResponse{
					Name:     h.Name,
					Response: responseData,
				},
			})
		} else {
			if h.Content != "" {
				matches := imagePathRegex.FindAllString(h.Content, -1)
				for _, match := range matches {
					if imageCount >= GeminiMaxImagesPerMsg {
						break
					}

					resolvedPath := resolveMediaPath(match)

					if info, err := os.Stat(resolvedPath); err == nil && info.Size() > GeminiMaxImageSize {
						logger.Warn("⚠️ [Gemini] Görsel çok büyük (%d byte): %s", info.Size(), resolvedPath)
						continue
					}

					imgData, err := os.ReadFile(resolvedPath)
					if err == nil {
						mimeType := "image/jpeg"
						if strings.HasSuffix(strings.ToLower(resolvedPath), ".png") {
							mimeType = "image/png"
						}
						b64Data := base64.StdEncoding.EncodeToString(imgData)
						parts = append(parts, part{
							InlineData: &inlineData{MimeType: mimeType, Data: b64Data},
						})
						imageCount++
					}
				}

				parts = append(parts, part{Text: h.Content})
			}

			if len(h.ToolCalls) > 0 {
				for _, tc := range h.ToolCalls {
					parts = append(parts, part{
						FunctionCall: &functionCall{
							Name: tc.Function,
							Args: tc.Arguments,
						},
					})
				}
			}
		}

		// 🚨 DÜZELTME #7: Images array'den gelen görselleri de limite tabi tut
		for _, img := range h.Images {
			if imageCount >= GeminiMaxImagesPerMsg {
				break
			}

			mimeType := "image/jpeg"
			b64Data := img
			if strings.HasPrefix(img, "data:") {
				partsSplit := strings.SplitN(img, ";base64,", 2)
				if len(partsSplit) == 2 {
					mimeType = strings.TrimPrefix(partsSplit[0], "data:")
					b64Data = partsSplit[1]
				}
			}
			parts = append(parts, part{
				InlineData: &inlineData{MimeType: mimeType, Data: b64Data},
			})
			imageCount++
		}

		if len(rawContents) > 0 && rawContents[len(rawContents)-1].Role == role {
			rawContents[len(rawContents)-1].Parts = append(rawContents[len(rawContents)-1].Parts, parts...)
		} else {
			rawContents = append(rawContents, content{Role: role, Parts: parts})
		}
	}

	logger.Debug("🔧 [Gemini] Sanitization öncesi: %d raw turn", len(rawContents))
	// 🚀 ZIRH 5: GEMİNİ INVALID_ARGUMENT ANTİ-VİRÜSÜ
	var sanitizedContents []content
	for i := 0; i < len(rawContents); i++ {
		c := rawContents[i]

		hasFunctionCall := false
		hasFunctionResponse := false
		for _, p := range c.Parts {
			if p.FunctionCall != nil {
				hasFunctionCall = true
			}
			if p.FunctionResponse != nil {
				hasFunctionResponse = true
			}
		}

		if hasFunctionResponse {
			if len(sanitizedContents) == 0 || sanitizedContents[len(sanitizedContents)-1].Role != "model" {
				continue
			}
		}

		if hasFunctionCall {
			if i+1 >= len(rawContents) {
				continue
			}
			nextHasResponse := false
			for _, np := range rawContents[i+1].Parts {
				if np.FunctionResponse != nil {
					nextHasResponse = true
					break
				}
			}
			if !nextHasResponse {
				continue
			}
		}

		sanitizedContents = append(sanitizedContents, c)
	}
	reqBody.Contents = sanitizedContents
	logger.Debug("✅ [Gemini] Sanitization sonrası: %d turn kaldı (atlanan: %d)", 
		len(sanitizedContents), len(rawContents)-len(sanitizedContents))
		
	// 3. İSTEK GÖNDER
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("gemini istek gövdesi oluşturulamadı: %v", err)
	}

	// 🚨 DÜZELTME #8: HTTP request hatasını kontrol et
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("gemini http isteği hazırlanamadı: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.APIKey)
	// 🚨 LOG EKLE: İstek gönderilmeden
	logger.Debug("📤 [Gemini] API isteği gönderiliyor: URL=%s, Payload=%d bytes", 
		url, len(jsonData))
		
	resp, err := g.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini API bağlantı hatası: %v", err)
	}
	defer resp.Body.Close()

	// 🚨 LOG EKLE: Response status
	logger.Debug("📥 [Gemini] Response alındı: Status=%d, Content-Length=%s", 
		resp.StatusCode, resp.Header.Get("Content-Length"))

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini API hatası (%d): %s", resp.StatusCode, string(bodyBytes))
	}

	// =========================================================================
	// 🚀 4. CANLI AKIŞ (SSE) VERİ SİNDİRME MOTORU
	// =========================================================================
	brainResp := &kernel.BrainResponse{}

	streamChan, hasStream := ctx.Value("stream_chan").(chan string)

	// 🚨 DÜZELTME #9: Scanner buffer boyutunu artır
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), GeminiMaxScannerBuffer)

	// 🚨 DÜZELTME #10: Content length tracking
	contentLength := 0

	for scanner.Scan() {
		// 🚨 DÜZELTME #11: Context cancellation kontrolü (her iterasyonda)
		select {
		case <-ctx.Done():
			logger.Warn("⚠️ [Gemini] Context iptal edildi, streaming durduruluyor")
			return brainResp, ctx.Err()
		default:
		}

		line := scanner.Text()

		// 🚨 LOG EKLE: Her SSE chunk işlendiğinde (debug modda)
		logger.Debug("🔄 [Gemini] Stream chunk: %d chars", len(line))

		if strings.HasPrefix(line, "data: ") {
			dataStr := strings.TrimPrefix(line, "data: ")

			if dataStr == "[DONE]" {
				break
			}

			var chunk struct {
				Candidates []struct {
					Content struct {
						Parts []part `json:"parts"`
					} `json:"content"`
				} `json:"candidates"`
			}

			if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
				logger.Debug("⚠️ [Gemini] JSON parse hatası: %v", err)
				continue
			}

			if len(chunk.Candidates) > 0 {
				for _, p := range chunk.Candidates[0].Content.Parts {
					if p.Text != "" {
						// 🚨 DÜZELTME #12: Content length limiti
						contentLength += len(p.Text)
						if contentLength > GeminiMaxContentLength {
							logger.Warn("⚠️ [Gemini] Response çok büyük, streaming durduruluyor")
							break
						}

						brainResp.Content += p.Text

						// 🚨 DÜZELTME #13: Stream channel blocking önleme (non-blocking send)
						if hasStream && streamChan != nil {
							select {
							case streamChan <- p.Text:
							default:
								logger.Debug("⚠️ [Gemini] Stream channel dolu, token atlandı")
							}
						}
					}
					if p.FunctionCall != nil {
						// 🚨 LOG EKLE: Tool call parse edildiğinde (struct initializer DIŞINDA)
						logger.Debug("⚡ [Gemini] Tool call parse edildi: Function=%s, Args=%v", 
							p.FunctionCall.Name, p.FunctionCall.Args)
							
						brainResp.ToolCalls = append(brainResp.ToolCalls, kernel.ToolCall{
							Function:  p.FunctionCall.Name,
							Arguments: p.FunctionCall.Args,
						})
					}
				}
			}
		}
	}

	// 🚨 DÜZELTME #14: Scanner error handling
	if err := scanner.Err(); err != nil {
		logger.Error("❌ [Gemini] Scanner hatası: %v", err)
		return nil, fmt.Errorf("canlı akış okuma hatası: %v", err)
	}

	// 🚨 DÜZELTME #15: Boş response kontrolü
	if brainResp.Content == "" && len(brainResp.ToolCalls) == 0 {
		logger.Warn("⚠️ [Gemini] Yanıt boş geldi")
		return &kernel.BrainResponse{
			Content: "[SİSTEM UYARISI]: Yanıt boş geldi. Lütfen kararını, kodunu veya aracını açıkça belirt.",
		}, nil
	}

	logger.Debug("✅ [Gemini] Response alındı: %d karakter, %d tool call", len(brainResp.Content), len(brainResp.ToolCalls))

	// 🚨 LOG EKLE: Final response
	logger.Success("✅ [Gemini] Chat tamamlandı: Content=%d chars, ToolCalls=%d, Tokens=%v", 
		len(brainResp.Content), len(brainResp.ToolCalls), brainResp.Usage)

	return brainResp, nil
}

// =========================================================================
// 🧠 GERÇEK Vektör Motoru (text-embedding-004)
// =========================================================================

// Embed: Metni vektöre çevirir
func (g *GeminiProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	// 🚨 DÜZELTME #16: Input validation
	if g == nil {
		return nil, fmt.Errorf("gemini provider nil")
	}

	if text == "" {
		return nil, fmt.Errorf("embed için metin boş olamaz")
	}

	// 🚨 DÜZELTME #17: Text length limiti
	if len(text) > GeminiMaxContentLength {
		text = text[:GeminiMaxContentLength]
		logger.Warn("⚠️ [Gemini] Embed text kırpıldı (%d karakter)", GeminiMaxContentLength)
	}

	url := fmt.Sprintf("%s/v1beta/models/text-embedding-004:embedContent", g.BaseURL)

	reqBody := map[string]interface{}{
		"model": "models/text-embedding-004",
		"content": map[string]interface{}{
			"parts": []map[string]interface{}{
				{"text": text},
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("embed payload oluşturulamadı: %v", err)
	}

	// 🚨 DÜZELTME #18: HTTP request hatasını kontrol et
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("embed http isteği hazırlanamadı: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.APIKey)
	// 🚨 LOG EKLE: İstek gönderilmeden
	logger.Debug("📤 [Gemini] Embed API isteği gönderiliyor: URL=%s, Payload=%d bytes", 
		url, len(jsonData))
		
	resp, err := g.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed API bağlantı hatası: %v", err)
	}
	defer resp.Body.Close()
	
	// 🚨 LOG EKLE: Response status
	logger.Debug("📥 [Gemini] Embed Response alındı: Status=%d", resp.StatusCode)
	
	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embed API hatası (%d): %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Embedding struct {
			Values []float32 `json:"values"`
		} `json:"embedding"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("embed yanıtı çözülemedi: %v", err)
	}

	// 🚨 DÜZELTME #19: Embedding validation
	if len(result.Embedding.Values) == 0 {
		return nil, fmt.Errorf("embedding vektörü boş")
	}

	// 🚨 LOG EKLE: Embedding başarıyla oluşturulduğunda
	logger.Debug("✅ [Gemini] Embedding oluşturuldu: %d boyut, input=%d chars", 
		len(result.Embedding.Values), len(text))

	return result.Embedding.Values, nil
}
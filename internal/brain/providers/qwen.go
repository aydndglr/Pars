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
    "path/filepath"
    "regexp"
    "strings"
    "time"

    "github.com/aydndglr/pars-agent-v3/internal/core/config"
    "github.com/aydndglr/pars-agent-v3/internal/core/kernel"
    "github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

const (
    QwenMaxScannerBuffer   = 10 * 1024 * 1024 
    QwenMaxImagesPerMsg    = 5                
    QwenMaxImageSize       = 10 * 1024 * 1024 
    QwenAPIBaseURL         = "https://dashscope-intl.aliyuncs.com/compatible-mode/v1"
)

type QwenProvider struct {
    BaseURL     string
    APIKey      string
    Model       string
    Temperature float64
    NumCtx      int
    Client      *http.Client
}

func NewQwen(url, key, model string, temp float64, numCtx int) *QwenProvider {
    if url == "" {
        url = QwenAPIBaseURL
    }
    if numCtx == 0 {
        numCtx = 8192 
    }
    if temp == 0 {
        temp = 0.7
    }

    return &QwenProvider{
        BaseURL:     strings.TrimSpace(strings.TrimRight(url, "/")),
        APIKey:      key,
        Model:       model,
        Temperature: temp,
        NumCtx:      numCtx,
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

func resolveQwenMediaPath(path string) string {
    if filepath.IsAbs(path) {
        return path
    }
    absPath, err := filepath.Abs(path)
    if err != nil {
        return path
    }
    return absPath
}

type qwenMessage struct {
    Role      string         `json:"role"`
    Content   interface{}    `json:"content"` 
    ToolCalls []qwenToolCall `json:"tool_calls,omitempty"`
}

type qwenPart struct {
    Type     string        `json:"type"` 
    Text     string        `json:"text,omitempty"`
    ImageURL *qwenImageURL `json:"image_url,omitempty"`
}

type qwenImageURL struct {
    URL string `json:"url"`
}

type qwenToolCall struct {
    ID       string           `json:"id"`
    Type     string           `json:"type"` 
    Function qwenFunctionCall `json:"function"`
}

type qwenFunctionCall struct {
    Name      string                 `json:"name"`
    Arguments map[string]interface{} `json:"arguments"` 
}

type qwenTool struct {
    Type     string           `json:"type"` 
    Function qwenFunctionDecl `json:"function"`
}

type qwenFunctionDecl struct {
    Name        string                 `json:"name"`
    Description string                 `json:"description"`
    Parameters  map[string]interface{} `json:"parameters"`
}

type qwenRequest struct {
    Model       string        `json:"model"`
    Messages    []qwenMessage `json:"messages"`
    Tools       []qwenTool    `json:"tools,omitempty"`
    Stream      bool          `json:"stream"`
    Temperature float64       `json:"temperature,omitempty"`
}

type qwenStreamResponse struct {
    ID      string `json:"id"`
    Choices []struct {
        Delta struct {
            Role      string         `json:"role,omitempty"`
            Content   string         `json:"content,omitempty"`
            ToolCalls []qwenToolCall `json:"tool_calls,omitempty"`
        } `json:"delta"`
        FinishReason *string `json:"finish_reason,omitempty"`
    } `json:"choices"`
}

func (q *QwenProvider) Chat(ctx context.Context, history []kernel.Message, tools []kernel.Tool) (*kernel.BrainResponse, error) {
    if q == nil {
        return nil, fmt.Errorf("qwen provider nil")
    }
    if q.APIKey == "" {
        logger.Warn("⚠️ [Qwen] API key eksik")
    }

    totalChars := 0
    for _, msg := range history {
        totalChars += len(msg.Content)
    }
    if totalChars > config.LLMMaxContentLength*2 {
        logger.Warn("⚠️ [Qwen] History çok büyük, kırpılıyor")
        history = history[len(history)/2:]
    }

    var messages []qwenMessage
    imagePathRegex := regexp.MustCompile(`[a-zA-Z0-9_\\\/\-\.\:]+\.(png|jpg|jpeg)`)

    for _, h := range history {
        imageCount := 0
        var content interface{}

        if len(h.Images) > 0 || h.Content != "" {
            var parts []qwenPart

            if h.Content != "" {
                parts = append(parts, qwenPart{Type: "text", Text: h.Content})

                matches := imagePathRegex.FindAllString(h.Content, -1)
                for _, match := range matches {
                    if imageCount >= QwenMaxImagesPerMsg {
                        break
                    }
                    resolvedPath := resolveQwenMediaPath(match)
                    if info, err := os.Stat(resolvedPath); err == nil && info.Size() > QwenMaxImageSize {
                        continue
                    }
                    imgData, err := os.ReadFile(resolvedPath)
                    if err == nil {
                        b64 := base64.StdEncoding.EncodeToString(imgData)
                        parts = append(parts, qwenPart{
                            Type: "image_url",
                            ImageURL: &qwenImageURL{
                                URL: "data:image/jpeg;base64," + b64,
                            },
                        })
                        imageCount++
                    }
                }
            }

            for _, img := range h.Images {
                if imageCount >= QwenMaxImagesPerMsg {
                    break
                }
                imgUrl := img
                if !strings.HasPrefix(img, "data:image") {
                    imgUrl = "data:image/jpeg;base64," + img
                }
                parts = append(parts, qwenPart{
                    Type: "image_url",
                    ImageURL: &qwenImageURL{URL: imgUrl},
                })
                imageCount++
            }
            content = parts
        } else {
            content = h.Content
        }

        var toolCalls []qwenToolCall
        for _, tc := range h.ToolCalls {
            toolCalls = append(toolCalls, qwenToolCall{
                ID:   tc.ID,
                Type: "function",
                Function: qwenFunctionCall{
                    Name:      tc.Function,
                    Arguments: tc.Arguments,
                },
            })
        }

        messages = append(messages, qwenMessage{
            Role:      h.Role,
            Content:   content,
            ToolCalls: toolCalls,
        })
    }

    var qwenTools []qwenTool
    for _, t := range tools {
        qwenTools = append(qwenTools, qwenTool{
            Type: "function",
            Function: qwenFunctionDecl{
                Name:        t.Name(),
                Description: t.Description(),
                Parameters:  t.Parameters(),
            },
        })
    }

    reqBody := qwenRequest{
        Model:       q.Model,
        Messages:    messages,
        Tools:       qwenTools,
        Stream:      true, 
        Temperature: q.Temperature,
    }

    jsonData, err := json.Marshal(reqBody)
    if err != nil {
        return nil, fmt.Errorf("JSON paketleme hatası: %v", err)
    }

    var url string
    if strings.HasSuffix(q.BaseURL, "/chat/completions") {
        url = q.BaseURL
    } else if strings.HasSuffix(q.BaseURL, "/v1") {
        url = q.BaseURL + "/chat/completions"
    } else {
        url = q.BaseURL + "/compatible-mode/v1/chat/completions"
    }

    req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
    if err != nil {
        return nil, fmt.Errorf("HTTP request hatası: %v", err)
    }

    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+q.APIKey)
    req.Header.Set("Accept", "text/event-stream")

    resp, err := q.Client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("Qwen bağlantı hatası: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        body, _ := io.ReadAll(resp.Body)
        logger.Error("❌ [Qwen] API hatası (%d): %s", resp.StatusCode, string(body))
        return nil, fmt.Errorf("Qwen API hatası (%d): %s", resp.StatusCode, string(body))
    }

    brainResp := &kernel.BrainResponse{}
    
    streamChan, hasStream := ctx.Value("stream_chan").(chan string)

    scanner := bufio.NewScanner(resp.Body)
    scanner.Buffer(make([]byte, 64*1024), QwenMaxScannerBuffer)

    contentLength := 0
    var currentToolCall *kernel.ToolCall 
    var rawArguments string

    for scanner.Scan() {
        select {
        case <-ctx.Done():
            logger.Warn("⚠️ [Qwen] Context iptal edildi, streaming durduruluyor")
            return brainResp, ctx.Err()
        default:
        }

        line := scanner.Text()

        if strings.HasPrefix(line, "data: ") {
            dataStr := strings.TrimPrefix(line, "data: ")
            if dataStr == "[DONE]" {
                break
            }

            var chunk qwenStreamResponse
            if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
                continue
            }

            if len(chunk.Choices) > 0 {
                delta := chunk.Choices[0].Delta

                if delta.Content != "" {
                    contentLength += len(delta.Content)
                    if contentLength > config.LLMMaxContentLength {
                        logger.Warn("⚠️ [Qwen] Response çok büyük, streaming durduruluyor")
                        break
                    }

                    brainResp.Content += delta.Content
                    if hasStream && streamChan != nil {
                        select {
                        case streamChan <- delta.Content: 
                        default:
                        }
                    }
                }

                if len(delta.ToolCalls) > 0 {
                    tcDelta := delta.ToolCalls[0]
                    
                    if tcDelta.Function.Name != "" {
                        if currentToolCall != nil {
                            var argsMap map[string]interface{}
                            if rawArguments != "" {
                                json.Unmarshal([]byte(rawArguments), &argsMap)
                            }
                            currentToolCall.Arguments = argsMap
                            brainResp.ToolCalls = append(brainResp.ToolCalls, *currentToolCall)
                        }

                        currentToolCall = &kernel.ToolCall{
                            ID:       tcDelta.ID,
                            Function: tcDelta.Function.Name,
                        }
                        rawArguments = "" 
                    }
                    
                    if tcDelta.Function.Arguments != nil {
   
                        if argsStr, ok := tcDelta.Function.Arguments[""].(string); ok {
                             rawArguments += argsStr
                        } else if b, err := json.Marshal(tcDelta.Function.Arguments); err == nil {
                             rawArguments += string(b)
                        }
                    }
                }
            }
        }
    }

    if currentToolCall != nil {
        var argsMap map[string]interface{}
        if rawArguments != "" {
            json.Unmarshal([]byte(rawArguments), &argsMap)
        }
        currentToolCall.Arguments = argsMap
        brainResp.ToolCalls = append(brainResp.ToolCalls, *currentToolCall)
    }

    if err := scanner.Err(); err != nil {
        logger.Error("❌ [Qwen] Scanner hatası: %v", err)
        return nil, fmt.Errorf("canlı akış okuma hatası: %v", err)
    }

    if brainResp.Content == "" && len(brainResp.ToolCalls) == 0 {
        return &kernel.BrainResponse{
            Content: "[QWEN UYARISI]: Yanıt boş geldi.",
        }, nil
    }

    logger.Debug("✅ [Qwen] Response alındı: %d karakter, %d tool call", len(brainResp.Content), len(brainResp.ToolCalls))

    return brainResp, nil
}

func (q *QwenProvider) Embed(ctx context.Context, text string) ([]float32, error) {
    if q == nil {
        return nil, fmt.Errorf("qwen provider nil")
    }
    if text == "" {
        return nil, fmt.Errorf("embed için metin boş")
    }

    if len(text) > config.LLMMaxContentLength {
        text = text[:config.LLMMaxContentLength]
    }

    reqBody := map[string]interface{}{
        "model": "text-embedding-v2",
        "input": map[string]interface{}{
            "texts": []string{text},
        },
    }

    jsonData, err := json.Marshal(reqBody)
    if err != nil {
        return nil, fmt.Errorf("JSON paketleme hatası: %v", err)
    }

    cleanBase := q.BaseURL
    if idx := strings.Index(cleanBase, "/compatible-mode"); idx != -1 {
        cleanBase = cleanBase[:idx]
    } else if idx := strings.Index(cleanBase, "/api-openai"); idx != -1 {
        cleanBase = cleanBase[:idx]
    } else if idx := strings.Index(cleanBase, "/v1"); idx != -1 {
        cleanBase = cleanBase[:idx]
    }

    url := fmt.Sprintf("%s/api/v1/services/embeddings/text-embedding/text-embedding", cleanBase)
    req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
    if err != nil {
        return nil, fmt.Errorf("HTTP request hatası: %v", err)
    }

    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+q.APIKey)

    resp, err := q.Client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("Qwen bağlantı hatası: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("Qwen embed hatası (%d): %s", resp.StatusCode, string(body))
    }

    var result struct {
        Output struct {
            Embeddings []struct {
                Embedding []float32 `json:"embedding"`
            } `json:"embeddings"`
        } `json:"output"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, fmt.Errorf("JSON decode hatası: %v", err)
    }

    if len(result.Output.Embeddings) == 0 || len(result.Output.Embeddings[0].Embedding) == 0 {
        return nil, fmt.Errorf("embedding vektörü boş")
    }

    return result.Output.Embeddings[0].Embedding, nil
}
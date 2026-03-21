/*
package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

const (
	ipcPort               = "5137"
	MaxStreamBuffer       = 500
	MaxCommandTimeout     = 60 * time.Minute
	ShutdownTimeout       = 5 * time.Second
	MaxMessageSize        = 10 * 1024 * 1024
	InitialWaitTimeout    = 5 * time.Minute
	ActivityHeartbeat     = 30 * time.Second
	InactivityThreshold   = 5 * time.Minute
	ProgressCheckInterval = 10 * time.Second
)

var (
	toolCallPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)"action"\s*:\s*"`),
		regexp.MustCompile(`(?i)"function"\s*:\s*"`),
		regexp.MustCompile(`(?i)"name"\s*:\s*"`),
		regexp.MustCompile(`(?i)"tool"\s*:\s*"`),
		regexp.MustCompile(`(?i)jsonaction`),
		regexp.MustCompile(`\{"action":`),
		regexp.MustCompile(`\{"function":`),
	}
	jsonStartPattern = regexp.MustCompile(`\{["\s]`)
	jsonEndPattern   = regexp.MustCompile(`\}`)
)

type ActivityTracker struct {
	lastActivityTime int64
	totalTokens      int64
	lastTokenCount   int64
	isStuck          int32
}

type CommandRequest struct {
	Prompt string `json:"prompt"`
	CWD    string `json:"cwd"`
	TaskID string `json:"task_id"`
}

type CommandResponse struct {
	Result string `json:"result"`
	Error  string `json:"error,omitempty"`
}

func NewActivityTracker() *ActivityTracker {
	logger.Debug("📊 [ActivityTracker] Yeni tracker oluşturuldu")
	return &ActivityTracker{
		lastActivityTime: time.Now().Unix(),
	}
}

func (t *ActivityTracker) MarkActivity() {
	atomic.StoreInt64(&t.lastActivityTime, time.Now().Unix())
}

func (t *ActivityTracker) AddToken(count int) {
	atomic.AddInt64(&t.totalTokens, int64(count))
	atomic.StoreInt64(&t.lastTokenCount, int64(count))
	t.MarkActivity()
	logger.Debug("📊 [ActivityTracker] Token eklendi: %d, Toplam: %d", count, t.GetTotalTokens())
}

func (t *ActivityTracker) GetLastActivity() time.Time {
	ts := atomic.LoadInt64(&t.lastActivityTime)
	return time.Unix(ts, 0)
}

func (t *ActivityTracker) GetTotalTokens() int64 {
	return atomic.LoadInt64(&t.totalTokens)
}

func (t *ActivityTracker) IsInactive(threshold time.Duration) bool {
	lastActivity := t.GetLastActivity()
	return time.Since(lastActivity) > threshold
}

func (t *ActivityTracker) MarkStuck(stuck bool) {
	var val int32 = 0
	if stuck {
		val = 1
	}
	atomic.StoreInt32(&t.isStuck, val)
	logger.Warn("📊 [ActivityTracker] Stuck durumu: %v", stuck)
}

func (t *ActivityTracker) IsStuck() bool {
	return atomic.LoadInt32(&t.isStuck) == 1
}

func (t *ActivityTracker) GetStatus() map[string]interface{} {
	lastActivity := t.GetLastActivity()
	inactiveDuration := time.Since(lastActivity)
	logger.Debug("📊 [ActivityTracker] Status sorgulandı: Tokens=%d, Stuck=%v", t.GetTotalTokens(), t.IsStuck())
	return map[string]interface{}{
		"last_activity":     lastActivity,
		"inactive_duration": inactiveDuration.String(),
		"total_tokens":      t.GetTotalTokens(),
		"is_stuck":          t.IsStuck(),
		"is_inactive":       t.IsInactive(InactivityThreshold),
	}
}

type StreamClient struct {
	TaskID       string
	Chan         chan string
	closeOnce    sync.Once
	disconnected int32
	tracker      *ActivityTracker
}

func NewStreamClient(taskID string) *StreamClient {
	logger.Debug("🔌 [StreamClient] Yeni stream client oluşturuldu: %s", taskID)
	return &StreamClient{
		TaskID:       taskID,
		Chan:         make(chan string, MaxStreamBuffer),
		tracker:      NewActivityTracker(),
		disconnected: 0,
	}
}

func (sc *StreamClient) SafeClose() {
	sc.closeOnce.Do(func() {
		atomic.StoreInt32(&sc.disconnected, 1)
		defer func() {
			if r := recover(); r != nil {
				logger.Debug("⚠️ [IPC] Channel close panic recovered: %v", r)
			}
		}()
		close(sc.Chan)
		logger.Debug("🔌 [StreamClient] Stream client kapatıldı: %s", sc.TaskID)
	})
}

func (sc *StreamClient) IsDisconnected() bool {
	return atomic.LoadInt32(&sc.disconnected) == 1
}

func (sc *StreamClient) GetTracker() *ActivityTracker {
	return sc.tracker
}

var (
	streamClients      = make(map[string]*StreamClient)
	streamMu           sync.RWMutex
	loggerHookID       int
	loggerHookMu       sync.Mutex
	serverInstance     *http.Server
	executionSemaphore chan struct{}
	rateLimiter        *RateLimiter
)

type RateLimiter struct {
	requests    []time.Time
	mu          sync.Mutex
	window      time.Duration
	maxRequests int
}

func NewRateLimiter(window time.Duration, maxRequests int) *RateLimiter {
	logger.Debug("📊 [RateLimiter] Yeni rate limiter: window=%v, max=%d", window, maxRequests)
	return &RateLimiter{
		requests:    make([]time.Time, 0, maxRequests),
		window:      window,
		maxRequests: maxRequests,
	}
}

func (r *RateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-r.window)
	validCount := 0
	for i, t := range r.requests {
		if t.After(cutoff) {
			if i != validCount {
				r.requests[validCount] = t
			}
			validCount++
		}
	}
	r.requests = r.requests[:validCount]
	if len(r.requests) >= r.maxRequests {
		logger.Warn("⚠️ [RateLimiter] Rate limit aşıldı: %d/%d", len(r.requests), r.maxRequests)
		return false
	}
	r.requests = append(r.requests, now)
	return true
}

func StartServer(ctx context.Context, agent kernel.Agent) {
	logger.Info("🚀 [IPC] IPC server başlatılıyor...")
	
	if agent == nil {
		logger.Error("❌ [IPC] Agent nil! IPC server başlatılamadı.")
		return
	}

	executionSemaphore = make(chan struct{}, 10)
	rateLimiter = NewRateLimiter(1*time.Minute, 20)

	mux := http.NewServeMux()

	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		taskID := r.URL.Query().Get("task_id")
		if taskID == "" {
			logger.Warn("⚠️ [IPC] Stream request: TaskID eksik")
			http.Error(w, "TaskID required", http.StatusBadRequest)
			return
		}

		logger.Debug("📡 [IPC] Stream bağlantısı: %s", taskID)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Accel-Buffering", "no")

		flusher, ok := w.(http.Flusher)
		if !ok {
			logger.Error("❌ [IPC] Streaming desteklenmiyor")
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		client := NewStreamClient(taskID)

		streamMu.Lock()
		streamClients[taskID] = client
		streamMu.Unlock()

		logger.Debug("🔌 [%s] Stream frequency açıldı.", taskID)

		defer func() {
			streamMu.Lock()
			delete(streamClients, taskID)
			streamMu.Unlock()
			client.SafeClose()
			logger.Debug("🔌 [%s] Stream frequency kapatıldı.", taskID)
		}()

		for {
			select {
			case msg, ok := <-client.Chan:
				if !ok {
					logger.Debug("🔌 [%s] Stream channel kapandı.", taskID)
					return
				}
				fmt.Fprintf(w, "data: %s\n\n", msg)
				flusher.Flush()
			case <-r.Context().Done():
				logger.Debug("🔌 [%s] Client bağlantısı kesildi.", taskID)
				return
			case <-ctx.Done():
				logger.Debug("🔌 [%s] Daemon kapanıyor.", taskID)
				return
			}
		}
	})

	mux.HandleFunc("/execute", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			logger.Warn("⚠️ [IPC] Geçersiz HTTP method: %s", r.Method)
			http.Error(w, "POST method only", http.StatusMethodNotAllowed)
			return
		}

		if rateLimiter != nil && !rateLimiter.Allow() {
			logger.Warn("⚠️ [IPC] Rate limit aşıldı: /execute")
			http.Error(w, fmt.Sprintf("Rate limit exceeded. Try again in %v", 1*time.Minute), 
				http.StatusTooManyRequests)
			return
		}

		select {
		case executionSemaphore <- struct{}{}:
			defer func() { <-executionSemaphore }()
		case <-r.Context().Done():
			logger.Warn("⚠️ [IPC] Request iptal edildi")
			http.Error(w, "Request cancelled", http.StatusRequestTimeout)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, MaxMessageSize)

		var req CommandRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			logger.Warn("⚠️ [IPC] Geçersiz JSON: %v", err)
			http.Error(w, "Invalid JSON request", http.StatusBadRequest)
			return
		}

		if req.Prompt == "" {
			logger.Warn("⚠️ [IPC] Prompt boş olamaz")
			http.Error(w, "Prompt cannot be empty", http.StatusBadRequest)
			return
		}

		if req.TaskID == "" {
			req.TaskID = fmt.Sprintf("IPC-%d", time.Now().UnixNano()%1000000)
		}

		logger.Debug("⚡ [IPC] Komut alındı: TaskID=%s, Prompt=%d karakter", req.TaskID, len(req.Prompt))

		execCtx := context.WithValue(r.Context(), "client_task_id", req.TaskID)
		llmStreamChan := make(chan string, MaxStreamBuffer)
		execCtx = context.WithValue(execCtx, "stream_chan", llmStreamChan)

		streamCtx, streamCancel := context.WithCancel(execCtx)
		
		var wg sync.WaitGroup
		wg.Add(1)
		tracker := NewActivityTracker()
		var tokenCount atomic.Int64
		activityChan := make(chan struct{}, 10)

		go func(taskID string) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					logger.Error("🚨 [IPC] Stream goroutine panic: %v", r)
				}
			}()

			for {
				select {
				case token, ok := <-llmStreamChan:
					if !ok {
						logger.Debug("🔌 [%s] LLM stream channel kapandı.", taskID)
						return
					}

					streamMu.RLock()
					sseClient, hasSSE := streamClients[taskID]
					streamMu.RUnlock()

					if hasSSE && sseClient != nil && !sseClient.IsDisconnected() {
						tracker.MarkActivity()
						tokenCount.Add(1)
						
						select {
						case activityChan <- struct{}{}:
						default:
						}

						filteredToken := filterToolCallJSON(token)
						
						if filteredToken != "" {
							if !sendToChannel(sseClient.Chan, "TOKEN::"+filteredToken, 50*time.Millisecond) {
								logger.Debug("⚠️ [IPC] Stream channel dolu, token atlandı: %s", taskID)
							}
						} else {
							logger.Debug("🔒 [IPC] Tool call JSON filtrelendi (terminal'den gizlendi)")
						}
					} else {
						logger.Debug("⚠️ [IPC] SSE client yok veya disconnected: %s", taskID)
					}
				case <-streamCtx.Done():
					logger.Debug("🔌 [%s] Stream context iptal edildi.", taskID)
					return
				}
			}
		}(req.TaskID)

		go func(taskID string) {
			initialDeadline := time.Now().Add(InitialWaitTimeout)
			
			for {
				select {
				case <-activityChan:
					initialDeadline = time.Now().Add(InitialWaitTimeout)
					continue
					
				case <-time.After(ActivityHeartbeat):
					lastActivity := tracker.GetLastActivity()
					inactiveDuration := time.Since(lastActivity)
					totalTokens := tracker.GetTotalTokens()
					
					if time.Now().After(initialDeadline) && totalTokens == 0 {
						logger.Error("🚨 [%s] DYNAMIC TIMEOUT: %d dakika içinde yanıt yok!",
							taskID, int(InitialWaitTimeout.Minutes()))
						tracker.MarkStuck(true)
						streamCancel()
						return
					}
					
					if inactiveDuration > InactivityThreshold && totalTokens > 0 {
						logger.Warn("⚠️ [%s] DYNAMIC TIMEOUT UYARISI: %d dakikadır token yok (Toplam: %d)",
							taskID, int(inactiveDuration.Minutes()), totalTokens)
					
						select {
						case <-activityChan:
							continue
						case <-time.After(ActivityHeartbeat):
							if time.Since(tracker.GetLastActivity()) > InactivityThreshold {
								logger.Error("🚨 [%s] DYNAMIC TIMEOUT: Model yanıt vermiyor, iptal ediliyor", taskID)
								tracker.MarkStuck(true)
								streamCancel()
								return
							}
						}
					}
					
				case <-streamCtx.Done():
					logger.Debug("🔌 [%s] Stream context iptal edildi (monitor).", taskID)
					return
				}
			}
		}(req.TaskID)

		enhancedPrompt := fmt.Sprintf("[SYSTEM] Active Path: %s\nUser: %s", req.CWD, req.Prompt)

		var result string
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("🚨 [IPC] Agent.Run panic: %v", r)
					err = fmt.Errorf("agent panic: %v", r)
				}
			}()
			logger.Info("🚀 [%s] Agent.Run başlatılıyor...", req.TaskID)
			result, err = agent.Run(execCtx, enhancedPrompt, nil)
			logger.Info("✅ [%s] Agent.Run tamamlandı", req.TaskID)
		}()

		streamCancel()
		
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()
		
		select {
		case <-done:
			logger.Debug("🔌 [%s] Stream goroutine tamamlandı.", req.TaskID)
		case <-time.After(5 * time.Second):
			logger.Warn("⚠️ [%s] Stream goroutine timeout (5 sn).", req.TaskID)
		}
		
		close(llmStreamChan)
		close(activityChan)
		time.Sleep(500 * time.Millisecond)

		status := tracker.GetStatus()
		logger.Info("✅ [%s] Görev tamamlandı: %d token, Son aktivite=%v önce, Stuck=%v",
			req.TaskID, tokenCount.Load(), status["inactive_duration"], status["is_stuck"])

		resp := CommandResponse{Result: result}
		if err != nil {
			resp.Error = err.Error()
			logger.Error("❌ [IPC] Komut hatası: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
			logger.Error("❌ [IPC] Response encoding hatası: %v", encErr)
		}
	})

	loggerHookMu.Lock()
	if loggerHookID > 0 {
		logger.RemoveOutputHook(loggerHookID)
		logger.Debug("🗑️ [IPC] Eski logger hook kaldırıldı (ID: %d)", loggerHookID)
	}
	
	loggerHookID = logger.AddOutputHook(func(level, msg string) {
		if level == "DEBUG" {
			return
		}

		streamMu.RLock()
		if len(streamClients) == 0 {
			streamMu.RUnlock()
			return
		}

		clientsCopy := make([]*StreamClient, 0, len(streamClients))
		for _, client := range streamClients {
			clientsCopy = append(clientsCopy, client)
		}
		streamMu.RUnlock()

		for _, client := range clientsCopy {
			if client.IsDisconnected() {
				continue
			}

			tag := fmt.Sprintf("[%s]", client.TaskID)
			if !strings.Contains(msg, tag) {
				continue
			}

			cleanMsg := strings.Replace(strings.Replace(msg, tag+" ", "", 1), tag, "", 1)
			lines := strings.Split(strings.TrimRight(cleanMsg, "\n"), "\n")
			
			for i, line := range lines {
				var formattedMsg string
				if i == 0 {
					formattedMsg = fmt.Sprintf("\033[90m  ⚙️ [%-7s] %s\033[0m", level, line)
				} else {
					formattedMsg = fmt.Sprintf("\033[90m  ⚙️ %-9s %s\033[0m", "", line)
				}
				
				select {
				case client.Chan <- formattedMsg:
				case <-time.After(100 * time.Millisecond):
					logger.Debug("⚠️ [IPC] Log channel dolu, mesaj atlandı: %s", client.TaskID)
				}
			}
		}
	})
	loggerHookMu.Unlock()

	server := &http.Server{
		Addr:         ":" + ipcPort,
		Handler:      mux,
		ReadTimeout:  0,
		WriteTimeout: 0,
		IdleTimeout:  5 * time.Minute,
	}

	serverInstance = server

	go func() {
		logger.Info("📡 [IPC] IPC sunucu başlatıldı: Port %s", ipcPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("🔌 IPC Server Hatası: %v", err)
		}
	}()

	<-ctx.Done()
	logger.Info("🛑 [IPC] IPC sunucu kapatılıyor...")

	loggerHookMu.Lock()
	if loggerHookID > 0 {
		logger.RemoveOutputHook(loggerHookID)
		logger.Debug("🗑️ [IPC] Logger hook kaldırıldı (ID: %d)", loggerHookID)
		loggerHookID = 0
	}
	loggerHookMu.Unlock()

	streamMu.Lock()
	for _, client := range streamClients {
		client.SafeClose()
	}
	streamClients = make(map[string]*StreamClient)
	streamMu.Unlock()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("❌ [IPC] Server shutdown hatası: %v", err)
	}

	logger.Info("✅ [IPC] IPC sunucu güvenli şekilde kapatıldı.")
}

func StopServer() {
	if serverInstance != nil {
		logger.Info("🛑 [IPC] External shutdown istendi...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
		defer cancel()
		if err := serverInstance.Shutdown(shutdownCtx); err != nil {
			logger.Error("❌ [IPC] External shutdown hatası: %v", err)
		}
	}
}

func SendCommand(taskID, prompt string) (string, error) {
	if prompt == "" {
		return "", fmt.Errorf("prompt boş olamaz")
	}

	if taskID == "" {
		taskID = fmt.Sprintf("CMD-%d", time.Now().UnixNano()%1000000)
	}

	cwd, err := os.Getwd()
	if err != nil {
		logger.Warn("⚠️ [IPC] CWD mevcut değil: %v", err)
		cwd = "."
	}

	reqBody, err := json.Marshal(CommandRequest{
		Prompt: prompt,
		CWD:    cwd,
		TaskID: taskID,
	})
	if err != nil {
		return "", fmt.Errorf("JSON paketleme hatası: %v", err)
	}

	client := &http.Client{
		Timeout: MaxCommandTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     5 * time.Minute,
		},
	}

	logger.Debug("📡 [IPC] Daemon'a komut gönderiliyor: %s", taskID)
	resp, err := client.Post(
		fmt.Sprintf("http://localhost:%s/execute", ipcPort),
		"application/json",
		bytes.NewBuffer(reqBody),
	)
	if err != nil {
		return "", fmt.Errorf("Pars uyanık değil. Önce '--daemon' ile başlatın.")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("IPC server hatası (%d): %s", resp.StatusCode, string(bodyBytes))
	}

	var cmdResp CommandResponse
	if err := json.NewDecoder(resp.Body).Decode(&cmdResp); err != nil {
		return "", fmt.Errorf("Geçersiz response: %v", err)
	}

	if cmdResp.Error != "" {
		return "", fmt.Errorf("%s", cmdResp.Error)
	}

	logger.Debug("✅ [IPC] Komut tamamlandı: %s", taskID)
	return cmdResp.Result, nil
}

func sendToChannel(ch chan string, msg string, retryDelay time.Duration) bool {
	select {
	case ch <- msg:
		return true
	default:
		if retryDelay > 0 {
			time.Sleep(retryDelay)
			select {
			case ch <- msg:
				return true
			default:
				return false
			}
		}
		return false
	}
}

func formatLogMessage(level, line string, isFirst bool) string {
	if isFirst {
		return fmt.Sprintf("\033[90m  ⚙️ [%-7s] %s\033[0m", level, line)
	}
	return fmt.Sprintf("\033[90m  ⚙️ %-9s %s\033[0m", "", line)
}

func filterToolCallJSON(token string) string {
	if token == "" {
		return ""
	}
	
	for _, pattern := range toolCallPatterns {
		if pattern.MatchString(token) {
			logger.Debug("🔒 [IPC] Tool call pattern eşleşti, filtrelendi: %s", token[:min(50, len(token))])
			return ""
		}
	}
	
	if strings.Contains(token, `"jsonaction"`) || 
	   strings.Contains(token, `"action":`) ||
	   strings.Contains(token, `"function":`) ||
	   strings.Contains(token, `"name":`) {
		logger.Debug("🔒 [IPC] Tool call JSON içeriği filtrelendi")
		return ""
	}
	
	if jsonStartPattern.MatchString(token) && jsonEndPattern.MatchString(token) {
		logger.Debug("🔒 [IPC] JSON bloğu filtrelendi")
		return ""
	}
	
	return token
}

func isToolCallJSON(buffer string) bool {
	for _, pattern := range toolCallPatterns {
		if pattern.MatchString(buffer) {
			return true
		}
	}
	
	if strings.Contains(buffer, `"action"`) ||
	   strings.Contains(buffer, `"function"`) ||
	   strings.Contains(buffer, `"name"`) ||
	   strings.Contains(buffer, `"arguments"`) ||
	   strings.Contains(buffer, `"parameters"`) {
		return true
	}
	
	return false
}

func cleanJSONFromBuffer(buffer string) string {
	startIdx := strings.Index(buffer, "{")
	if startIdx == -1 {
		return buffer
	}
	
	depth := 0
	inJSON := false
	var result strings.Builder
	
	for i := startIdx; i < len(buffer); i++ {
		switch buffer[i] {
		case '{':
			if !inJSON {
				inJSON = true
			}
			depth++
		case '}':
			depth--
			if depth == 0 && inJSON {
				inJSON = false
			}
		default:
			if !inJSON {
				result.WriteByte(buffer[i])
			}
		}
	}
	
	return strings.TrimSpace(result.String())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
*/




package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

const (
	ipcPort               = "5137"
	MaxStreamBuffer       = 500
	MaxCommandTimeout     = 60 * time.Minute
	ShutdownTimeout       = 5 * time.Second
	MaxMessageSize        = 10 * 1024 * 1024
	InitialWaitTimeout    = 5 * time.Minute
	ActivityHeartbeat     = 30 * time.Second
	InactivityThreshold   = 5 * time.Minute
	ProgressCheckInterval = 10 * time.Second
)

var (
	toolCallPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)"action"\s*:\s*"`),
		regexp.MustCompile(`(?i)"function"\s*:\s*"`),
		regexp.MustCompile(`(?i)"name"\s*:\s*"`),
		regexp.MustCompile(`(?i)"tool"\s*:\s*"`),
		regexp.MustCompile(`(?i)jsonaction`),
		regexp.MustCompile(`\{"action":`),
		regexp.MustCompile(`\{"function":`),
	}
	jsonStartPattern = regexp.MustCompile(`\{["\s]`)
	jsonEndPattern   = regexp.MustCompile(`\}`)
)

type ActivityTracker struct {
	lastActivityTime int64
	totalTokens      int64
	lastTokenCount   int64
	isStuck          int32
}

type CommandRequest struct {
	Prompt string `json:"prompt"`
	CWD    string `json:"cwd"`
	TaskID string `json:"task_id"`
}

type CommandResponse struct {
	Result string `json:"result"`
	Error  string `json:"error,omitempty"`
}

func NewActivityTracker() *ActivityTracker {
	logger.Debug("📊 [ActivityTracker] Yeni tracker oluşturuldu")
	return &ActivityTracker{
		lastActivityTime: time.Now().Unix(),
	}
}

func (t *ActivityTracker) MarkActivity() {
	atomic.StoreInt64(&t.lastActivityTime, time.Now().Unix())
}

func (t *ActivityTracker) AddToken(count int) {
	atomic.AddInt64(&t.totalTokens, int64(count))
	atomic.StoreInt64(&t.lastTokenCount, int64(count))
	t.MarkActivity()
	logger.Debug("📊 [ActivityTracker] Token eklendi: %d, Toplam: %d", count, t.GetTotalTokens())
}

func (t *ActivityTracker) GetLastActivity() time.Time {
	ts := atomic.LoadInt64(&t.lastActivityTime)
	return time.Unix(ts, 0)
}

func (t *ActivityTracker) GetTotalTokens() int64 {
	return atomic.LoadInt64(&t.totalTokens)
}

func (t *ActivityTracker) IsInactive(threshold time.Duration) bool {
	lastActivity := t.GetLastActivity()
	return time.Since(lastActivity) > threshold
}

func (t *ActivityTracker) MarkStuck(stuck bool) {
	var val int32 = 0
	if stuck {
		val = 1
	}
	atomic.StoreInt32(&t.isStuck, val)
	logger.Warn("📊 [ActivityTracker] Stuck durumu: %v", stuck)
}

func (t *ActivityTracker) IsStuck() bool {
	return atomic.LoadInt32(&t.isStuck) == 1
}

func (t *ActivityTracker) GetStatus() map[string]interface{} {
	lastActivity := t.GetLastActivity()
	inactiveDuration := time.Since(lastActivity)
	logger.Debug("📊 [ActivityTracker] Status sorgulandı: Tokens=%d, Stuck=%v", t.GetTotalTokens(), t.IsStuck())
	return map[string]interface{}{
		"last_activity":     lastActivity,
		"inactive_duration": inactiveDuration.String(),
		"total_tokens":      t.GetTotalTokens(),
		"is_stuck":          t.IsStuck(),
		"is_inactive":       t.IsInactive(InactivityThreshold),
	}
}

type StreamClient struct {
	TaskID       string
	Chan         chan string
	closeOnce    sync.Once
	disconnected int32
	tracker      *ActivityTracker
}

func NewStreamClient(taskID string) *StreamClient {
	logger.Debug("🔌 [StreamClient] Yeni stream client oluşturuldu: %s", taskID)
	return &StreamClient{
		TaskID:       taskID,
		Chan:         make(chan string, MaxStreamBuffer),
		tracker:      NewActivityTracker(),
		disconnected: 0,
	}
}

func (sc *StreamClient) SafeClose() {
	sc.closeOnce.Do(func() {
		atomic.StoreInt32(&sc.disconnected, 1)
		defer func() {
			if r := recover(); r != nil {
				logger.Debug("⚠️ [IPC] Channel close panic recovered: %v", r)
			}
		}()
		close(sc.Chan)
		logger.Debug("🔌 [StreamClient] Stream client kapatıldı: %s", sc.TaskID)
	})
}

func (sc *StreamClient) IsDisconnected() bool {
	return atomic.LoadInt32(&sc.disconnected) == 1
}

func (sc *StreamClient) GetTracker() *ActivityTracker {
	return sc.tracker
}

var (
	streamClients      = make(map[string]*StreamClient)
	streamMu           sync.RWMutex
	loggerHookID       int
	loggerHookMu       sync.Mutex
	serverInstance     *http.Server
	executionSemaphore chan struct{}
	rateLimiter        *RateLimiter
)

type RateLimiter struct {
	requests    []time.Time
	mu          sync.Mutex
	window      time.Duration
	maxRequests int
}

func NewRateLimiter(window time.Duration, maxRequests int) *RateLimiter {
	logger.Debug("📊 [RateLimiter] Yeni rate limiter: window=%v, max=%d", window, maxRequests)
	return &RateLimiter{
		requests:    make([]time.Time, 0, maxRequests),
		window:      window,
		maxRequests: maxRequests,
	}
}

func (r *RateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-r.window)
	validCount := 0
	for i, t := range r.requests {
		if t.After(cutoff) {
			if i != validCount {
				r.requests[validCount] = t
			}
			validCount++
		}
	}
	r.requests = r.requests[:validCount]
	if len(r.requests) >= r.maxRequests {
		logger.Warn("⚠️ [RateLimiter] Rate limit aşıldı: %d/%d", len(r.requests), r.maxRequests)
		return false
	}
	r.requests = append(r.requests, now)
	return true
}

func StartServer(ctx context.Context, agent kernel.Agent) {
	logger.Info("🚀 [IPC] IPC server başlatılıyor...")

	if agent == nil {
		logger.Error("❌ [IPC] Agent nil! IPC server başlatılamadı.")
		return
	}

	executionSemaphore = make(chan struct{}, 10)
	rateLimiter = NewRateLimiter(1*time.Minute, 20)

	mux := http.NewServeMux()

	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		taskID := r.URL.Query().Get("task_id")
		if taskID == "" {
			logger.Warn("⚠️ [IPC] Stream request: TaskID eksik")
			http.Error(w, "TaskID required", http.StatusBadRequest)
			return
		}

		logger.Debug("📡 [IPC] Stream bağlantısı: %s", taskID)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Accel-Buffering", "no")

		flusher, ok := w.(http.Flusher)
		if !ok {
			logger.Error("❌ [IPC] Streaming desteklenmiyor")
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		client := NewStreamClient(taskID)

		streamMu.Lock()
		streamClients[taskID] = client
		streamMu.Unlock()

		logger.Debug("🔌 [%s] Stream frequency açıldı.", taskID)

		defer func() {
			streamMu.Lock()
			delete(streamClients, taskID)
			streamMu.Unlock()
			client.SafeClose()
			logger.Debug("🔌 [%s] Stream frequency kapatıldı.", taskID)
		}()

		for {
			select {
			case msg, ok := <-client.Chan:
				if !ok {
					logger.Debug("🔌 [%s] Stream channel kapandı.", taskID)
					return
				}
				fmt.Fprintf(w, "data: %s\n\n", msg)
				flusher.Flush()
			case <-r.Context().Done():
				logger.Debug("🔌 [%s] Client bağlantısı kesildi.", taskID)
				return
			case <-ctx.Done():
				logger.Debug("🔌 [%s] Daemon kapanıyor.", taskID)
				return
			}
		}
	})

	mux.HandleFunc("/execute", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			logger.Warn("⚠️ [IPC] Geçersiz HTTP method: %s", r.Method)
			http.Error(w, "POST method only", http.StatusMethodNotAllowed)
			return
		}

		if rateLimiter != nil && !rateLimiter.Allow() {
			logger.Warn("⚠️ [IPC] Rate limit aşıldı: /execute")
			http.Error(w, fmt.Sprintf("Rate limit exceeded. Try again in %v", 1*time.Minute),
				http.StatusTooManyRequests)
			return
		}

		select {
		case executionSemaphore <- struct{}{}:
			defer func() { <-executionSemaphore }()
		case <-r.Context().Done():
			logger.Warn("⚠️ [IPC] Request iptal edildi (Sıra beklerken)")
			http.Error(w, "Request cancelled", http.StatusRequestTimeout)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, MaxMessageSize)

		var req CommandRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			logger.Warn("⚠️ [IPC] Geçersiz JSON: %v", err)
			http.Error(w, "Invalid JSON request", http.StatusBadRequest)
			return
		}

		if req.Prompt == "" {
			logger.Warn("⚠️ [IPC] Prompt boş olamaz")
			http.Error(w, "Prompt cannot be empty", http.StatusBadRequest)
			return
		}

		if req.TaskID == "" {
			req.TaskID = fmt.Sprintf("IPC-%d", time.Now().UnixNano()%1000000)
		}

		logger.Debug("⚡ [IPC] Komut alındı: TaskID=%s, Prompt=%d karakter", req.TaskID, len(req.Prompt))

		// 🔥 HAYAT KURTARAN DÜZELTME BURADA 🔥
		// r.Context() kullanmıyoruz. Pars'ı ana Daemon context'ine (ctx) bağlıyoruz.
		// Böylece WhatsApp/Client timeout verip bağlantıyı koparsa bile Pars görevini arka planda bitirir.
		execCtx := context.WithValue(ctx, "client_task_id", req.TaskID)
		
		llmStreamChan := make(chan string, MaxStreamBuffer)
		execCtx = context.WithValue(execCtx, "stream_chan", llmStreamChan)

		streamCtx, streamCancel := context.WithCancel(execCtx)

		var wg sync.WaitGroup
		wg.Add(1)
		tracker := NewActivityTracker()
		var tokenCount atomic.Int64
		activityChan := make(chan struct{}, 10)

		go func(taskID string) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					logger.Error("🚨 [IPC] Stream goroutine panic: %v", r)
				}
			}()

			for {
				select {
				case token, ok := <-llmStreamChan:
					if !ok {
						logger.Debug("🔌 [%s] LLM stream channel kapandı.", taskID)
						return
					}

					streamMu.RLock()
					sseClient, hasSSE := streamClients[taskID]
					streamMu.RUnlock()

					if hasSSE && sseClient != nil && !sseClient.IsDisconnected() {
						tracker.MarkActivity()
						tokenCount.Add(1)

						select {
						case activityChan <- struct{}{}:
						default:
						}

						filteredToken := filterToolCallJSON(token)

						if filteredToken != "" {
							if !sendToChannel(sseClient.Chan, "TOKEN::"+filteredToken, 50*time.Millisecond) {
								logger.Debug("⚠️ [IPC] Stream channel dolu, token atlandı: %s", taskID)
							}
						} else {
							logger.Debug("🔒 [IPC] Tool call JSON filtrelendi (terminal'den gizlendi)")
						}
					} else {
						logger.Debug("⚠️ [IPC] SSE client yok veya disconnected: %s", taskID)
					}
				case <-streamCtx.Done():
					logger.Debug("🔌 [%s] Stream context iptal edildi.", taskID)
					return
				}
			}
		}(req.TaskID)

		go func(taskID string) {
			initialDeadline := time.Now().Add(InitialWaitTimeout)

			for {
				select {
				case <-activityChan:
					initialDeadline = time.Now().Add(InitialWaitTimeout)
					continue

				case <-time.After(ActivityHeartbeat):
					lastActivity := tracker.GetLastActivity()
					inactiveDuration := time.Since(lastActivity)
					totalTokens := tracker.GetTotalTokens()

					if time.Now().After(initialDeadline) && totalTokens == 0 {
						logger.Error("🚨 [%s] DYNAMIC TIMEOUT: %d dakika içinde yanıt yok!",
							taskID, int(InitialWaitTimeout.Minutes()))
						tracker.MarkStuck(true)
						streamCancel()
						return
					}

					if inactiveDuration > InactivityThreshold && totalTokens > 0 {
						logger.Warn("⚠️ [%s] DYNAMIC TIMEOUT UYARISI: %d dakikadır token yok (Toplam: %d)",
							taskID, int(inactiveDuration.Minutes()), totalTokens)

						select {
						case <-activityChan:
							continue
						case <-time.After(ActivityHeartbeat):
							if time.Since(tracker.GetLastActivity()) > InactivityThreshold {
								logger.Error("🚨 [%s] DYNAMIC TIMEOUT: Model yanıt vermiyor, iptal ediliyor", taskID)
								tracker.MarkStuck(true)
								streamCancel()
								return
							}
						}
					}

				case <-streamCtx.Done():
					logger.Debug("🔌 [%s] Stream context iptal edildi (monitor).", taskID)
					return
				}
			}
		}(req.TaskID)

		enhancedPrompt := fmt.Sprintf("[SYSTEM] Active Path: %s\nUser: %s", req.CWD, req.Prompt)

		var result string
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("🚨 [IPC] Agent.Run panic: %v", r)
					err = fmt.Errorf("agent panic: %v", r)
				}
			}()
			logger.Info("🚀 [%s] Agent.Run başlatılıyor...", req.TaskID)
			result, err = agent.Run(execCtx, enhancedPrompt, nil)
			logger.Info("✅ [%s] Agent.Run tamamlandı", req.TaskID)
		}()

		streamCancel()

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			logger.Debug("🔌 [%s] Stream goroutine tamamlandı.", req.TaskID)
		case <-time.After(5 * time.Second):
			logger.Warn("⚠️ [%s] Stream goroutine timeout (5 sn).", req.TaskID)
		}

		close(llmStreamChan)
		close(activityChan)
		time.Sleep(500 * time.Millisecond)

		status := tracker.GetStatus()
		logger.Info("✅ [%s] Görev tamamlandı: %d token, Son aktivite=%v önce, Stuck=%v",
			req.TaskID, tokenCount.Load(), status["inactive_duration"], status["is_stuck"])

		resp := CommandResponse{Result: result}
		if err != nil {
			resp.Error = err.Error()
			logger.Error("❌ [IPC] Komut hatası: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		// WhatsApp/Client bağlantıyı kopardıysa bura hata verecek ama umurumuzda değil, görev çoktan yapıldı!
		if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
			logger.Warn("⚠️ [IPC] Response encoding hatası (İstemci ayrılmış olabilir): %v", encErr)
		}
	})

	loggerHookMu.Lock()
	if loggerHookID > 0 {
		logger.RemoveOutputHook(loggerHookID)
		logger.Debug("🗑️ [IPC] Eski logger hook kaldırıldı (ID: %d)", loggerHookID)
	}

	loggerHookID = logger.AddOutputHook(func(level, msg string) {
		if level == "DEBUG" {
			return
		}

		streamMu.RLock()
		if len(streamClients) == 0 {
			streamMu.RUnlock()
			return
		}

		clientsCopy := make([]*StreamClient, 0, len(streamClients))
		for _, client := range streamClients {
			clientsCopy = append(clientsCopy, client)
		}
		streamMu.RUnlock()

		for _, client := range clientsCopy {
			if client.IsDisconnected() {
				continue
			}

			tag := fmt.Sprintf("[%s]", client.TaskID)
			if !strings.Contains(msg, tag) {
				continue
			}

			cleanMsg := strings.Replace(strings.Replace(msg, tag+" ", "", 1), tag, "", 1)
			lines := strings.Split(strings.TrimRight(cleanMsg, "\n"), "\n")

			for i, line := range lines {
				var formattedMsg string
				if i == 0 {
					formattedMsg = fmt.Sprintf("\033[90m  ⚙️ [%-7s] %s\033[0m", level, line)
				} else {
					formattedMsg = fmt.Sprintf("\033[90m  ⚙️ %-9s %s\033[0m", "", line)
				}

				select {
				case client.Chan <- formattedMsg:
				case <-time.After(100 * time.Millisecond):
					logger.Debug("⚠️ [IPC] Log channel dolu, mesaj atlandı: %s", client.TaskID)
				}
			}
		}
	})
	loggerHookMu.Unlock()

	server := &http.Server{
		Addr:         ":" + ipcPort,
		Handler:      mux,
		ReadTimeout:  0,
		WriteTimeout: 0,
		IdleTimeout:  5 * time.Minute,
	}

	serverInstance = server

	go func() {
		logger.Info("📡 [IPC] IPC sunucu başlatıldı: Port %s", ipcPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("🔌 IPC Server Hatası: %v", err)
		}
	}()

	<-ctx.Done()
	logger.Info("🛑 [IPC] IPC sunucu kapatılıyor...")

	loggerHookMu.Lock()
	if loggerHookID > 0 {
		logger.RemoveOutputHook(loggerHookID)
		logger.Debug("🗑️ [IPC] Logger hook kaldırıldı (ID: %d)", loggerHookID)
		loggerHookID = 0
	}
	loggerHookMu.Unlock()

	streamMu.Lock()
	for _, client := range streamClients {
		client.SafeClose()
	}
	streamClients = make(map[string]*StreamClient)
	streamMu.Unlock()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("❌ [IPC] Server shutdown hatası: %v", err)
	}

	logger.Info("✅ [IPC] IPC sunucu güvenli şekilde kapatıldı.")
}

func StopServer() {
	if serverInstance != nil {
		logger.Info("🛑 [IPC] External shutdown istendi...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
		defer cancel()
		if err := serverInstance.Shutdown(shutdownCtx); err != nil {
			logger.Error("❌ [IPC] External shutdown hatası: %v", err)
		}
	}
}

func SendCommand(taskID, prompt string) (string, error) {
	if prompt == "" {
		return "", fmt.Errorf("prompt boş olamaz")
	}

	if taskID == "" {
		taskID = fmt.Sprintf("CMD-%d", time.Now().UnixNano()%1000000)
	}

	cwd, err := os.Getwd()
	if err != nil {
		logger.Warn("⚠️ [IPC] CWD mevcut değil: %v", err)
		cwd = "."
	}

	reqBody, err := json.Marshal(CommandRequest{
		Prompt: prompt,
		CWD:    cwd,
		TaskID: taskID,
	})
	if err != nil {
		return "", fmt.Errorf("JSON paketleme hatası: %v", err)
	}

	client := &http.Client{
		Timeout: MaxCommandTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     5 * time.Minute,
		},
	}

	logger.Debug("📡 [IPC] Daemon'a komut gönderiliyor: %s", taskID)
	resp, err := client.Post(
		fmt.Sprintf("http://localhost:%s/execute", ipcPort),
		"application/json",
		bytes.NewBuffer(reqBody),
	)
	if err != nil {
		return "", fmt.Errorf("Pars uyanık değil. Önce '--daemon' ile başlatın.")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("IPC server hatası (%d): %s", resp.StatusCode, string(bodyBytes))
	}

	var cmdResp CommandResponse
	if err := json.NewDecoder(resp.Body).Decode(&cmdResp); err != nil {
		return "", fmt.Errorf("Geçersiz response: %v", err)
	}

	if cmdResp.Error != "" {
		return "", fmt.Errorf("%s", cmdResp.Error)
	}

	logger.Debug("✅ [IPC] Komut tamamlandı: %s", taskID)
	return cmdResp.Result, nil
}

func sendToChannel(ch chan string, msg string, retryDelay time.Duration) bool {
	select {
	case ch <- msg:
		return true
	default:
		if retryDelay > 0 {
			time.Sleep(retryDelay)
			select {
			case ch <- msg:
				return true
			default:
				return false
			}
		}
		return false
	}
}

func formatLogMessage(level, line string, isFirst bool) string {
	if isFirst {
		return fmt.Sprintf("\033[90m  ⚙️ [%-7s] %s\033[0m", level, line)
	}
	return fmt.Sprintf("\033[90m  ⚙️ %-9s %s\033[0m", "", line)
}

func filterToolCallJSON(token string) string {
	if token == "" {
		return ""
	}

	for _, pattern := range toolCallPatterns {
		if pattern.MatchString(token) {
			logger.Debug("🔒 [IPC] Tool call pattern eşleşti, filtrelendi: %s", token[:min(50, len(token))])
			return ""
		}
	}

	if strings.Contains(token, `"jsonaction"`) ||
		strings.Contains(token, `"action":`) ||
		strings.Contains(token, `"function":`) ||
		strings.Contains(token, `"name":`) {
		logger.Debug("🔒 [IPC] Tool call JSON içeriği filtrelendi")
		return ""
	}

	if jsonStartPattern.MatchString(token) && jsonEndPattern.MatchString(token) {
		logger.Debug("🔒 [IPC] JSON bloğu filtrelendi")
		return ""
	}

	return token
}

func isToolCallJSON(buffer string) bool {
	for _, pattern := range toolCallPatterns {
		if pattern.MatchString(buffer) {
			return true
		}
	}

	if strings.Contains(buffer, `"action"`) ||
		strings.Contains(buffer, `"function"`) ||
		strings.Contains(buffer, `"name"`) ||
		strings.Contains(buffer, `"arguments"`) ||
		strings.Contains(buffer, `"parameters"`) {
		return true
	}

	return false
}

func cleanJSONFromBuffer(buffer string) string {
	startIdx := strings.Index(buffer, "{")
	if startIdx == -1 {
		return buffer
	}

	depth := 0
	inJSON := false
	var result strings.Builder

	for i := startIdx; i < len(buffer); i++ {
		switch buffer[i] {
		case '{':
			if !inJSON {
				inJSON = true
			}
			depth++
		case '}':
			depth--
			if depth == 0 && inJSON {
				inJSON = false
			}
		default:
			if !inJSON {
				result.WriteByte(buffer[i])
			}
		}
	}

	return strings.TrimSpace(result.String())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
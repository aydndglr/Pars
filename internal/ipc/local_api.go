
// internal/ipc/local_api.go
// 🚀 DÜZELTME V8: TAMAMEN DİNAMİK TIMEOUT - Model Aktivitesine Göre Çalışır
// ⚠️ DİKKAT: Sabit timeout'lar kaldırıldı, activity-based heartbeat sistemi eklendi
// 📅 Oluşturulma: 2026-03-07 (Pars V5 Critical Fix #9)

package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

// ========================================================================
// 🕐 DİNAMİK TIMEOUT SABİTLERİ (Sadece fallback için)
// ========================================================================
const (
	ipcPort                = "5137"
	MaxStreamBuffer        = 500
	MaxCommandTimeout      = 60 * time.Minute  // Absolute max (sadece güvenlik için)
	ShutdownTimeout        = 5 * time.Second
	MaxMessageSize         = 10 * 1024 * 1024  // 10 MB
	
	// 🆕 DİNAMİK TIMEOUT AYARLARI
	InitialWaitTimeout     = 5 * time.Minute   // İlk cevap için max bekleme
	ActivityHeartbeat      = 30 * time.Second  // Aktivite kontrol aralığı
	InactivityThreshold    = 3 * time.Minute   // İşlem yoksa timeout (3 dakika sessizlik)
	ProgressCheckInterval  = 10 * time.Second  // İlerleme kontrol aralığı
)

// ========================================================================
// 📊 AKTİVİTE TAKİP YAPISI
// ========================================================================
// ActivityTracker: Model aktivitesini gerçek zamanlı izler
type ActivityTracker struct {
	lastActivityTime atomic.Value // time.Time
	totalTokens      atomic.Int64
	lastTokenCount   atomic.Int64
	isStuck          atomic.Bool
	mu               sync.RWMutex
}

// CommandRequest: CLI'dan gelen komut isteği
type CommandRequest struct {
	Prompt string `json:"prompt"`
	CWD    string `json:"cwd"`
	TaskID string `json:"task_id"`
}

// CommandResponse: Daemon'dan dönen cevap
type CommandResponse struct {
	Result string `json:"result"`
	Error  string `json:"error,omitempty"`
}

// NewActivityTracker: Yeni aktivite takipçisi oluştur
func NewActivityTracker() *ActivityTracker {
	tracker := &ActivityTracker{}
	tracker.lastActivityTime.Store(time.Now())
	return tracker
}

// MarkActivity: Aktivite zamanını güncelle (her token'da çağrılacak)
func (t *ActivityTracker) MarkActivity() {
	t.lastActivityTime.Store(time.Now())
}

// AddToken: Token sayısını artır ve aktivite işaretle
func (t *ActivityTracker) AddToken(count int) {
	t.totalTokens.Add(int64(count))
	t.lastTokenCount.Store(int64(count))
	t.MarkActivity()
}

// GetLastActivity: Son aktivite zamanını al
func (t *ActivityTracker) GetLastActivity() time.Time {
	if val := t.lastActivityTime.Load(); val != nil {
		return val.(time.Time)
	}
	return time.Now()
}

// GetTotalTokens: Toplam token sayısını al
func (t *ActivityTracker) GetTotalTokens() int64 {
	return t.totalTokens.Load()
}

// IsInactive: Belirtilen süredir aktivite yok mu kontrol et
func (t *ActivityTracker) IsInactive(threshold time.Duration) bool {
	lastActivity := t.GetLastActivity()
	return time.Since(lastActivity) > threshold
}

// MarkStuck: İşlem sıkıştı olarak işaretle
func (t *ActivityTracker) MarkStuck(stuck bool) {
	t.isStuck.Store(stuck)
}

// IsStuck: İşlem sıkışmış mı kontrol et
func (t *ActivityTracker) IsStuck() bool {
	return t.isStuck.Load()
}

// GetStatus: Aktivite durumunu raporla
func (t *ActivityTracker) GetStatus() map[string]interface{} {
	lastActivity := t.GetLastActivity()
	inactiveDuration := time.Since(lastActivity)
	
	return map[string]interface{}{
		"last_activity":      lastActivity,
		"inactive_duration":  inactiveDuration.String(),
		"total_tokens":       t.GetTotalTokens(),
		"is_stuck":           t.IsStuck(),
		"is_inactive":        t.IsInactive(InactivityThreshold),
	}
}

// ========================================================================
// 📦 STREAM CLIENT (GÜNCELLENDİ)
// ========================================================================
type StreamClient struct {
	TaskID       string
	Chan         chan string
	closeOnce    sync.Once
	disconnected bool
	mu           sync.RWMutex
	tracker      *ActivityTracker // 🆕 Aktivite takipçisi
}

// NewStreamClient: Yeni stream client oluştur (aktivite takibi ile)
func NewStreamClient(taskID string) *StreamClient {
	return &StreamClient{
		TaskID:       taskID,
		Chan:         make(chan string, MaxStreamBuffer),
		tracker:      NewActivityTracker(),
	}
}

// 🆕 YENİ: SafeClose - Channel'ı güvenli şekilde kapat
func (sc *StreamClient) SafeClose() {
	sc.closeOnce.Do(func() {
		sc.mu.Lock()
		sc.disconnected = true
		sc.mu.Unlock()
		close(sc.Chan)
	})
}

// 🆕 YENİ: IsDisconnected - Client koptu mu kontrol et
func (sc *StreamClient) IsDisconnected() bool {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.disconnected
}

// 🆕 YENİ: GetTracker - Aktivite takipçisini al
func (sc *StreamClient) GetTracker() *ActivityTracker {
	return sc.tracker
}

// ========================================================================
// 🌍 GLOBAL DEĞİŞKENLER
// ========================================================================
var (
	streamClients  = make(map[string]*StreamClient)
	streamMu       sync.RWMutex
	loggerHookID   int
	serverInstance *http.Server
	
	// 🆕 DİNAMİK TIMEOUT MONITOR
	timeoutMonitorDone chan struct{}
	timeoutMonitorOnce sync.Once
)

// ========================================================================
// 🚀 START SERVER
// ========================================================================
func StartServer(ctx context.Context, agent kernel.Agent) {
	// 🚨 DÜZELTME #1: Nil check
	if agent == nil {
		logger.Error("❌ [IPC] Agent nil! IPC sunucu başlatılamadı.")
		return
	}

	mux := http.NewServeMux()

	// ========================================================================
	// 📡 [ENDPOINT] /stream : Telsiz Odasına Giriş (SSE)
	// ========================================================================
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		taskID := r.URL.Query().Get("task_id")
		if taskID == "" {
			http.Error(w, "TaskID gerekli", http.StatusBadRequest)
			return
		}

		// 🚨 DÜZELTME #2: CORS headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Accel-Buffering", "no")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming desteklenmiyor", http.StatusInternalServerError)
			return
		}

		// 🎫 Odayı Kaydet (aktivite takibi ile)
		client := NewStreamClient(taskID)

		streamMu.Lock()
		streamClients[taskID] = client
		streamMu.Unlock()

		logger.Debug("🔌 [%s] Telsiz frekansı açıldı.", taskID)

		// 🚪 Çıkışta Odayı Kapat ve Temizle
		defer func() {
			streamMu.Lock()
			delete(streamClients, taskID)
			streamMu.Unlock()
			client.SafeClose()
			logger.Debug("🔌 [%s] Telsiz frekansı kapatıldı.", taskID)
		}()

		// 🌊 Log ve Token Akışı Başlıyor
		for {
			select {
			case msg, ok := <-client.Chan:
				if !ok {
					return
				}
				fmt.Fprintf(w, "data: %s\n\n", msg)
				flusher.Flush()
			case <-r.Context().Done():
				logger.Debug("🔌 [%s] İstemci bağlantısı kesildi.", taskID)
				return
			case <-ctx.Done():
				logger.Debug("🔌 [%s] Daemon kapanıyor.", taskID)
				return
			}
		}
	})

	// ========================================================================
	// ⚡ [ENDPOINT] /execute : Mutfağa Sipariş Gönder (Komut)
	// ========================================================================
	mux.HandleFunc("/execute", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Sadece POST metodu", http.StatusMethodNotAllowed)
			return
		}

		// 🚨 DÜZELTME #4: Request body size limit
		r.Body = http.MaxBytesReader(w, r.Body, MaxMessageSize)

		var req CommandRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			logger.Warn("⚠️ [IPC] Geçersiz JSON: %v", err)
			http.Error(w, "Mutfak hatası: Geçersiz JSON", http.StatusBadRequest)
			return
		}

		// 🚨 DÜZELTME #5: Input validation
		if req.Prompt == "" {
			http.Error(w, "Prompt boş olamaz", http.StatusBadRequest)
			return
		}

		if req.TaskID == "" {
			req.TaskID = fmt.Sprintf("IPC-%d", time.Now().UnixNano()%1000000)
		}

		logger.Debug("⚡ [IPC] Komut alındı: TaskID=%s, Prompt=%d karakter", req.TaskID, len(req.Prompt))

		// 🚀 ID'yi Context'e enjekte et
		execCtx := context.WithValue(r.Context(), "client_task_id", req.TaskID)

		// 🚀 CANLI AKIŞ (STREAMING) KÖPRÜSÜ
		llmStreamChan := make(chan string, MaxStreamBuffer)
		execCtx = context.WithValue(execCtx, "stream_chan", llmStreamChan)

		// 🚨 DÜZELTME #6: Stream goroutine için context + WaitGroup
		streamCtx, streamCancel := context.WithCancel(execCtx)
		
		// 🆕 YENİ: WaitGroup ile goroutine takibi
		var wg sync.WaitGroup
		wg.Add(1)

		// 🆕 YENİ: Aktivite takipçisi oluştur
		tracker := NewActivityTracker()
		
		// 🆕 YENİ: Token sayacı
		var tokenCount atomic.Int64

		// 🆕 YENİ: Activity monitor channel
		activityChan := make(chan struct{}, 1)

		// Arka planda modelin kelimelerini dinle ve terminale ilet
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
						return
					}

					// 🚨 DÜZELTME: Her token'da dinamik lookup yap
					streamMu.RLock()
					sseClient, hasSSE := streamClients[taskID]
					streamMu.RUnlock()

					if hasSSE && sseClient != nil {
						// 🚨 YENİ: Client koptu mu kontrol et
						if sseClient.IsDisconnected() {
							logger.Debug("⚠️ [IPC] Client koptu, token gönderimi durduruluyor")
							return
						}

						// 🆕 YENİ: Aktivite işaretle
						tracker.MarkActivity()
						tokenCount.Add(1)
						
						// 🆕 YENİ: Activity signal gönder
						select {
						case activityChan <- struct{}{}:
						default:
						}

						// 🎯 Şifreli Mühür
						select {
						case sseClient.Chan <- "TOKEN::" + token:
						default:
							// 🚨 YENİ: Channel doluysa kısa bekle ve tekrar dene
							time.Sleep(50 * time.Millisecond)
							select {
							case sseClient.Chan <- "TOKEN::" + token:
							default:
								logger.Debug("⚠️ [IPC] Stream channel dolu, token atlandı")
							}
						}
					}
				case <-streamCtx.Done():
					return
				}
			}
		}(req.TaskID)

		// 🆕 YENİ: Dinamik Timeout Monitor Goroutine
		go func(taskID string) {
			logger.Debug("⏱️ [%s] Dinamik timeout monitor başlatıldı", taskID)
			
			// İlk cevap için bekleme süresi
			initialDeadline := time.Now().Add(InitialWaitTimeout)
			
			for {
				select {
				case <-activityChan:
					// Aktivite var, deadline'i uzat
					initialDeadline = time.Now().Add(InitialWaitTimeout)
					continue
					
				case <-time.After(ActivityHeartbeat):
					// 🆕 Aktivite kontrolü
					lastActivity := tracker.GetLastActivity()
					inactiveDuration := time.Since(lastActivity)
					totalTokens := tracker.GetTotalTokens()
					
					// Debug log (her 30 saniyede)
					logger.Debug("⏱️ [%s] Aktivite kontrolü: Son aktivite=%v önce, Token=%d, Inactive=%v",
						taskID, inactiveDuration.Round(time.Second), totalTokens, inactiveDuration)
					
					// 🚨 KRİTİK: İlk cevap hiç gelmedi mi?
					if time.Now().After(initialDeadline) && totalTokens == 0 {
						logger.Error("🚨 [%s] DİNAMİK TIMEOUT: İlk cevap %d dakika içinde gelmedi!",
							taskID, int(InitialWaitTimeout.Minutes()))
						tracker.MarkStuck(true)
						streamCancel()
						return
					}
					
					// 🚨 KRİTİK: Uzun süre sessizlik (model sıkıştı)
					if inactiveDuration > InactivityThreshold && totalTokens > 0 {
						logger.Warn("⚠️ [%s] DİNAMİK TIMEOUT UYARISI: %d dakikadır token gelmiyor (Toplam: %d token)",
							taskID, int(inactiveDuration.Minutes()), totalTokens)
						
						// İkinci kontrol (bir heartbeat daha bekle)
						select {
						case <-activityChan:
							// Aktivite geldi, devam et
							continue
						case <-time.After(ActivityHeartbeat):
							// Hala aktivite yok
							lastActivity2 := tracker.GetLastActivity()
							if time.Since(lastActivity2) > InactivityThreshold {
								logger.Error("🚨 [%s] DİNAMİK TIMEOUT: Model %d dakikadır yanıt vermiyor, işlem iptal ediliyor",
									taskID, int(InactivityThreshold.Minutes()))
								tracker.MarkStuck(true)
								streamCancel()
								return
							}
						}
					}
					
				case <-streamCtx.Done():
					logger.Debug("⏱️ [%s] Dinamik timeout monitor durduruldu", taskID)
					return
				}
			}
		}(req.TaskID)

		// Dosya yolları için akıllı düzenleme
		separator := string(os.PathSeparator)
		cwdPath := req.CWD
		if !strings.HasSuffix(cwdPath, separator) {
			cwdPath += separator
		}

		enhancedPrompt := fmt.Sprintf("[SYSTEM] Active Path: %s\nUser: %s", req.CWD, req.CWD, req.Prompt)

		// 🧠 Pars İş Başında!
		var result string
		var err error
		
		// 🆕 YENİ: Panic recovery ile agent.Run koruması
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("🚨 [IPC] Agent.Run panic: %v", r)
					err = fmt.Errorf("agent panic: %v", r)
				}
			}()
			result, err = agent.Run(execCtx, enhancedPrompt, nil)
		}()

		// 🚨 DÜZELTME #7: Stream goroutine'i bekle ve channel'ı kapat
		streamCancel()
		wg.Wait()
		
		// 🆕 YENİ: Kısa drain period (son token'lar için)
		time.Sleep(500 * time.Millisecond)
		
		close(llmStreamChan)
		close(activityChan)

		// 🆕 YENİ: Final aktivite raporu
		status := tracker.GetStatus()
		logger.Info("✅ [%s] Görev tamamlandı: %d token, Son aktivite=%v önce, Sıkışma=%v",
			req.TaskID, tokenCount.Load(), status["inactive_duration"], status["is_stuck"])

		resp := CommandResponse{Result: result}
		if err != nil {
			resp.Error = err.Error()
			logger.Error("❌ [IPC] Komut hatası: %v", err)
		} else {
			logger.Debug("✅ [IPC] Komut tamamlandı: TaskID=%s", req.TaskID)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// ========================================================================
	// 🔌 AKILLI LOG KANCASI (İZOLASYON FİLTRESİ)
	// ========================================================================
	loggerHookID = logger.AddOutputHook(func(level, msg string) {
		if level == "DEBUG" {
			return
		}

		streamMu.RLock()
		if len(streamClients) == 0 {
			streamMu.RUnlock()
			return
		}

		// 🆕 YENİ: Client'ları kopyala (lock'u hızlı bırak)
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

			if strings.Contains(msg, tag) {
				cleanMsg := strings.Replace(msg, tag+" ", "", 1)
				cleanMsg = strings.Replace(cleanMsg, tag, "", 1)

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
					default:
						time.Sleep(10 * time.Millisecond)
						select {
						case client.Chan <- formattedMsg:
						default:
							logger.Debug("⚠️ [IPC] Log channel dolu, mesaj atlandı: %s", client.TaskID)
						}
					}
				}
			}
		}
	})

	server := &http.Server{
		Addr:         ":" + ipcPort,
		Handler:      mux,
		ReadTimeout:  0,  // 🆕 Sınırsız (dinamik timeout kullanıyoruz)
		WriteTimeout: 0,  // 🆕 Sınırsız (dinamik timeout kullanıyoruz)
		IdleTimeout:  5 * time.Minute,  // 🆕 Idle connections için
	}

	serverInstance = server

	go func() {
		logger.Info("📡 [IPC] IPC sunucu başlatıldı: Port %s", ipcPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("🔌 IPC Sunucu Hatası: %v", err)
		}
	}()

	<-ctx.Done()
	logger.Info("🛑 [IPC] IPC sunucu kapatılıyor...")

	if loggerHookID > 0 {
		logger.RemoveOutputHook(loggerHookID)
		logger.Debug("🗑️ [IPC] Logger hook temizlendi (ID: %d)", loggerHookID)
	}

	streamMu.Lock()
	for _, client := range streamClients {
		client.SafeClose()
	}
	streamClients = make(map[string]*StreamClient)
	streamMu.Unlock()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("❌ [IPC] Sunucu kapatma hatası: %v", err)
	}

	logger.Info("✅ [IPC] IPC sunucu güvenli şekilde kapatıldı.")
}

// 🆕 YENİ: StopServer
func StopServer() {
	if serverInstance != nil {
		logger.Info("🛑 [IPC] External shutdown çağrıldı...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
		defer cancel()
		if err := serverInstance.Shutdown(shutdownCtx); err != nil {
			logger.Error("❌ [IPC] External shutdown hatası: %v", err)
		}
	}
}

// SendCommand
func SendCommand(taskID, prompt string) (string, error) {
	if prompt == "" {
		return "", fmt.Errorf("prompt boş olamaz")
	}

	if taskID == "" {
		taskID = fmt.Sprintf("CMD-%d", time.Now().UnixNano()%1000000)
	}

	cwd, err := os.Getwd()
	if err != nil {
		logger.Warn("⚠️ [IPC] CWD alınamadı: %v", err)
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

	// 🆕 DİNAMİK: Timeout'u max_COMMAND_TIMEOUT olarak ayarla (gerçek timeout dinamik olacak)
	client := &http.Client{
		Timeout: MaxCommandTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     5 * time.Minute,  // 🆕 90sn → 5dk
		},
	}

	resp, err := client.Post(
		fmt.Sprintf("http://localhost:%s/execute", ipcPort),
		"application/json",
		bytes.NewBuffer(reqBody),
	)
	if err != nil {
		return "", fmt.Errorf("Pars uyanık değil. Önce '--daemon' ile uyandırın.")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("IPC sunucu hatası (%d): %s", resp.StatusCode, string(bodyBytes))
	}

	var cmdResp CommandResponse
	if err := json.NewDecoder(resp.Body).Decode(&cmdResp); err != nil {
		return "", fmt.Errorf("Mutfaktan geçersiz yanıt geldi: %v", err)
	}

	if cmdResp.Error != "" {
		return "", fmt.Errorf("%s", cmdResp.Error)
	}

	return cmdResp.Result, nil
}
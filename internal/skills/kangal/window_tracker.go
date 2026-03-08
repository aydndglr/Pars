// internal/skills/kangal/window_tracker.go
// 🚀 KANGAL - WINDOW TRACKER (Aktif Pencere İzleyici)
// 📅 Oluşturulma: 2026-03-07 (Pars V5 - Kangal Edition)
// ⚠️ DİKKAT: Sadece Windows'ta çalışır, Linux/Mac'te stub fonksiyonlar döner

package kangal

import (
	"context"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/aydndglr/pars-agent-v3/internal/core/config"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"golang.org/x/sys/windows"
)

// ========================================================================
// 🪟 WINDOWS API TANHIMLARI
// ========================================================================
var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	
	procGetForegroundWindow    = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW         = user32.NewProc("GetWindowTextW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procEnumWindows            = user32.NewProc("EnumWindows")
	procIsWindowVisible        = user32.NewProc("IsWindowVisible")
	procGetWindowTextLengthW   = user32.NewProc("GetWindowTextLengthW")
	
	procOpenProcess            = kernel32.NewProc("OpenProcess")
	procGetModuleBaseNameW     = user32.NewProc("GetModuleBaseNameW") // psapi.dll'den
	procCloseHandle            = kernel32.NewProc("CloseHandle")
)

const (
	PROCESS_QUERY_INFORMATION = 0x0400
	PROCESS_VM_READ           = 0x0010
	MAX_PATH                  = 260
)

// ========================================================================
// 📦 WINDOW TRACKER YAPISI
// ========================================================================
// WindowTracker: Aktif pencere ve process izleme motoru
type WindowTracker struct {
	Config         *config.KangalConfig
	ctx            context.Context
	cancel         context.CancelFunc
	isRunning      bool
	mu             sync.RWMutex
	
	// Son bilinen durum (cache)
	lastActiveWindow string
	lastActiveProcess string
	lastCheckTime    time.Time
	
	// Event callback (context değiştiğinde çağrılır)
	onWindowChange func(windowTitle, processName string)
	
	// İzlenen uygulama listesi (config'den)
	trackedApps map[string]bool
	
	// Polling aralığı (sensitivity'ye göre değişir)
	pollInterval time.Duration
}

// WindowInfo: Aktif pencere bilgisi
type WindowInfo struct {
	HWND        windows.HWND
	Title       string
	ProcessName string
	ProcessID   uint32
	ThreadID    uint32
	IsVisible   bool
	Timestamp   time.Time
}

// ========================================================================
// 🆕 YENİ: WindowTracker Oluşturucu
// ========================================================================
// NewWindowTracker: Yeni pencere izleyici oluşturur
func NewWindowTracker(ctx context.Context, cfg *config.KangalConfig) *WindowTracker {
	// 🚨 DÜZELTME #1: Nil kontrolleri
	if cfg == nil {
		logger.Error("❌ [WindowTracker] Config nil! WindowTracker oluşturulamadı.")
		return nil
	}
	
	// İzlenen uygulamaları map'e çevir (hızlı lookup için)
	trackedApps := make(map[string]bool)
	for _, app := range cfg.TrackedApps {
		trackedApps[strings.ToLower(app)] = true
	}
	
	// Sensitivity'ye göre polling aralığı belirle
	pollInterval := 2 * time.Second // balanced (varsayılan)
	switch cfg.SensitivityLevel {
	case "low":
		pollInterval = 5 * time.Second
	case "high":
		pollInterval = 500 * time.Millisecond
	}
	
	logger.Debug("🔍 [WindowTracker] Polling aralığı: %v (sensitivity: %s)", 
		pollInterval, cfg.SensitivityLevel)
	
	return &WindowTracker{
		Config:       cfg,
		ctx:          ctx,
		isRunning:    false,
		trackedApps:  trackedApps,
		pollInterval: pollInterval,
	}
}

// ========================================================================
// 🚀 BAŞLATMA / DURDURMA
// ========================================================================
// Start: Window tracker'ı başlatır
func (w *WindowTracker) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	if w.isRunning {
		logger.Warn("⚠️ [WindowTracker] Zaten aktif, başlatma atlandı")
		return nil
	}
	
	// 🚨 DÜZELTME #2: Config enabled kontrolü
	if !w.Config.Enabled {
		logger.Debug("ℹ️ [WindowTracker] Config'de disabled, başlatılmadı")
		return nil
	}
	
	// Context oluştur
	w.ctx, w.cancel = context.WithCancel(context.Background())
	
	w.isRunning = true
	
	// Arka planda izleme goroutine'i başlat
	go w.monitorLoop()
	
	logger.Success("✅ [WindowTracker] Aktif pencere izleme başlatıldı (Interval: %v)", w.pollInterval)
	return nil
}

// Stop: Window tracker'ı güvenli şekilde durdurur
func (w *WindowTracker) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	if !w.isRunning {
		logger.Debug("ℹ️ [WindowTracker] Zaten durmuş")
		return
	}
	
	logger.Info("🛑 [WindowTracker] Pencere izleme durduruluyor...")
	
	if w.cancel != nil {
		w.cancel()
	}
	
	w.isRunning = false
	logger.Success("✅ [WindowTracker] Pencere izleme durduruldu")
}

// ========================================================================
// 🔄 İZLEME DÖNGÜSÜ
// ========================================================================
// monitorLoop: Arka planda sürekli aktif pencereyi izler
func (w *WindowTracker) monitorLoop() {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	
	logger.Debug("🔍 [WindowTracker] Monitor loop başlatıldı")
	
	for {
		select {
		case <-w.ctx.Done():
			logger.Debug("🛑 [WindowTracker] Monitor loop durduruldu (context done)")
			return
		case <-ticker.C:
			w.checkActiveWindow()
		}
	}
}

// checkActiveWindow: Aktif pencereyi kontrol eder ve değişiklik varsa callback çağırır
func (w *WindowTracker) checkActiveWindow() {
	// 🚨 DÜZELTME #3: Windows API çağrısı
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		// Pencere bulunamadı (minimalize veya masaüstü)
		return
	}
	
	// Pencere bilgilerini al
	windowInfo := w.getWindowInfo(windows.HWND(hwnd))
	if windowInfo == nil {
		return
	}
	
	// 🚨 DÜZELTME #4: Değişiklik kontrolü (cache ile karşılaştır)
	w.mu.Lock()
	windowChanged := (windowInfo.Title != w.lastActiveWindow || 
		                 windowInfo.ProcessName != w.lastActiveProcess)
	w.mu.Unlock()
	
	if windowChanged {
		// 🚨 DÜZELTME #5: Tracked apps filtresi
		if !w.isTrackedApp(windowInfo.ProcessName) {
			// İzlenen uygulama değil, logla ama event gönderme
			logger.Debug("🪟 [WindowTracker] İzlenmeyen uygulama: %s (%s)", 
				windowInfo.ProcessName, windowInfo.Title)
			
			// Cache'i yine güncelle (bir sonraki tracked app için)
			w.mu.Lock()
			w.lastActiveWindow = windowInfo.Title
			w.lastActiveProcess = windowInfo.ProcessName
			w.lastCheckTime = time.Now()
			w.mu.Unlock()
			return
		}
		
		// Cache güncelle
		w.mu.Lock()
		w.lastActiveWindow = windowInfo.Title
		w.lastActiveProcess = windowInfo.ProcessName
		w.lastCheckTime = time.Now()
		w.mu.Unlock()
		
		logger.Info("🪟 [WindowTracker] Aktif pencere değişti: %s -> %s (%s)", 
			windowInfo.ProcessName, windowInfo.Title, windowInfo.HWND)
		
		// 🚨 DÜZELTME #6: Callback çağır (eğer tanımlıysa)
		if w.onWindowChange != nil {
			go w.onWindowChange(windowInfo.Title, windowInfo.ProcessName)
		}
	}
}

// ========================================================================
// 🛠️ WINDOWS API HELPER FONKSİYONLARI
// ========================================================================
// getWindowInfo: HWND'den pencere bilgilerini çıkarır
func (w *WindowTracker) getWindowInfo(hwnd windows.HWND) *WindowInfo {
	// 🚨 DÜZELTME #7: Görünür pencere mi kontrol et
	visible, _, _ := procIsWindowVisible.Call(uintptr(hwnd))
	if visible == 0 {
		return nil // Görünmez pencere (arka plan process'i)
	}
	
	// Pencere başlığını al
	title := w.getWindowText(hwnd)
	if title == "" {
		return nil // Başlık yoksa önemli pencere değil
	}
	
	// Process ID'yi al
	var processID uint32
	var threadID uint32
	_, _, _ = procGetWindowThreadProcessId.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&processID)),
	)
	
	// Thread ID'yi de al (ayrı çağrı)
	_, _, _ = procGetWindowThreadProcessId.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&threadID)),
	)
	
	// Process adını al
	processName := w.getProcessName(processID)
	if processName == "" {
		processName = "unknown"
	}
	
	return &WindowInfo{
		HWND:        hwnd,
		Title:       title,
		ProcessName: processName,
		ProcessID:   processID,
		ThreadID:    threadID,
		IsVisible:   visible != 0,
		Timestamp:   time.Now(),
	}
}

// getWindowText: Pencere başlığını Unicode olarak okur
func (w *WindowTracker) getWindowText(hwnd windows.HWND) string {
	// Başlık uzunluğunu al
	length, _, _ := procGetWindowTextLengthW.Call(uintptr(hwnd))
	if length == 0 {
		return ""
	}
	
	// Buffer ayır (Unicode için 2 byte per char)
	buf := make([]uint16, length+1)
	procGetWindowTextW.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(length+1),
	)
	
	return windows.UTF16ToString(buf)
}

// getProcessName: Process ID'den process adını çıkarır
func (w *WindowTracker) getProcessName(processID uint32) string {
	// 🚨 DÜZELTME #8: Process handle'ı aç
	handle, _, _ := procOpenProcess.Call(
		uintptr(PROCESS_QUERY_INFORMATION|PROCESS_VM_READ),
		0,
		uintptr(processID),
	)
	if handle == 0 {
		// Process açılamadı (erişim yok veya kapandı)
		return ""
	}
	defer procCloseHandle.Call(handle)
	
	// 🚨 DÜZELTME #9: psapi.dll'den GetModuleBaseNameW çağır
	psapi := windows.NewLazySystemDLL("psapi.dll")
	procGetModuleBaseNameW := psapi.NewProc("GetModuleBaseNameW")
	
	buf := make([]uint16, MAX_PATH)
	ret, _, _ := procGetModuleBaseNameW.Call(
		handle,
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(MAX_PATH),
	)
	
	if ret == 0 {
		return ""
	}
	
	return windows.UTF16ToString(buf)
}

// ========================================================================
// 🎯 PUBLIC API
// ========================================================================
// IsRunning: Window tracker'ın çalışıp çalışmadığını döndür
func (w *WindowTracker) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.isRunning
}

// GetStatus: Window tracker durum raporunu döndür
func (w *WindowTracker) GetStatus() map[string]interface{} {
	w.mu.RLock()
	defer w.mu.RUnlock()
	
	return map[string]interface{}{
		"is_running":       w.isRunning,
		"last_window":      w.lastActiveWindow,
		"last_process":     w.lastActiveProcess,
		"last_check":       w.lastCheckTime,
		"poll_interval":    w.pollInterval.String(),
		"tracked_apps":     len(w.trackedApps),
		"sensitivity":      w.Config.SensitivityLevel,
	}
}

// GetActiveWindow: Şu anki aktif pencere bilgilerini döndür (thread-safe)
func (w *WindowTracker) GetActiveWindow() (string, string, time.Time) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lastActiveWindow, w.lastActiveProcess, w.lastCheckTime
}

// SetOnWindowChange: Pencere değiştiğinde çağrılacak callback'i ayarla
func (w *WindowTracker) SetOnWindowChange(callback func(windowTitle, processName string)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onWindowChange = callback
}

// SetSensitivity: Hassasiyet seviyesini değiştir (runtime'da)
func (w *WindowTracker) SetSensitivity(level string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	switch level {
	case "low":
		w.pollInterval = 5 * time.Second
	case "balanced":
		w.pollInterval = 2 * time.Second
	case "high":
		w.pollInterval = 500 * time.Millisecond
	default:
		logger.Warn("⚠️ [WindowTracker] Geçersiz sensitivity level: %s", level)
		return
	}
	
	logger.Debug("🔧 [WindowTracker] Sensitivity değiştirildi: %s (interval: %v)", 
		level, w.pollInterval)
}

// ========================================================================
// 🔍 HELPER FONKSİYONLAR
// ========================================================================
// isTrackedApp: Uygulama izlenenler listesinde mi kontrol et
func (w *WindowTracker) isTrackedApp(processName string) bool {
	if len(w.trackedApps) == 0 {
		// Tracked apps listesi boşsa TÜM uygulamaları izle
		return true
	}
	
	// Case-insensitive comparison
	_, exists := w.trackedApps[strings.ToLower(processName)]
	return exists
}

// GetTrackedApps: İzlenen uygulama listesini döndür
func (w *WindowTracker) GetTrackedApps() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	
	apps := make([]string, 0, len(w.trackedApps))
	for app := range w.trackedApps {
		apps = append(apps, app)
	}
	return apps
}

// AddTrackedApp: İzlenen uygulamalara yeni app ekle
func (w *WindowTracker) AddTrackedApp(processName string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	w.trackedApps[strings.ToLower(processName)] = true
	logger.Debug("➕ [WindowTracker] İzlenen uygulama eklendi: %s", processName)
}

// RemoveTrackedApp: İzlenen uygulamalardan app çıkar
func (w *WindowTracker) RemoveTrackedApp(processName string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	delete(w.trackedApps, strings.ToLower(processName))
	logger.Debug("➖ [WindowTracker] İzlenen uygulama çıkarıldı: %s", processName)
}

// TriggerScan: Manuel tarama tetikle (debug/test için)
func (w *WindowTracker) TriggerScan() {
	logger.Debug("🔍 [WindowTracker] Manuel tarama tetiklendi")
	w.checkActiveWindow()
}

// ========================================================================
// 🪟 CONTEXT DETECTION (Uygulama-Spesifik Bağlam)
// ========================================================================
// GetAppContext: Uygulama adına göre bağlam bilgisi döndür
func (w *WindowTracker) GetAppContext(processName string) string {
	processName = strings.ToLower(processName)
	
	// Uygulama tiplerini tanımla
	appContexts := map[string]string{
		"code.exe":           "vscode",
		"notepad.exe":        "text_editor",
		"notepad++.exe":      "text_editor",
		"sublime_text.exe":   "text_editor",
		"chrome.exe":         "browser",
		"msedge.exe":         "browser",
		"firefox.exe":        "browser",
		"python.exe":         "python_runtime",
		"go.exe":             "go_runtime",
		"node.exe":           "node_runtime",
		"powershell.exe":     "terminal",
		"cmd.exe":            "terminal",
		"wt.exe":             "terminal",
		"conhost.exe":        "terminal",
		"explorer.exe":       "file_explorer",
		"outlook.exe":        "email_client",
		"winword.exe":        "word_processor",
		"excel.exe":          "spreadsheet",
	}
	
	if ctx, exists := appContexts[processName]; exists {
		return ctx
	}
	
	return "unknown"
}

// IsCodingApp: Kullanıcı kod yazıyor mu?
func (w *WindowTracker) IsCodingApp() bool {
	_, processName, _ := w.GetActiveWindow()
	ctx := w.GetAppContext(processName)
	
	codingContexts := []string{"vscode", "text_editor", "python_runtime", "go_runtime", "node_runtime"}
	
	for _, codingCtx := range codingContexts {
		if ctx == codingCtx {
			return true
		}
	}
	
	return false
}

// IsBrowserApp: Kullanıcı tarayıcı kullanıyor mu?
func (w *WindowTracker) IsBrowserApp() bool {
	_, processName, _ := w.GetActiveWindow()
	return w.GetAppContext(processName) == "browser"
}

// IsTerminalApp: Kullanıcı terminal kullanıyor mu?
func (w *WindowTracker) IsTerminalApp() bool {
	_, processName, _ := w.GetActiveWindow()
	return w.GetAppContext(processName) == "terminal"
}
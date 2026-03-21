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

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	psapi    = windows.NewLazySystemDLL("psapi.dll")
	
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindowTextLengthW     = user32.NewProc("GetWindowTextLengthW")
	
	procOpenProcess           = kernel32.NewProc("OpenProcess")
	procCloseHandle           = kernel32.NewProc("CloseHandle")
	procGetModuleBaseNameW    = psapi.NewProc("GetModuleBaseNameW")
)

const (
	PROCESS_QUERY_INFORMATION = 0x0400
	PROCESS_VM_READ           = 0x0010
	MAX_PATH                  = 260
)

type WindowTracker struct {
	Config         *config.KangalConfig
	ctx            context.Context
	cancel         context.CancelFunc
	isRunning      bool
	mu             sync.RWMutex
	
	lastActiveWindow  string
	lastActiveProcess string
	lastCheckTime     time.Time
	
	onWindowChange func(windowTitle, processName string)
	trackedApps    map[string]bool
	pollInterval   time.Duration
}

type WindowInfo struct {
	HWND        windows.HWND
	Title       string
	ProcessName string
	ProcessID   uint32
	ThreadID    uint32
	IsVisible   bool
	Timestamp   time.Time
}

func NewWindowTracker(ctx context.Context, cfg *config.KangalConfig) *WindowTracker {
	if cfg == nil {
		logger.Error("❌ [WindowTracker] Config nil! WindowTracker oluşturulamadı.")
		return nil
	}
	
	trackedApps := make(map[string]bool)
	for _, app := range cfg.TrackedApps {
		trackedApps[strings.ToLower(app)] = true
	}
	
	pollInterval := 2 * time.Second
	switch cfg.SensitivityLevel {
	case "low":
		pollInterval = 5 * time.Second
	case "high":
		pollInterval = 500 * time.Millisecond
	}
	
	return &WindowTracker{
		Config:       cfg,
		ctx:          ctx,
		isRunning:    false,
		trackedApps:  trackedApps,
		pollInterval: pollInterval,
	}
}

func (w *WindowTracker) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	if w.isRunning {
		return nil
	}
	
	if !w.Config.Enabled {
		return nil
	}
	
	w.ctx, w.cancel = context.WithCancel(context.Background())
	w.isRunning = true
	
	go w.monitorLoop()
	
	logger.Success("✅ [WindowTracker] Aktif pencere izleme başlatıldı (Interval: %v)", w.pollInterval)
	return nil
}

func (w *WindowTracker) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	if !w.isRunning {
		return
	}
	
	if w.cancel != nil {
		w.cancel()
	}
	
	w.isRunning = false
	logger.Success("✅ [WindowTracker] Pencere izleme durduruldu")
}

func (w *WindowTracker) monitorLoop() {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.checkActiveWindow()
		}
	}
}

func (w *WindowTracker) checkActiveWindow() {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return
	}
	
	windowInfo := w.getWindowInfo(windows.HWND(hwnd))
	if windowInfo == nil {
		return
	}
	
	w.mu.RLock()
	windowChanged := (windowInfo.Title != w.lastActiveWindow || windowInfo.ProcessName != w.lastActiveProcess)
	w.mu.RUnlock()
	
	if windowChanged {
		w.mu.Lock()
		w.lastActiveWindow = windowInfo.Title
		w.lastActiveProcess = windowInfo.ProcessName
		w.lastCheckTime = time.Now()
		w.mu.Unlock()

		if !w.isTrackedApp(windowInfo.ProcessName) {
			logger.Debug("🪟 [WindowTracker] İzlenmeyen uygulama: %s (%s)", windowInfo.ProcessName, windowInfo.Title)
			return
		}
		
		logger.Info("🪟 [WindowTracker] Aktif pencere değişti: %s -> %s", windowInfo.ProcessName, windowInfo.Title)
		
		if w.onWindowChange != nil {
			go w.onWindowChange(windowInfo.Title, windowInfo.ProcessName)
		}
	}
}

func (w *WindowTracker) getWindowInfo(hwnd windows.HWND) *WindowInfo {
	visible, _, _ := procIsWindowVisible.Call(uintptr(hwnd))
	if visible == 0 {
		return nil
	}
	
	title := w.getWindowText(hwnd)
	if title == "" {
		return nil
	}
	
	var processID uint32
	threadID, _, _ := procGetWindowThreadProcessId.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&processID)),
	)
	
	processName := w.getProcessName(processID)
	if processName == "" {
		processName = "unknown"
	}
	
	return &WindowInfo{
		HWND:        hwnd,
		Title:       title,
		ProcessName: processName,
		ProcessID:   processID,
		ThreadID:    uint32(threadID),
		IsVisible:   true,
		Timestamp:   time.Now(),
	}
}

func (w *WindowTracker) getWindowText(hwnd windows.HWND) string {
	length, _, _ := procGetWindowTextLengthW.Call(uintptr(hwnd))
	if length == 0 {
		return ""
	}
	
	buf := make([]uint16, length+1)
	procGetWindowTextW.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(length+1),
	)
	
	return windows.UTF16ToString(buf)
}

func (w *WindowTracker) getProcessName(processID uint32) string {
	handle, _, _ := procOpenProcess.Call(
		uintptr(PROCESS_QUERY_INFORMATION|PROCESS_VM_READ),
		0,
		uintptr(processID),
	)
	if handle == 0 {
		return ""
	}
	defer procCloseHandle.Call(handle)
	
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

func (w *WindowTracker) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.isRunning
}

func (w *WindowTracker) GetStatus() map[string]interface{} {
	w.mu.RLock()
	defer w.mu.RUnlock()
	
	return map[string]interface{}{
		"is_running":    w.isRunning,
		"last_window":   w.lastActiveWindow,
		"last_process":  w.lastActiveProcess,
		"last_check":    w.lastCheckTime,
		"poll_interval": w.pollInterval.String(),
		"tracked_apps":  len(w.trackedApps),
		"sensitivity":   w.Config.SensitivityLevel,
	}
}

func (w *WindowTracker) GetActiveWindow() (string, string, time.Time) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lastActiveWindow, w.lastActiveProcess, w.lastCheckTime
}

func (w *WindowTracker) SetOnWindowChange(callback func(windowTitle, processName string)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onWindowChange = callback
}

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
		return
	}
}

func (w *WindowTracker) isTrackedApp(processName string) bool {
	if len(w.trackedApps) == 0 {
		return true
	}
	_, exists := w.trackedApps[strings.ToLower(processName)]
	return exists
}

func (w *WindowTracker) TriggerScan() {
	w.checkActiveWindow()
}

func (w *WindowTracker) GetAppContext(processName string) string {
	processName = strings.ToLower(processName)
	appContexts := map[string]string{
		"code.exe":           "vscode",
		"notepad.exe":        "text_editor",
		"chrome.exe":         "browser",
		"msedge.exe":         "browser",
		"firefox.exe":        "browser",
		"powershell.exe":     "terminal",
		"cmd.exe":            "terminal",
		"wt.exe":             "terminal",
		"explorer.exe":       "file_explorer",
	}
	if ctx, exists := appContexts[processName]; exists {
		return ctx
	}
	return "unknown"
}
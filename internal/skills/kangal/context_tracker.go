package kangal

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/config"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

type UserContext struct {
	ActiveApp      string            
	ActiveWindow   string           
	ActiveFile     string           
	ActivityType   string            
	IsIdle         bool             
	IdleSince      time.Time        
	LastActiveTime time.Time         
	SessionStart   time.Time        
	Metadata       map[string]interface{}
	mu             sync.RWMutex
}

type ActivityEvent struct {
	Type      string            
	Timestamp time.Time
	Data      map[string]interface{}
}

type ContextTracker struct {
	Config        *config.KangalConfig
	ctx           context.Context
	cancel        context.CancelFunc
	isRunning     bool
	mu            sync.RWMutex
	
	currentContext *UserContext
	
	activityHistory []ActivityEvent
	historyMu       sync.RWMutex
	
	onContextChange func(ctx *UserContext)
	
	idleThreshold time.Duration
	
	stats ContextStats
	statsMu sync.RWMutex
}

type ContextStats struct {
	TotalContextSwitches int
	TotalIdleEvents      int
	TotalActiveEvents    int
	LastAppChange        time.Time
	LastFileChange       time.Time
	AvgSessionDuration   time.Duration
}

func NewContextTracker(ctx context.Context, cfg *config.KangalConfig) *ContextTracker {
	if cfg == nil {
		logger.Error("❌ [ContextTracker] Config nil! Oluşturulamadı.")
		return nil
	}
	
	idleThreshold := 5 * time.Minute 
	switch cfg.SensitivityLevel {
	case "low":
		idleThreshold = 10 * time.Minute
	case "high":
		idleThreshold = 2 * time.Minute
	}
	
	logger.Debug("⏱️ [ContextTracker] Idle threshold: %v (sensitivity: %s)", 
		idleThreshold, cfg.SensitivityLevel)
	
	return &ContextTracker{
		Config:        cfg,
		ctx:           ctx,
		isRunning:     false,
		currentContext: &UserContext{
			ActiveApp:      "unknown",
			ActiveWindow:   "unknown",
			ActiveFile:     "",
			ActivityType:   "unknown",
			IsIdle:         false,
			LastActiveTime: time.Now(),
			SessionStart:   time.Now(),
			Metadata:       make(map[string]interface{}),
		},
		activityHistory: make([]ActivityEvent, 0, 100),
		idleThreshold:   idleThreshold,
		stats: ContextStats{
			LastAppChange: time.Now(),
			LastFileChange: time.Now(),
		},
	}
}

func (c *ContextTracker) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.isRunning {
		logger.Warn("⚠️ [ContextTracker] Zaten aktif, başlatma atlandı")
		return nil
	}
	
	if !c.Config.Enabled {
		logger.Debug("ℹ️ [ContextTracker] Config'de disabled, başlatılmadı")
		return nil
	}
	
	c.ctx, c.cancel = context.WithCancel(context.Background())
	
	c.isRunning = true
	
	go c.monitorIdle()
	
	logger.Success("✅ [ContextTracker] Bağlam takibi başlatıldı (Idle: %v)", c.idleThreshold)
	return nil
}

func (c *ContextTracker) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if !c.isRunning {
		logger.Debug("ℹ️ [ContextTracker] Zaten durmuş")
		return
	}
	
	logger.Info("🛑 [ContextTracker] Bağlam takibi durduruluyor...")
	
	if c.cancel != nil {
		c.cancel()
	}
	
	c.isRunning = false
	logger.Success("✅ [ContextTracker] Bağlam takibi durduruldu")
}

func (c *ContextTracker) monitorIdle() {
	ticker := time.NewTicker(30 * time.Second) 
	defer ticker.Stop()
	
	logger.Debug("🔍 [ContextTracker] Idle monitor başlatıldı")
	
	for {
		select {
		case <-c.ctx.Done():
			logger.Debug("🛑 [ContextTracker] Idle monitor durduruldu")
			return
		case <-ticker.C:
			c.checkIdleStatus()
		}
	}
}

func (c *ContextTracker) checkIdleStatus() {
	c.mu.RLock()
	lastActive := c.currentContext.LastActiveTime
	c.mu.RUnlock()
	
	idleDuration := time.Since(lastActive)
	isIdle := idleDuration > c.idleThreshold
	
	c.mu.Lock()
	wasIdle := c.currentContext.IsIdle
	
	if isIdle && !wasIdle {
		c.currentContext.IsIdle = true
		c.currentContext.IdleSince = time.Now()
		c.addActivityEvent(ActivityEvent{
			Type:      "idle",
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"idle_duration": idleDuration.String(),
			},
		})
		
		c.statsMu.Lock()
		c.stats.TotalIdleEvents++
		c.statsMu.Unlock()
		
		logger.Info("😴 [ContextTracker] Kullanıcı idle duruma geçti (%v sonra)", idleDuration)
	} else if !isIdle && wasIdle {
		c.currentContext.IsIdle = false
		c.currentContext.IdleSince = time.Time{}
		c.addActivityEvent(ActivityEvent{
			Type:      "active",
			Timestamp: time.Now(),
			Data:      map[string]interface{}{},
		})
		
		c.statsMu.Lock()
		c.stats.TotalActiveEvents++
		c.statsMu.Unlock()
		
		logger.Info("😊 [ContextTracker] Kullanıcı aktif duruma geçti")
	}
	c.mu.Unlock()
}

func (c *ContextTracker) UpdateActiveWindow(windowTitle, processName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	oldApp := c.currentContext.ActiveApp
	c.currentContext.ActiveApp = processName
	c.currentContext.ActiveWindow = windowTitle
	c.currentContext.LastActiveTime = time.Now()
	c.currentContext.ActivityType = c.determineActivityType(processName)
	if oldApp != processName {
		c.addActivityEvent(ActivityEvent{
			Type:      "app_change",
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"old_app": oldApp,
				"new_app": processName,
				"window":  windowTitle,
			},
		})
		c.statsMu.Lock()
		c.stats.TotalContextSwitches++
		c.stats.LastAppChange = time.Now()
		c.statsMu.Unlock()
		
		logger.Info("🪟 [ContextTracker] Uygulama değişti: %s -> %s", oldApp, processName)
	}
	if c.onContextChange != nil {
		go c.onContextChange(c.currentContext)
	}
}

func (c *ContextTracker) UpdateFileActivity(filePath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	oldFile := c.currentContext.ActiveFile
	
	c.currentContext.ActiveFile = filePath
	c.currentContext.LastActiveTime = time.Now()
	
	if oldFile != filePath {
		c.addActivityEvent(ActivityEvent{
			Type:      "file_change",
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"old_file": oldFile,
				"new_file": filePath,
			},
		})
		
		c.statsMu.Lock()
		c.stats.LastFileChange = time.Now()
		c.statsMu.Unlock()
		
		logger.Info("📁 [ContextTracker] Aktif dosya değişti: %s", filePath)
	}
}

func (c *ContextTracker) SetMetadata(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.currentContext.Metadata[key] = value
	logger.Debug("🏷️ [ContextTracker] Metadata eklendi: %s = %v", key, value)
}

func (c *ContextTracker) GetMetadata(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	val, exists := c.currentContext.Metadata[key]
	return val, exists
}

func (c *ContextTracker) determineActivityType(processName string) string {
	processName = strings.ToLower(processName)
	
	appTypes := map[string]string{
		"code.exe":           "coding",
		"notepad++.exe":      "coding",
		"sublime_text.exe":   "coding",
		"atom.exe":           "coding",
		"visualstudio.exe":   "coding",
		"jetbrains":          "coding", 
		"chrome.exe":         "browsing",
		"msedge.exe":         "browsing",
		"firefox.exe":        "browsing",
		"brave.exe":          "browsing",
		"powershell.exe":     "terminal",
		"cmd.exe":            "terminal",
		"wt.exe":             "terminal", 
		"conhost.exe":        "terminal",
		"bash.exe":           "terminal",
		"explorer.exe":       "explorer",
		"winword.exe":        "office",
		"excel.exe":          "office",
		"powerpnt.exe":       "office",
		"outlook.exe":        "office",
		"python.exe":         "coding",
		"go.exe":             "coding",
		"node.exe":           "coding",
		"java.exe":           "coding",
	}
	
	for keyword, activityType := range appTypes {
		if strings.Contains(processName, keyword) {
			return activityType
		}
	}
	
	return "unknown"
}

func (c *ContextTracker) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isRunning
}

func (c *ContextTracker) GetStatus() map[string]interface{} {
	c.mu.RLock()
	c.statsMu.RLock()
	defer c.mu.RUnlock()
	defer c.statsMu.RUnlock()
	
	return map[string]interface{}{
		"is_running":         c.isRunning,
		"active_app":         c.currentContext.ActiveApp,
		"active_window":      c.currentContext.ActiveWindow,
		"active_file":        c.currentContext.ActiveFile,
		"activity_type":      c.currentContext.ActivityType,
		"is_idle":            c.currentContext.IsIdle,
		"idle_since":         c.currentContext.IdleSince,
		"last_active":        c.currentContext.LastActiveTime,
		"session_start":      c.currentContext.SessionStart,
		"total_switches":     c.stats.TotalContextSwitches,
		"total_idle_events":  c.stats.TotalIdleEvents,
		"total_active_events": c.stats.TotalActiveEvents,
		"last_app_change":    c.stats.LastAppChange,
		"last_file_change":   c.stats.LastFileChange,
		"idle_threshold":     c.idleThreshold.String(),
	}
}

func (c *ContextTracker) GetCurrentContext() *UserContext {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return &UserContext{
		ActiveApp:      c.currentContext.ActiveApp,
		ActiveWindow:   c.currentContext.ActiveWindow,
		ActiveFile:     c.currentContext.ActiveFile,
		ActivityType:   c.currentContext.ActivityType,
		IsIdle:         c.currentContext.IsIdle,
		IdleSince:      c.currentContext.IdleSince,
		LastActiveTime: c.currentContext.LastActiveTime,
		SessionStart:   c.currentContext.SessionStart,
		Metadata:       c.currentContext.Metadata,
	}
}

func (c *ContextTracker) GetActivityHistory(limit int) []ActivityEvent {
	c.historyMu.RLock()
	defer c.historyMu.RUnlock()
	
	if limit <= 0 || limit > len(c.activityHistory) {
		limit = len(c.activityHistory)
	}
	
	start := len(c.activityHistory) - limit
	if start < 0 {
		start = 0
	}
	
	return c.activityHistory[start:]
}

func (c *ContextTracker) GetStats() ContextStats {
	c.statsMu.RLock()
	defer c.statsMu.RUnlock()
	
	return c.stats
}


func (c *ContextTracker) SetOnContextChange(callback func(ctx *UserContext)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onContextChange = callback
}


func (c *ContextTracker) addActivityEvent(event ActivityEvent) {
	c.historyMu.Lock()
	defer c.historyMu.Unlock()
	
	c.activityHistory = append(c.activityHistory, event)
	
	if len(c.activityHistory) > 100 {
		c.activityHistory = c.activityHistory[1:]
	}
}

func (c *ContextTracker) SetSensitivity(level string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	switch level {
	case "low":
		c.idleThreshold = 10 * time.Minute
	case "balanced":
		c.idleThreshold = 5 * time.Minute
	case "high":
		c.idleThreshold = 2 * time.Minute
	default:
		logger.Warn("⚠️ [ContextTracker] Geçersiz sensitivity level: %s", level)
		return
	}
	
	logger.Debug("🔧 [ContextTracker] Sensitivity değiştirildi: %s (idle: %v)", 
		level, c.idleThreshold)
}

func (c *ContextTracker) ResetSession() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.currentContext.SessionStart = time.Now()
	c.currentContext.LastActiveTime = time.Now()
	c.currentContext.IsIdle = false
	c.currentContext.IdleSince = time.Time{}
	
	logger.Info("🔄 [ContextTracker] Oturum sıfırlandı")
}

func (c *ContextTracker) GetSessionDuration() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return time.Since(c.currentContext.SessionStart)
}

func (c *ContextTracker) IsCodingApp() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return c.currentContext.ActivityType == "coding"
}

func (c *ContextTracker) IsBrowsingApp() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return c.currentContext.ActivityType == "browsing"
}

func (c *ContextTracker) IsTerminalApp() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return c.currentContext.ActivityType == "terminal"
}

func (c *ContextTracker) GetActiveFile() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return c.currentContext.ActiveFile
}

func (c *ContextTracker) GetActiveApp() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return c.currentContext.ActiveApp
}

func (c *ContextTracker) IsUserPresent() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return !c.currentContext.IsIdle
}

func (c *ContextTracker) ClearHistory() {
	c.historyMu.Lock()
	defer c.historyMu.Unlock()
	
	count := len(c.activityHistory)
	c.activityHistory = make([]ActivityEvent, 0, 100)
	logger.Debug("🧹 [ContextTracker] Aktivite geçmişi temizlendi: %d event", count)
}

func (c *ContextTracker) TriggerContextUpdate() {
	c.mu.RLock()
	ctx := c.currentContext
	c.mu.RUnlock()
	
	if c.onContextChange != nil {
		go c.onContextChange(ctx)
	}
	
	logger.Debug("🔍 [ContextTracker] Manuel bağlam güncellemesi tetiklendi")
}
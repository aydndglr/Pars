// internal/skills/kangal/context_tracker.go
// 🚀 KANGAL - CONTEXT TRACKER (Kullanıcı Bağlam Takibi)
// 📅 Oluşturulma: 2026-03-07 (Pars V5 - Kangal Edition)
// ⚠️ DİKKAT: Window Tracker ile entegre çalışır, kullanıcı aktivitesini izler

package kangal

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/config"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

// ========================================================================
// 📊 KULLANICI BAĞLAM YAPILARI
// ========================================================================
// UserContext: Kullanıcının mevcut aktivite bağlamını tutar
type UserContext struct {
	ActiveApp      string            // Aktif uygulama (Code.exe, chrome.exe, vs.)
	ActiveWindow   string            // Aktif pencere başlığı
	ActiveFile     string            // Üzerinde çalışılan dosya
	ActivityType   string            // "coding", "browsing", "terminal", "explorer", "unknown"
	IsIdle         bool              // Kullanıcı boşta mı?
	IdleSince      time.Time         // Ne zamandır boşta
	LastActiveTime time.Time         // Son aktivite zamanı
	SessionStart   time.Time         // Oturum başlangıcı
	Metadata       map[string]interface{} // Ek bağlam bilgisi
	mu             sync.RWMutex
}

// ActivityEvent: Aktivite değişikliği eventi
type ActivityEvent struct {
	Type      string            // "app_change", "file_change", "idle", "active"
	Timestamp time.Time
	Data      map[string]interface{}
}

// ========================================================================
// 📦 CONTEXT TRACKER YAPISI
// ========================================================================
// ContextTracker: Kullanıcı bağlamını ve aktivitesini takip eden modül
type ContextTracker struct {
	Config        *config.KangalConfig
	ctx           context.Context
	cancel        context.CancelFunc
	isRunning     bool
	mu            sync.RWMutex
	
	// Mevcut bağlam
	currentContext *UserContext
	
	// Aktivite geçmişi (son 100 event)
	activityHistory []ActivityEvent
	historyMu       sync.RWMutex
	
	// Event callback (bağlam değiştiğinde çağrılır)
	onContextChange func(ctx *UserContext)
	
	// Idle threshold (sensitivity'ye göre değişir)
	idleThreshold time.Duration
	
	// İstatistikler
	stats ContextStats
	statsMu sync.RWMutex
}

// ContextStats: Bağlam takip istatistikleri
type ContextStats struct {
	TotalContextSwitches int
	TotalIdleEvents      int
	TotalActiveEvents    int
	LastAppChange        time.Time
	LastFileChange       time.Time
	AvgSessionDuration   time.Duration
}

// ========================================================================
// 🆕 YENİ: ContextTracker Oluşturucu
// ========================================================================
// NewContextTracker: Yeni bağlam takipçisi oluşturur
func NewContextTracker(ctx context.Context, cfg *config.KangalConfig) *ContextTracker {
	// 🚨 DÜZELTME #1: Nil kontrolleri
	if cfg == nil {
		logger.Error("❌ [ContextTracker] Config nil! Oluşturulamadı.")
		return nil
	}
	
	// Sensitivity'ye göre idle threshold belirle
	idleThreshold := 5 * time.Minute // balanced (varsayılan)
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

// ========================================================================
// 🚀 BAŞLATMA / DURDURMA
// ========================================================================
// Start: Context Tracker'ı başlatır
func (c *ContextTracker) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.isRunning {
		logger.Warn("⚠️ [ContextTracker] Zaten aktif, başlatma atlandı")
		return nil
	}
	
	// 🚨 DÜZELTME #2: Config enabled kontrolü
	if !c.Config.Enabled {
		logger.Debug("ℹ️ [ContextTracker] Config'de disabled, başlatılmadı")
		return nil
	}
	
	// Context oluştur
	c.ctx, c.cancel = context.WithCancel(context.Background())
	
	c.isRunning = true
	
	// Arka planda idle monitor başlat
	go c.monitorIdle()
	
	logger.Success("✅ [ContextTracker] Bağlam takibi başlatıldı (Idle: %v)", c.idleThreshold)
	return nil
}

// Stop: Context Tracker'ı güvenli şekilde durdurur
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

// ========================================================================
// 🔍 IDLE MONITOR
// ========================================================================
// monitorIdle: Kullanıcı idle durumunu periyodik kontrol eder
func (c *ContextTracker) monitorIdle() {
	ticker := time.NewTicker(30 * time.Second) // Her 30 saniye kontrol
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

// checkIdleStatus: Kullanıcı idle mı kontrol eder
func (c *ContextTracker) checkIdleStatus() {
	c.mu.RLock()
	lastActive := c.currentContext.LastActiveTime
	c.mu.RUnlock()
	
	idleDuration := time.Since(lastActive)
	isIdle := idleDuration > c.idleThreshold
	
	c.mu.Lock()
	wasIdle := c.currentContext.IsIdle
	
	if isIdle && !wasIdle {
		// Yeni idle durumu
		c.currentContext.IsIdle = true
		c.currentContext.IdleSince = time.Now()
		
		// Event kaydet
		c.addActivityEvent(ActivityEvent{
			Type:      "idle",
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"idle_duration": idleDuration.String(),
			},
		})
		
		// İstatistik güncelle
		c.statsMu.Lock()
		c.stats.TotalIdleEvents++
		c.statsMu.Unlock()
		
		logger.Info("😴 [ContextTracker] Kullanıcı idle duruma geçti (%v sonra)", idleDuration)
	} else if !isIdle && wasIdle {
		// Idle'dan aktif duruma geçiş
		c.currentContext.IsIdle = false
		c.currentContext.IdleSince = time.Time{}
		
		// Event kaydet
		c.addActivityEvent(ActivityEvent{
			Type:      "active",
			Timestamp: time.Now(),
			Data:      map[string]interface{}{},
		})
		
		// İstatistik güncelle
		c.statsMu.Lock()
		c.stats.TotalActiveEvents++
		c.statsMu.Unlock()
		
		logger.Info("😊 [ContextTracker] Kullanıcı aktif duruma geçti")
	}
	c.mu.Unlock()
}

// ========================================================================
// 🎯 PUBLIC API - BAĞLAM GÜNCELLEME
// ========================================================================
// UpdateActiveWindow: Aktif pencere değiştiğinde çağrılır (Window Tracker'dan)
func (c *ContextTracker) UpdateActiveWindow(windowTitle, processName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	oldApp := c.currentContext.ActiveApp
	//_ := c.currentContext.ActiveWindow
	
	// Bağlam güncelle
	c.currentContext.ActiveApp = processName
	c.currentContext.ActiveWindow = windowTitle
	c.currentContext.LastActiveTime = time.Now()
	
	// Aktivite tipini belirle
	c.currentContext.ActivityType = c.determineActivityType(processName)
	
	// App değişti mi?
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
		
		// İstatistik güncelle
		c.statsMu.Lock()
		c.stats.TotalContextSwitches++
		c.stats.LastAppChange = time.Now()
		c.statsMu.Unlock()
		
		logger.Info("🪟 [ContextTracker] Uygulama değişti: %s -> %s", oldApp, processName)
	}
	
	// Callback çağır (eğer tanımlıysa)
	if c.onContextChange != nil {
		go c.onContextChange(c.currentContext)
	}
}

// UpdateFileActivity: Dosya aktivitesi değiştiğinde çağrılır
func (c *ContextTracker) UpdateFileActivity(filePath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	oldFile := c.currentContext.ActiveFile
	
	c.currentContext.ActiveFile = filePath
	c.currentContext.LastActiveTime = time.Now()
	
	// Dosya değişti mi?
	if oldFile != filePath {
		c.addActivityEvent(ActivityEvent{
			Type:      "file_change",
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"old_file": oldFile,
				"new_file": filePath,
			},
		})
		
		// İstatistik güncelle
		c.statsMu.Lock()
		c.stats.LastFileChange = time.Now()
		c.statsMu.Unlock()
		
		logger.Info("📁 [ContextTracker] Aktif dosya değişti: %s", filePath)
	}
}

// SetMetadata: Ek bağlam bilgisi ekle
func (c *ContextTracker) SetMetadata(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.currentContext.Metadata[key] = value
	logger.Debug("🏷️ [ContextTracker] Metadata eklendi: %s = %v", key, value)
}

// GetMetadata: Metadata değeri al
func (c *ContextTracker) GetMetadata(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	val, exists := c.currentContext.Metadata[key]
	return val, exists
}

// ========================================================================
// 🧠 AKTİVİTE TESPİTİ
// ========================================================================
// determineActivityType: Uygulama adına göre aktivite tipini belirle
func (c *ContextTracker) determineActivityType(processName string) string {
	processName = strings.ToLower(processName)
	
	// Uygulama tiplerini tanımla
	appTypes := map[string]string{
		// Kod Editörleri / IDE'ler
		"code.exe":           "coding",
		"notepad++.exe":      "coding",
		"sublime_text.exe":   "coding",
		"atom.exe":           "coding",
		"visualstudio.exe":   "coding",
		"jetbrains":          "coding", // IntelliJ, PyCharm, vs.
		
		// Tarayıcılar
		"chrome.exe":         "browsing",
		"msedge.exe":         "browsing",
		"firefox.exe":        "browsing",
		"brave.exe":          "browsing",
		
		// Terminal / Shell
		"powershell.exe":     "terminal",
		"cmd.exe":            "terminal",
		"wt.exe":             "terminal", // Windows Terminal
		"conhost.exe":        "terminal",
		"bash.exe":           "terminal",
		
		// Dosya Gezgini
		"explorer.exe":       "explorer",
		
		// Ofis Uygulamaları
		"winword.exe":        "office",
		"excel.exe":          "office",
		"powerpnt.exe":       "office",
		"outlook.exe":        "office",
		
		// Runtime'lar (kod çalıştırma)
		"python.exe":         "coding",
		"go.exe":             "coding",
		"node.exe":           "coding",
		"java.exe":           "coding",
	}
	
	// Partial match (örn: "idea64.exe" → "jetbrains")
	for keyword, activityType := range appTypes {
		if strings.Contains(processName, keyword) {
			return activityType
		}
	}
	
	return "unknown"
}

// ========================================================================
// 📊 GETTERS & STATUS
// ========================================================================
// IsRunning: Context Tracker'ın çalışıp çalışmadığını döndür
func (c *ContextTracker) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isRunning
}

// GetStatus: Context Tracker durum raporunu döndür
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

// GetCurrentContext: Mevcut kullanıcı bağlamını döndür (thread-safe copy)
func (c *ContextTracker) GetCurrentContext() *UserContext {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	// Deep copy döndür (thread-safe)
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

// GetActivityHistory: Son aktivite geçmişini döndür
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

// GetStats: İstatistikleri döndür
func (c *ContextTracker) GetStats() ContextStats {
	c.statsMu.RLock()
	defer c.statsMu.RUnlock()
	
	return c.stats
}

// ========================================================================
// 🎯 CALLBACK & EVENT YÖNETİMİ
// ========================================================================
// SetOnContextChange: Bağlam değiştiğinde çağrılacak callback'i ayarla
func (c *ContextTracker) SetOnContextChange(callback func(ctx *UserContext)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onContextChange = callback
}

// addActivityEvent: Aktivite eventini geçmişe ekle
func (c *ContextTracker) addActivityEvent(event ActivityEvent) {
	c.historyMu.Lock()
	defer c.historyMu.Unlock()
	
	c.activityHistory = append(c.activityHistory, event)
	
	// Son 100 event'i tut (memory leak önleme)
	if len(c.activityHistory) > 100 {
		c.activityHistory = c.activityHistory[1:]
	}
}

// ========================================================================
// 🔧 HELPER FONKSİYONLAR
// ========================================================================
// SetSensitivity: Hassasiyet seviyesini değiştir (runtime'da)
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

// ResetSession: Oturumu sıfırla (yeni session başlat)
func (c *ContextTracker) ResetSession() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.currentContext.SessionStart = time.Now()
	c.currentContext.LastActiveTime = time.Now()
	c.currentContext.IsIdle = false
	c.currentContext.IdleSince = time.Time{}
	
	logger.Info("🔄 [ContextTracker] Oturum sıfırlandı")
}

// GetSessionDuration: Mevcut oturum süresini döndür
func (c *ContextTracker) GetSessionDuration() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return time.Since(c.currentContext.SessionStart)
}

// IsCodingApp: Kullanıcı şu an kod yazıyor mu?
func (c *ContextTracker) IsCodingApp() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return c.currentContext.ActivityType == "coding"
}

// IsBrowsingApp: Kullanıcı şu an tarayıcı kullanıyor mu?
func (c *ContextTracker) IsBrowsingApp() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return c.currentContext.ActivityType == "browsing"
}

// IsTerminalApp: Kullanıcı şu an terminal kullanıyor mu?
func (c *ContextTracker) IsTerminalApp() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return c.currentContext.ActivityType == "terminal"
}

// GetActiveFile: Şu an aktif dosyayı döndür
func (c *ContextTracker) GetActiveFile() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return c.currentContext.ActiveFile
}

// GetActiveApp: Şu an aktif uygulamayı döndür
func (c *ContextTracker) GetActiveApp() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return c.currentContext.ActiveApp
}

// IsUserPresent: Kullanıcı bilgisayar başında mı? (idle değil mi?)
func (c *ContextTracker) IsUserPresent() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return !c.currentContext.IsIdle
}

// ClearHistory: Aktivite geçmişini temizle
func (c *ContextTracker) ClearHistory() {
	c.historyMu.Lock()
	defer c.historyMu.Unlock()
	
	count := len(c.activityHistory)
	c.activityHistory = make([]ActivityEvent, 0, 100)
	logger.Debug("🧹 [ContextTracker] Aktivite geçmişi temizlendi: %d event", count)
}

// TriggerContextUpdate: Manuel bağlam güncellemesi tetikle (debug/test için)
func (c *ContextTracker) TriggerContextUpdate() {
	c.mu.RLock()
	ctx := c.currentContext
	c.mu.RUnlock()
	
	if c.onContextChange != nil {
		go c.onContextChange(ctx)
	}
	
	logger.Debug("🔍 [ContextTracker] Manuel bağlam güncellemesi tetiklendi")
}
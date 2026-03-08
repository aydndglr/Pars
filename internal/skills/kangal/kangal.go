// internal/skills/kangal/kangal.go
// 🚀 DÜZELTME: Watchdog oluştururken primaryConfig parametresi eklendi

package kangal

import (
	"context"
	"fmt"
	"sync"
	//"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/config"
	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

// ========================================================================
// 🐕 KANGAL YAPISI
// ========================================================================
type Kangal struct {
	Config         *config.KangalConfig
	PrimaryConfig  *config.Config       
	Agent          kernel.Agent
	EventChan      chan<- string
	ctx            context.Context
	cancel         context.CancelFunc
	isRunning      bool
	mu             sync.RWMutex
	
	windowTracker  *WindowTracker
	errorDetector  *ErrorDetector
	notification   *NotificationEngine
	watchdog       *Watchdog
	contextTracker *ContextTracker
}

// ========================================================================
// 🆕 YENİ: Kangal Oluşturucu (GÜNCELLENDİ)
// ========================================================================
func NewKangal(cfg *config.KangalConfig, primaryConfig *config.Config, agent kernel.Agent, eventChan chan<- string) *Kangal {
	// 🚨 DÜZELTME #1: Nil kontrolleri
	if cfg == nil {
		logger.Error("❌ [Kangal] Config nil! Kangal oluşturulamadı.")
		return nil
	}
	
	if agent == nil {
		logger.Error("❌ [Kangal] Agent nil! Kangal oluşturulamadı.")
		return nil
	}
	
	// 🚨 KRİTİK: Config enabled kontrolü
	if !cfg.Enabled {
		logger.Info("🐕 [Kangal] Config'de disabled, pasif modda")
		return &Kangal{
			Config:        cfg,
			PrimaryConfig: primaryConfig,
			Agent:         agent,
			EventChan:     eventChan,
			isRunning:     false,
		}
	}
	
	// 🚨 KRİTİK: watchdog_model boşsa logla
	if cfg.WatchdogModel == "" {
		logger.Warn("⚠️ [Kangal] watchdog_model boş, Kangal sınırlı çalışacak")
	}
	
	logger.Info("🐕 [Kangal] Bekçi sistemi yapılandırılıyor...")
	
	return &Kangal{
		Config:        cfg,
		PrimaryConfig: primaryConfig,
		Agent:         agent,
		EventChan:     eventChan,
		isRunning:     false,
	}
}

// Start: Kangal'ı başlatır
func (k *Kangal) Start() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	
	if k.isRunning {
		logger.Warn("⚠️ [Kangal] Zaten aktif, başlatma atlandı")
		return fmt.Errorf("kangal zaten aktif")
	}
	
	// 🚨 KRİTİK: Config enabled kontrolü
	if !k.Config.Enabled {
		logger.Info("🐕 [Kangal] Config'de disabled, başlatılmadı")
		return fmt.Errorf("kangal config'de disabled")
	}
	
	k.ctx, k.cancel = context.WithCancel(context.Background())
	
	logger.Info("🐕 [Kangal] Bekçi sistemi başlatılıyor...")
	
	if err := k.initSubModules(); err != nil {
		logger.Error("❌ [Kangal] Alt modül başlatma hatası: %v", err)
		return err
	}
	
	k.isRunning = true
	
	logger.Success("✅ [Kangal] Bekçi sistemi aktif! (Sensitivity: %s, Watchdog: %s)", 
		k.Config.SensitivityLevel, k.Config.WatchdogModel)
	return nil
}
// Stop: Kangal'ı durdurur
func (k *Kangal) Stop() {
	k.mu.Lock()
	defer k.mu.Unlock()
	
	if !k.isRunning {
		logger.Debug("ℹ️ [Kangal] Zaten durmuş")
		return
	}
	
	logger.Info("🛑 [Kangal] Bekçi sistemi durduruluyor...")
	
	// Context iptal et
	if k.cancel != nil {
		k.cancel()
	}
	
	// Alt modülleri durdur
	k.stopSubModules()
	
	k.isRunning = false
	logger.Success("✅ [Kangal] Bekçi sistemi güvenli şekilde kapatıldı")
}

// initSubModules: Alt modülleri başlatır (watchdog güncellendi)
func (k *Kangal) initSubModules() error {
	var err error
	
	k.windowTracker = NewWindowTracker(k.ctx, k.Config)
	if err := k.windowTracker.Start(); err != nil {
		logger.Warn("⚠️ [Kangal] WindowTracker başlatılamadı: %v", err)
	}
	
	k.errorDetector = NewErrorDetector(k.ctx, k.Config, k.EventChan)
	if err := k.errorDetector.Start(); err != nil {
		logger.Warn("⚠️ [Kangal] ErrorDetector başlatılamadı: %v", err)
	}
	
	k.notification = NewNotificationEngine(k.Config, k.EventChan)
	if err := k.notification.Start(); err != nil {
		logger.Warn("⚠️ [Kangal] NotificationEngine başlatılamadı: %v", err)
	}
	
	// 🚨 DÜZELTME: Watchdog artık primaryConfig alıyor
	k.watchdog = NewWatchdog(k.ctx, k.Config, k.PrimaryConfig, k.Agent, k.notification)
	if err := k.watchdog.Start(); err != nil {
		logger.Warn("⚠️ [Kangal] Watchdog başlatılamadı: %v", err)
	}
	
	k.contextTracker = NewContextTracker(k.ctx, k.Config)
	if err := k.contextTracker.Start(); err != nil {
		logger.Warn("⚠️ [Kangal] ContextTracker başlatılamadı: %v", err)
	}
	
	return err
}


// stopSubModules: Tüm alt modülleri durdurur
func (k *Kangal) stopSubModules() {
	if k.windowTracker != nil {
		k.windowTracker.Stop()
	}
	if k.errorDetector != nil {
		k.errorDetector.Stop()
	}
	if k.notification != nil {
		k.notification.Stop()
	}
	if k.watchdog != nil {
		k.watchdog.Stop()
	}
	if k.contextTracker != nil {
		k.contextTracker.Stop()
	}
}

// ========================================================================
// 🆕 PUBLIC API (Dışarıdan erişim)
// ========================================================================
// IsRunning: Kangal'ın çalışıp çalışmadığını döndür
func (k *Kangal) IsRunning() bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.isRunning
}

// GetStatus: Kangal durum raporunu döndür (debug için)
func (k *Kangal) GetStatus() map[string]interface{} {
	k.mu.RLock()
	defer k.mu.RUnlock()
	
	status := map[string]interface{}{
		"is_running": k.isRunning,
		"enabled":    k.Config.Enabled,
		"sensitivity": k.Config.SensitivityLevel,
		"watchdog_model": k.Config.WatchdogModel,
		//"primary_model": k.Config.PrimaryModel,
	}
	
	// Alt modül durumlarını ekle
	if k.windowTracker != nil {
		status["window_tracker"] = k.windowTracker.GetStatus()
	}
	if k.errorDetector != nil {
		status["error_detector"] = k.errorDetector.GetStatus()
	}
	if k.notification != nil {
		status["notification"] = k.notification.GetStatus()
	}
	if k.watchdog != nil {
		status["watchdog"] = k.watchdog.GetStatus()
	}
	if k.contextTracker != nil {
		status["context_tracker"] = k.contextTracker.GetStatus()
	}
	
	return status
}

// SetEnabled: Kangal'ı runtime'da aç/kapa
func (k *Kangal) SetEnabled(enabled bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	
	if enabled == k.Config.Enabled {
		return // Zaten istenen durumda
	}
	
	k.Config.Enabled = enabled
	
	if enabled && !k.isRunning {
		// Aç
		logger.Info("🐕 [Kangal] Runtime'da aktif ediliyor...")
		k.mu.Unlock() // Lock'u bırak, Start kendi lock'unu alacak
		_ = k.Start()
		k.mu.Lock()
	} else if !enabled && k.isRunning {
		// Kapa
		logger.Info("🐕 [Kangal] Runtime'da kapatılıyor...")
		k.mu.Unlock() // Lock'u bırak, Stop kendi lock'unu alacak
		k.stopSubModules()
		k.mu.Lock()
	}
	
	logger.Info("🐕 [Kangal] Enabled durumu değiştirildi: %v", enabled)
}

// SetSensitivity: Hassasiyet seviyesini değiştir (low, balanced, high)
func (k *Kangal) SetSensitivity(level string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	
	validLevels := map[string]bool{
		"low": true,
		"balanced": true,
		"high": true,
	}
	
	if !validLevels[level] {
		logger.Warn("⚠️ [Kangal] Geçersiz sensitivity level: %s (low/balanced/high)", level)
		return
	}
	
	k.Config.SensitivityLevel = level
	logger.Info("🐕 [Kangal] Sensitivity level değiştirildi: %s", level)
	
	// Alt modüllere bildir (gerekirse)
	if k.windowTracker != nil {
		k.windowTracker.SetSensitivity(level)
	}
	if k.errorDetector != nil {
		k.errorDetector.SetSensitivity(level)
	}
}

// TriggerManualScan: Manuel tarama tetikle (debug/test için)
func (k *Kangal) TriggerManualScan() {
	k.mu.RLock()
	running := k.isRunning
	k.mu.RUnlock()
	
	if !running {
		logger.Warn("⚠️ [Kangal] Manuel tarama atlandı (Kangal çalışmıyor)")
		return
	}
	
	logger.Info("🐕 [Kangal] Manuel tarama tetiklendi...")
	
	// Tüm modüllere scan sinyali gönder
	if k.windowTracker != nil {
		k.windowTracker.TriggerScan()
	}
	if k.errorDetector != nil {
		k.errorDetector.TriggerScan()
	}
}

// ========================================================================
// 🆕 EVENT HANDLING (Sistem olaylarını işleme)
// ========================================================================
// HandleEvent: Dışarıdan gelen olayları işler (Event Bus'tan)
func (k *Kangal) HandleEvent(eventType string, data map[string]interface{}) {
	k.mu.RLock()
	running := k.isRunning
	k.mu.RUnlock()
	
	if !running {
		return
	}
	
	logger.Debug("🐕 [Kangal] Event alındı: %s", eventType)
	
	switch eventType {
	case "terminal_error":
		// Terminal'den hata geldi
		if k.errorDetector != nil {
			k.errorDetector.ProcessTerminalError(data)
		}
	case "window_change":
		// Aktif pencere değişti
		if k.contextTracker != nil {
			windowTitle, _ := data["window"].(string)
			processName, _ := data["process"].(string)
			k.contextTracker.UpdateActiveWindow(windowTitle, processName)
		}
	case "process_crash":
		// Process çöktü
		if k.errorDetector != nil {
			k.errorDetector.ProcessCrash(data)
		}
	case "file_change":
		// Dosya değişti (kod yazılırken)
		if k.contextTracker != nil {
			filePath, _ := data["path"].(string)
			k.contextTracker.UpdateFileActivity(filePath)
		}
	default:
		logger.Debug("🐕 [Kangal] Bilinmeyen event: %s", eventType)
	}
}

// ========================================================================
// 🆕 HELPER FONKSİYONLAR
// ========================================================================
// sendAlert: Bildirim gönder (notification engine üzerinden)
func (k *Kangal) sendAlert(priority string, title string, message string) {
	if k.notification == nil {
		logger.Warn("⚠️ [Kangal] Notification engine nil, alert gönderilemedi")
		return
	}
	
	var notifPriority Priority
	switch priority {
	case "critical":
		notifPriority = PriorityCritical
	case "warning":
		notifPriority = PriorityWarning
	default:
		notifPriority = PriorityInfo
	}
	k.notification.SendAlert(notifPriority, title, message)
}

// escalateToPrimary: Kritik olayı primary model'e escalat et (watchdog üzerinden)
func (k *Kangal) escalateToPrimary(summary string, context map[string]interface{}) {
	if k.watchdog == nil {
		logger.Warn("⚠️ [Kangal] Watchdog nil, escalasyon yapılamadı")
		return
	}
	
	k.watchdog.Escalate(summary, context)
}

// ========================================================================
// 🆕 GETTERS (Alt modüllere erişim için)
// ========================================================================
// GetWindowTracker: Window tracker'ı döndür
func (k *Kangal) GetWindowTracker() *WindowTracker {
	return k.windowTracker
}

// GetErrorDetector: Error detector'ı döndür
func (k *Kangal) GetErrorDetector() *ErrorDetector {
	return k.errorDetector
}

// GetNotification: Notification engine'i döndür
func (k *Kangal) GetNotification() *NotificationEngine {
	return k.notification
}

// GetWatchdog: Watchdog'u döndür
func (k *Kangal) GetWatchdog() *Watchdog {
	return k.watchdog
}

// GetContextTracker: Context tracker'ı döndür
func (k *Kangal) GetContextTracker() *ContextTracker {
	return k.contextTracker
}
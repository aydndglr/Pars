package kangal

import (
	"context"
	"fmt"
	"sync"
	"github.com/aydndglr/pars-agent-v3/internal/core/config"
	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

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

func NewKangal(cfg *config.KangalConfig, primaryConfig *config.Config, agent kernel.Agent, eventChan chan<- string) *Kangal {
	if cfg == nil {
		logger.Error("❌ [Kangal] Config nil! Kangal oluşturulamadı.")
		return nil
	}
	
	if agent == nil {
		logger.Error("❌ [Kangal] Agent nil! Kangal oluşturulamadı.")
		return nil
	}
	
	if !cfg.Enabled {
		logger.Info("🐕 [Kangal] Config'de disabled, pasif modda")
		return &Kangal{
			Config:         cfg,
			PrimaryConfig:  primaryConfig,
			Agent:          agent,
			EventChan:      eventChan,
			isRunning:      false,
		}
	}
	
	if cfg.WatchdogModel == "" {
		logger.Warn("⚠️ [Kangal] watchdog_model boş, Kangal sınırlı çalışacak")
	}
	
	logger.Info("🐕 [Kangal] Bekçi sistemi yapılandırılıyor...")
	
	return &Kangal{
		Config:         cfg,
		PrimaryConfig:  primaryConfig,
		Agent:          agent,
		EventChan:      eventChan,
		isRunning:      false,
	}
}

func (k *Kangal) Start() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	
	if k.isRunning {
		logger.Warn("⚠️ [Kangal] Zaten aktif, başlatma atlandı")
		return fmt.Errorf("kangal zaten aktif")
	}
	
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

func (k *Kangal) Stop() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.isRunning {
		logger.Debug("ℹ️ [Kangal] Zaten durmuş")
		return
	}
	logger.Info("🛑 [Kangal] Bekçi sistemi durduruluyor...")
	if k.cancel != nil {
		k.cancel()
	}
	k.stopSubModules()
	
	k.isRunning = false
	logger.Success("✅ [Kangal] Bekçi sistemi güvenli şekilde kapatıldı")
}

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

func (k *Kangal) IsRunning() bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.isRunning
}

func (k *Kangal) GetStatus() map[string]interface{} {
	k.mu.RLock()
	defer k.mu.RUnlock()
	
	status := map[string]interface{}{
		"is_running": k.isRunning,
		"enabled":    k.Config.Enabled,
		"sensitivity": k.Config.SensitivityLevel,
		"watchdog_model": k.Config.WatchdogModel,
	}
	
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

func (k *Kangal) SetEnabled(enabled bool) {
	k.mu.Lock()
	if enabled == k.Config.Enabled {
		k.mu.Unlock()
		return 
	}
	
	k.Config.Enabled = enabled
	wasRunning := k.isRunning
	k.mu.Unlock()
	
	if enabled && !wasRunning {
		logger.Info("🐕 [Kangal] Runtime'da aktif ediliyor...")
		_ = k.Start()
	} else if !enabled && wasRunning {
		logger.Info("🐕 [Kangal] Runtime'da kapatılıyor...")
		k.Stop()
	}
	
	logger.Info("🐕 [Kangal] Enabled durumu değiştirildi: %v", enabled)
}

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
	
	if k.windowTracker != nil {
		k.windowTracker.SetSensitivity(level)
	}
	if k.errorDetector != nil {
		k.errorDetector.SetSensitivity(level)
	}
}

func (k *Kangal) TriggerManualScan() {
	k.mu.RLock()
	running := k.isRunning
	k.mu.RUnlock()
	
	if !running {
		logger.Warn("⚠️ [Kangal] Manuel tarama atlandı (Kangal çalışmıyor)")
		return
	}
	
	logger.Info("🐕 [Kangal] Manuel tarama tetiklendi...")
	if k.windowTracker != nil {
		k.windowTracker.TriggerScan()
	}
	if k.errorDetector != nil {
		k.errorDetector.TriggerScan()
	}
}

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
		if k.errorDetector != nil {
			k.errorDetector.ProcessTerminalError(data)
		}
	case "window_change":
		if k.contextTracker != nil {
			windowTitle, _ := data["window"].(string)
			processName, _ := data["process"].(string)
			k.contextTracker.UpdateActiveWindow(windowTitle, processName)
		}
	case "process_crash":
		if k.errorDetector != nil {
			k.errorDetector.ProcessCrash(data)
		}
	case "file_change":
		if k.contextTracker != nil {
			filePath, _ := data["path"].(string)
			k.contextTracker.UpdateFileActivity(filePath)
		}
	default:
		logger.Debug("🐕 [Kangal] Bilinmeyen event: %s", eventType)
	}
}

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

func (k *Kangal) escalateToPrimary(summary string, context map[string]interface{}) {
	if k.watchdog == nil {
		logger.Warn("⚠️ [Kangal] Watchdog nil, escalasyon yapılamadı")
		return
	}
	
	k.watchdog.Escalate(summary, context)
}


func (k *Kangal) GetWindowTracker() *WindowTracker {
	return k.windowTracker
}

func (k *Kangal) GetErrorDetector() *ErrorDetector {
	return k.errorDetector
}

func (k *Kangal) GetNotification() *NotificationEngine {
	return k.notification
}

func (k *Kangal) GetWatchdog() *Watchdog {
	return k.watchdog
}

func (k *Kangal) GetContextTracker() *ContextTracker {
	return k.contextTracker
}
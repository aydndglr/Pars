package kangal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/brain/providers"
	"github.com/aydndglr/pars-agent-v3/internal/core/config"
	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)


type EscalationEvent struct {
	ID          string
	Summary     string
	FullContext string
	Priority    string
	Context     map[string]interface{}
	Timestamp   time.Time
}

type WatchdogStats struct {
	TotalEvents       int
	CriticalCount     int
	WarningCount      int
	InfoCount         int
	EscalatedCount    int
	AvgResponseTimeMs int64
	LastActivity      time.Time
}

type WatchdogDecision struct {
	Priority       string `json:"priority"`
	Category       string `json:"category"`
	Summary        string `json:"summary"`
	ShouldEscalate bool   `json:"should_escalate"`
	Suggestion     string `json:"suggestion"`
}

type Watchdog struct {
	Config          *config.KangalConfig
	PrimaryConfig   *config.Config
	WatchdogBrain   kernel.Brain
	PrimaryBrain    kernel.Brain
	Agent           kernel.Agent
	Notification    *NotificationEngine
	ctx             context.Context
	cancel          context.CancelFunc
	isRunning       bool
	mu              sync.RWMutex

	escalationQueue chan EscalationEvent
	stats           WatchdogStats
	statsMu         sync.RWMutex
	lastEscalation  time.Time
	escalationMu    sync.RWMutex
}


func NewWatchdog(ctx context.Context, cfg *config.KangalConfig, primaryConfig *config.Config, agent kernel.Agent, notification *NotificationEngine) *Watchdog {
	if cfg == nil {
		logger.Error("❌ [Watchdog] Config nil! Watchdog oluşturulamadı.")
		return nil
	}

	if !cfg.IsWatchdogEnabled() {
		logger.Info("🐕 [Watchdog] Watchdog modeli tanımlı değil, pasif modda çalışacak")
		return &Watchdog{
			Config:        cfg,
			PrimaryConfig: primaryConfig,
			isRunning:     false,
		}
	}

	if agent == nil {
		logger.Error("❌ [Watchdog] Agent nil! Watchdog oluşturulamadı.")
		return nil
	}

	var watchdogBrain kernel.Brain
	if cfg.WatchdogModel != "" {
		watchdogBrain = providers.NewOllama(
			primaryConfig.Brain.Primary.BaseURL,
			cfg.WatchdogModel,
			0.3,
			2048,
			primaryConfig.Brain.APIKeys.Ollama,
		)
	}

	var primaryBrain kernel.Brain
	if primaryConfig.Brain.Primary.ModelName != "" {
		primaryBrain = providers.NewOllama(
			primaryConfig.Brain.Primary.BaseURL,
			primaryConfig.Brain.Primary.ModelName,
			0.6,
			primaryConfig.Brain.Primary.NumCtx,
			primaryConfig.Brain.APIKeys.Ollama,
		)
	}

	return &Watchdog{
		Config:          cfg,
		PrimaryConfig:   primaryConfig,
		WatchdogBrain:   watchdogBrain,
		PrimaryBrain:    primaryBrain,
		Agent:           agent,
		Notification:    notification,
		ctx:             ctx,
		isRunning:       false,
		escalationQueue: make(chan EscalationEvent, 50),
		stats: WatchdogStats{
			LastActivity: time.Now(),
		},
	}
}

func (w *Watchdog) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.isRunning {
		return nil
	}

	if !w.Config.IsWatchdogEnabled() {
		return nil
	}

	w.ctx, w.cancel = context.WithCancel(context.Background())
	w.isRunning = true

	go w.processEscalations()

	logger.Success("✅ [Watchdog] Hafif analiz motoru aktif! (Model: %s)", w.Config.WatchdogModel)
	return nil
}

func (w *Watchdog) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.isRunning {
		return
	}

	if w.cancel != nil {
		w.cancel()
	}

	w.isRunning = false
	logger.Success("✅ [Watchdog] Analiz motoru durduruldu")
}

func (w *Watchdog) ClassifyEvent(eventType string, eventData map[string]interface{}) *WatchdogDecision {
	startTime := time.Now()

	w.statsMu.Lock()
	w.stats.TotalEvents++
	w.stats.LastActivity = time.Now()
	w.statsMu.Unlock()

	if w.WatchdogBrain == nil {
		decision := w.ruleBasedClassification(eventType, eventData)
		w.updateStats(decision, startTime)
		return decision
	}

	eventStr := w.formatEventForLLM(eventType, eventData)
	analysisCtx, cancel := context.WithTimeout(w.ctx, 3*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(`[SİSTEM OLAYI ANALİZİ]
Olay Tipi: %s
Olay Detayı: %s
Döneceğin JSON formatı: {"priority": "critical/warning/info", "category": "dll/crash/...", "summary": "...", "should_escalate": true/false, "suggestion": "..."}`, eventType, eventStr)

	history := []kernel.Message{
		{Role: "system", Content: "Sadece JSON döndüren bir olay sınıflandırma motorusun."},
		{Role: "user", Content: prompt},
	}

	resp, err := w.WatchdogBrain.Chat(analysisCtx, history, nil)
	if err != nil {
		decision := w.ruleBasedClassification(eventType, eventData)
		w.updateStats(decision, startTime)
		return decision
	}

	decision := w.parseDecision(resp.Content)
	if decision == nil {
		decision = w.ruleBasedClassification(eventType, eventData)
	}

	w.updateStats(decision, startTime)
	return decision
}

func (w *Watchdog) ruleBasedClassification(eventType string, eventData map[string]interface{}) *WatchdogDecision {
	eventStr := fmt.Sprintf("%v", eventData)
	eventLower := strings.ToLower(eventStr)

	decision := &WatchdogDecision{
		Priority:       "info",
		Category:       "other",
		Summary:        eventType,
		ShouldEscalate: false,
		Suggestion:     "Olay kaydedildi.",
	}

	criticalPatterns := []string{"dll", "crash", "fatal", "panic", "access denied"}
	for _, pattern := range criticalPatterns {
		if strings.Contains(eventLower, pattern) {
			decision.Priority = "critical"
			decision.ShouldEscalate = true
			decision.Suggestion = "Kritik hata tespit edildi. Analiz ediliyor..."
			break
		}
	}
	return decision
}

func (w *Watchdog) parseDecision(content string) *WatchdogDecision {
	jsonStart := strings.Index(content, "{")
	jsonEnd := strings.LastIndex(content, "}")
	if jsonStart == -1 || jsonEnd == -1 {
		return nil
	}

	var decision WatchdogDecision
	if err := json.Unmarshal([]byte(content[jsonStart:jsonEnd+1]), &decision); err != nil {
		return nil
	}
	return &decision
}

func (w *Watchdog) updateStats(decision *WatchdogDecision, startTime time.Time) {
	w.statsMu.Lock()
	defer w.statsMu.Unlock()

	switch decision.Priority {
	case "critical":
		w.stats.CriticalCount++
	case "warning":
		w.stats.WarningCount++
	default:
		w.stats.InfoCount++
	}

	if decision.ShouldEscalate {
		w.stats.EscalatedCount++
	}

	responseTime := time.Since(startTime).Milliseconds()
	if w.stats.AvgResponseTimeMs == 0 {
		w.stats.AvgResponseTimeMs = responseTime
	} else {
		w.stats.AvgResponseTimeMs = (w.stats.AvgResponseTimeMs*9 + responseTime) / 10
	}
}

func (w *Watchdog) Escalate(summary string, context map[string]interface{}) {
	w.mu.RLock()
	running := w.isRunning
	w.mu.RUnlock()

	if !running || !w.canEscalate() {
		return
	}

	event := EscalationEvent{
		ID:        fmt.Sprintf("esc_%d", time.Now().UnixNano()),
		Summary:   summary,
		Context:   context,
		Timestamp: time.Now(),
	}

	select {
	case w.escalationQueue <- event:
	default:
		logger.Warn("⚠️ [Watchdog] Escalation queue dolu")
	}
}

func (w *Watchdog) processEscalations() {
	for {
		select {
		case <-w.ctx.Done():
			return
		case event := <-w.escalationQueue:
			w.handleEscalation(event)
		}
	}
}

func (w *Watchdog) handleEscalation(event EscalationEvent) {
	if w.PrimaryBrain == nil {
		return
	}

	w.escalationMu.Lock()
	w.lastEscalation = time.Now()
	w.escalationMu.Unlock()

	analysisCtx, cancel := context.WithTimeout(w.ctx, 30*time.Second)
	defer cancel()

	prompt := fmt.Sprintf("[KRİTİK OLAY] Özet: %s, Bağlam: %v", event.Summary, event.Context)
	history := []kernel.Message{
		{Role: "system", Content: "Kritik hataları çözen bir uzmansın."},
		{Role: "user", Content: prompt},
	}

	resp, err := w.PrimaryBrain.Chat(analysisCtx, history, nil)
	if err == nil && w.Notification != nil {
		w.Notification.SendCritical("🚨 Kritik Sistem Olayı", resp.Content)
	}
}

func (w *Watchdog) canEscalate() bool {
	w.escalationMu.RLock()
	defer w.escalationMu.RUnlock()
	return time.Since(w.lastEscalation) > 1*time.Minute
}

func (w *Watchdog) GetStatus() map[string]interface{} {
	w.mu.RLock()
	w.statsMu.RLock()
	defer w.mu.RUnlock()
	defer w.statsMu.RUnlock()

	return map[string]interface{}{
		"is_running":      w.isRunning,
		"total_events":    w.stats.TotalEvents,
		"critical_count":  w.stats.CriticalCount,
		"escalated_count": w.stats.EscalatedCount,
		"avg_response_ms": w.stats.AvgResponseTimeMs,
	}
}

func (w *Watchdog) formatEventForLLM(eventType string, eventData map[string]interface{}) string {
	return fmt.Sprintf("Tip: %s, Veri: %v", eventType, eventData)
}
// internal/skills/kangal/watchdog.go
// 🚀 KANGAL WATCHDOG - Qwen 1.5B Hafif Analiz Motoru
// ⚠️ DİKKAT: watchdog_model boşsa Kangal pasif çalışır
// 📅 Oluşturulma: 2026-03-07 (Pars V5 - Kangal Edition)

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

// ========================================================================
// 📦 TYPE TANIMLARI (EN ÜSTTE OLMALI!)
// ========================================================================

// EscalationEvent: Primary model'e gönderilecek kritik olay
type EscalationEvent struct {
	ID          string
	Summary     string
	FullContext string
	Priority    string
	Context     map[string]interface{}
	Timestamp   time.Time
}

// WatchdogStats: Watchdog istatistikleri
type WatchdogStats struct {
	TotalEvents       int
	CriticalCount     int
	WarningCount      int
	InfoCount         int
	EscalatedCount    int
	AvgResponseTimeMs int64
	LastActivity      time.Time
}

// WatchdogDecision: Watchdog'un olay sınıflandırma kararı
type WatchdogDecision struct {
	Priority       string `json:"priority"`
	Category       string `json:"category"`
	Summary        string `json:"summary"`
	ShouldEscalate bool   `json:"should_escalate"`
	Suggestion     string `json:"suggestion"`
}

// ========================================================================
// 🧠 WATCHDOG YAPISI
// ========================================================================
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



// ========================================================================
// 🆕 YENİ: Watchdog Oluşturucu
// ========================================================================
func NewWatchdog(ctx context.Context, cfg *config.KangalConfig, primaryConfig *config.Config, agent kernel.Agent, notification *NotificationEngine) *Watchdog {
	// 🚨 DÜZELTME #1: Nil kontrolleri
	if cfg == nil {
		logger.Error("❌ [Watchdog] Config nil! Watchdog oluşturulamadı.")
		return nil
	}

	// 🚨 KRİTİK: watchdog_model boşsa watchdog pasif
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

	logger.Info("🐕 [Watchdog] Hafif analiz motoru yapılandırılıyor...")

	// 🚨 DÜZELTME #2: Watchdog brain oluştur (qwen3:1.5b veya benzeri)
	var watchdogBrain kernel.Brain
	if cfg.WatchdogModel != "" {
		watchdogBrain = providers.NewOllama(
			primaryConfig.Brain.Primary.BaseURL,
			cfg.WatchdogModel,
			0.3,
			2048,
			primaryConfig.Brain.APIKeys.Ollama, 
		)
		logger.Debug("🧠 [Watchdog] Qwen analiz motoru hazır: %s", cfg.WatchdogModel)
	}

	// 🚨 DÜZELTME #3: Primary brain artık brain.primary.model_name kullanıyor
	var primaryBrain kernel.Brain
	if primaryConfig.Brain.Primary.ModelName != "" {
		primaryBrain = providers.NewOllama(
			primaryConfig.Brain.Primary.BaseURL,
			primaryConfig.Brain.Primary.ModelName,
			0.6,
			primaryConfig.Brain.Primary.NumCtx,
			primaryConfig.Brain.APIKeys.Ollama, // 🚀 YENİ: Vast.ai API Key eklendi
		)
		logger.Debug("🧠 [Watchdog] Primary brain hazır: %s", primaryConfig.Brain.Primary.ModelName)
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

// ========================================================================
// 🚀 BAŞLATMA / DURDURMA
// ========================================================================
func (w *Watchdog) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.isRunning {
		logger.Warn("⚠️ [Watchdog] Zaten aktif, başlatma atlandı")
		return nil
	}

	// 🚨 KRİTİK: Config enabled + watchdog_model kontrolü
	if !w.Config.IsWatchdogEnabled() {
		logger.Debug("ℹ️ [Watchdog] Config'de disabled veya watchdog_model boş, başlatılmadı")
		return nil
	}

	if w.WatchdogBrain == nil {
		logger.Warn("⚠️ [Watchdog] WatchdogBrain nil, sadece rule-based sınıflandırma yapılacak")
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
		logger.Debug("ℹ️ [Watchdog] Zaten durmuş")
		return
	}

	logger.Info("🛑 [Watchdog] Analiz motoru durduruluyor...")

	if w.cancel != nil {
		w.cancel()
	}

	w.isRunning = false
	logger.Success("✅ [Watchdog] Analiz motoru durduruldu")
}

// ========================================================================
// 🧠 ANALİZ MOTORU
// ========================================================================
func (w *Watchdog) ClassifyEvent(eventType string, eventData map[string]interface{}) *WatchdogDecision {
	startTime := time.Now()

	// 🚨 DÜZELTME #6: İstatistik güncelle
	w.statsMu.Lock()
	w.stats.TotalEvents++
	w.stats.LastActivity = time.Now()
	w.statsMu.Unlock()

	// 🚨 DÜZELTME #7: Rule-based fallback (AI yoksa)
	if w.WatchdogBrain == nil {
		decision := w.ruleBasedClassification(eventType, eventData)
		w.updateStats(decision, startTime)
		return decision
	}

	// 🚨 DÜZELTME #8: Event'i string'e çevir (LLM için)
	eventStr := w.formatEventForLLM(eventType, eventData)

	// 🚨 DÜZELTME #9: Timeout'lu context (hızlı yanıt için)
	analysisCtx, cancel := context.WithTimeout(w.ctx, 3*time.Second)
	defer cancel()

	// 🚨 DÜZELTME #10: Watchdog prompt'u (kısa ve odaklı)
	prompt := fmt.Sprintf(`
[SİSTEM OLAYI ANALİZİ - HIZLI SINIFLANDIRMA]

Olay Tipi: %s
Olay Detayı: %s

GÖREV:
Bu olayı aşağıdaki kriterlere göre sınıflandır:

1. ÖNCELİK (priority):
   - "critical": Kullanıcı müdahalesi gerekir (DLL hatası, crash, veri kaybı riski)
   - "warning": Öneri sun (syntax error, yüksek RAM, yavaş performans)
   - "info": Sadece logla (dosya kaydetme, normal işlem)

2. KATEGORİ (category):
   - "dll", "crash", "timeout", "code_error", "permission", "network", "disk", "memory", "other"

3. ESCALATION (should_escalate):
   - true: Primary model'e gönder (kompleks problem çözme gerekir)
   - false: Sadece bildirim gönder (basit öneri yeterli)

4. ÖNERİ (suggestion):
   - Kullanıcıya gösterilecek kısa çözüm önerisi (max 50 kelime)

SADECE JSON DÖNDÜR (başka hiçbir açıklama yok):
{
  "priority": "...",
  "category": "...",
  "summary": "...",
  "should_escalate": true/false,
  "suggestion": "..."
}
`, eventType, eventStr)

	// 🚨 DÜZELTME #11: LLM chat çağrısı
	history := []kernel.Message{
		{Role: "system", Content: "Sen bir olay sınıflandırma motorusun. Sadece JSON döndürürsün. Açıklama yapmazsın."},
		{Role: "user", Content: prompt},
	}

	resp, err := w.WatchdogBrain.Chat(analysisCtx, history, nil)
	if err != nil {
		logger.Warn("⚠️ [Watchdog] LLM analiz hatası: %v, rule-based fallback kullanılıyor", err)
		decision := w.ruleBasedClassification(eventType, eventData)
		w.updateStats(decision, startTime)
		return decision
	}

	// 🚨 DÜZELTME #12: JSON parse
	decision := w.parseDecision(resp.Content)
	if decision == nil {
		logger.Warn("⚠️ [Watchdog] JSON parse hatası, rule-based fallback kullanılıyor")
		decision = w.ruleBasedClassification(eventType, eventData)
	}

	w.updateStats(decision, startTime)
	return decision
}

// ruleBasedClassification: AI yoksa rule-based sınıflandırma yap
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

	// 🚨 DÜZELTME #13: Critical pattern'ler
	criticalPatterns := []string{
		"dll", "crash", "fatal", "panic", "0xc0000",
		"access denied", "permission denied", "out of memory",
	}

	for _, pattern := range criticalPatterns {
		if strings.Contains(eventLower, pattern) {
			decision.Priority = "critical"
			decision.ShouldEscalate = true
			decision.Suggestion = "Kritik hata tespit edildi. Çözüm önerisi hazırlanıyor..."
			break
		}
	}

	// 🚨 DÜZELTME #14: Warning pattern'ler
	warningPatterns := []string{
		"warning", "timeout", "slow", "high cpu", "high ram",
		"syntax error", "import error", "module not found",
	}

	for _, pattern := range warningPatterns {
		if strings.Contains(eventLower, pattern) {
			if decision.Priority != "critical" {
				decision.Priority = "warning"
				decision.Suggestion = "Uyarı tespit edildi. Öneri: İşlemi kontrol edin."
			}
			break
		}
	}

	// 🚨 DÜZELTME #15: Category detection
	if strings.Contains(eventLower, "dll") {
		decision.Category = "dll"
	} else if strings.Contains(eventLower, "crash") {
		decision.Category = "crash"
	} else if strings.Contains(eventLower, "timeout") {
		decision.Category = "timeout"
	} else if strings.Contains(eventLower, "error") {
		decision.Category = "code_error"
	} else if strings.Contains(eventLower, "permission") {
		decision.Category = "permission"
	} else if strings.Contains(eventLower, "network") {
		decision.Category = "network"
	} else if strings.Contains(eventLower, "disk") {
		decision.Category = "disk"
	} else if strings.Contains(eventLower, "memory") {
		decision.Category = "memory"
	}

	return decision
}

// formatEventForLLM: Event'i LLM için formatla
func (w *Watchdog) formatEventForLLM(eventType string, eventData map[string]interface{}) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Tip: %s\n", eventType))

	for key, value := range eventData {
		sb.WriteString(fmt.Sprintf("%s: %v\n", key, value))
	}

	return sb.String()
}

// parseDecision: LLM yanıtından WatchdogDecision parse et
func (w *Watchdog) parseDecision(content string) *WatchdogDecision {
	// 🚨 DÜZELTME #16: JSON bloğunu çıkar
	jsonStart := strings.Index(content, "{")
	jsonEnd := strings.LastIndex(content, "}")

	if jsonStart == -1 || jsonEnd == -1 {
		return nil
	}

	jsonStr := content[jsonStart : jsonEnd+1]

	var decision WatchdogDecision
	if err := json.Unmarshal([]byte(jsonStr), &decision); err != nil {
		logger.Debug("⚠️ [Watchdog] JSON parse hatası: %v", err)
		return nil
	}

	// 🚨 DÜZELTME #17: Validation
	if decision.Priority == "" {
		decision.Priority = "info"
	}
	if decision.Category == "" {
		decision.Category = "other"
	}
	if decision.Summary == "" {
		decision.Summary = "Olay analiz edildi"
	}

	return &decision
}

// updateStats: İstatistikleri güncelle
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

	// Response time hesapla
	responseTime := time.Since(startTime).Milliseconds()
	w.stats.AvgResponseTimeMs = (w.stats.AvgResponseTimeMs + responseTime) / 2
}

// ========================================================================
// 🚨 ESCALATION ENGINE
// ========================================================================
func (w *Watchdog) Escalate(summary string, context map[string]interface{}) {
	w.mu.RLock()
	running := w.isRunning
	w.mu.RUnlock()

	if !running {
		logger.Debug("ℹ️ [Watchdog] Escalation atlandı (watchdog çalışmıyor)")
		return
	}

	// 🚨 DÜZELTME #18: Rate limiting kontrolü
	if !w.canEscalate() {
		logger.Debug("⏱️ [Watchdog] Escalation rate limit nedeniyle atlandı")
		return
	}

	event := EscalationEvent{
		ID:          fmt.Sprintf("esc_%d", time.Now().UnixNano()),
		Summary:     summary,
		FullContext: fmt.Sprintf("%v", context),
		Priority:    "critical",
		Context:     context,
		Timestamp:   time.Now(),
	}

	// 🚨 DÜZELTME #19: Non-blocking send
	select {
	case w.escalationQueue <- event:
		logger.Debug("🚨 [Watchdog] Escalation kuyruğa eklendi: %s", event.ID)
	default:
		logger.Warn("⚠️ [Watchdog] Escalation queue dolu, event atlandı")
	}
}

// processEscalations: Arka planda escalation'ları işle
func (w *Watchdog) processEscalations() {
	logger.Debug("🚨 [Watchdog] Escalation processor başlatıldı")

	for {
		select {
		case <-w.ctx.Done():
			logger.Debug("🛑 [Watchdog] Escalation processor durduruldu")
			return
		case event := <-w.escalationQueue:
			w.handleEscalation(event)
		}
	}
}

// handleEscalation: Tek bir escalation'ı işle
func (w *Watchdog) handleEscalation(event EscalationEvent) {
	// 🚨 DÜZELTME #20: Primary brain kontrolü
	if w.PrimaryBrain == nil {
		logger.Warn("⚠️ [Watchdog] PrimaryBrain nil, escalation atlandı")
		return
	}

	// 🚨 DÜZELTME #21: Rate limit güncelle
	w.escalationMu.Lock()
	w.lastEscalation = time.Now()
	w.escalationMu.Unlock()

	// 🚨 DÜZELTME #22: Timeout'lu context
	analysisCtx, cancel := context.WithTimeout(w.ctx, 30*time.Second)
	defer cancel()

	// 🚨 DÜZELTME #23: Primary prompt (detaylı analiz için)
	prompt := fmt.Sprintf(`
[KRİTİK SİSTEM OLAYI - DETAYLI ANALİZ]

ÖZET: %s
BAĞLAM: %v

GÖREV:
1. Bu kritik olayı analiz et
2. Kök sebebi bul
3. Adım adım çözüm önerisi sun
4. Otomatik düzeltme mümkünse komut öner

KULLANICIYA YÖNELİK ÇIKTI:
- Kısa ve net ol (max 200 kelime)
- Teknik jargon'u açıkla
- Otomatik düzeltme için hazır komut ver

ÇÖZÜM ÖNERİSİ:
`, event.Summary, event.Context)

	history := []kernel.Message{
		{Role: "system", Content: "Sen bir sistem sorun giderme uzmanısın. Kritik hataları analiz edip kullanıcıya net çözüm önerileri sunarsın."},
		{Role: "user", Content: prompt},
	}

	resp, err := w.PrimaryBrain.Chat(analysisCtx, history, nil)
	if err != nil {
		logger.Error("❌ [Watchdog] Primary analiz hatası: %v", err)
		return
	}

	// 🚨 DÜZELTME #24: Kullanıcıya bildirim gönder
	if w.Notification != nil {
		w.Notification.SendCritical("🚨 Kritik Sistem Olayı", resp.Content)
	}

	logger.Success("✅ [Watchdog] Escalation işlendi: %s", event.ID)
}

// canEscalate: Rate limiting kontrolü (escalation için)
func (w *Watchdog) canEscalate() bool {
	w.escalationMu.RLock()
	defer w.escalationMu.RUnlock()

	// Son escalasyon'dan beri en az 1 dakika geçmiş olmalı
	return time.Since(w.lastEscalation) > 1*time.Minute
}

// ========================================================================
// 🎯 PUBLIC API
// ========================================================================
func (w *Watchdog) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.isRunning
}

func (w *Watchdog) GetStatus() map[string]interface{} {
	w.mu.RLock()
	w.statsMu.RLock()
	w.escalationMu.RLock()
	defer w.mu.RUnlock()
	defer w.statsMu.RUnlock()
	defer w.escalationMu.RUnlock()

	queueSize := len(w.escalationQueue)

	return map[string]interface{}{
		"is_running":           w.isRunning,
		"watchdog_model":       w.Config.WatchdogModel,
		"primary_model":        w.PrimaryConfig.Brain.Primary.ModelName,
		"total_events":         w.stats.TotalEvents,
		"critical_count":       w.stats.CriticalCount,
		"warning_count":        w.stats.WarningCount,
		"info_count":           w.stats.InfoCount,
		"escalated_count":      w.stats.EscalatedCount,
		"avg_response_ms":      w.stats.AvgResponseTimeMs,
		"last_activity":        w.stats.LastActivity,
		"escalation_queue":     queueSize,
		"last_escalation":      w.lastEscalation,
		"watchdog_brain_ok":    w.WatchdogBrain != nil,
		"primary_brain_ok":     w.PrimaryBrain != nil,
	}
}

func (w *Watchdog) GetStats() WatchdogStats {
	w.statsMu.RLock()
	defer w.statsMu.RUnlock()

	return w.stats
}

func (w *Watchdog) TriggerAnalysis(eventType string, eventData map[string]interface{}) *WatchdogDecision {
	logger.Debug("🔍 [Watchdog] Manuel analiz tetiklendi: %s", eventType)
	return w.ClassifyEvent(eventType, eventData)
}

func (w *Watchdog) ResetStats() {
	w.statsMu.Lock()
	defer w.statsMu.Unlock()

	w.stats = WatchdogStats{
		LastActivity: time.Now(),
	}
	logger.Debug("🧹 [Watchdog] İstatistikler sıfırlandı")
}

// SetModels: Runtime'da model değiştir (debug için)
func (w *Watchdog) SetModels(watchdogModel, primaryModel string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	if watchdogModel != "" {
		w.Config.WatchdogModel = watchdogModel
		
		// 🚀 YENİ: Tüm parametreler için varsayılan (fallback) değerler
		baseURL := "http://localhost:11434" 
		apiKey := ""                        
		temp := 0.3                         
		numCtx := 2048                      
		
		// 🚀 YENİ: Eğer config varsa, bütün değerleri oradan al
		if w.PrimaryConfig != nil {
			baseURL = w.PrimaryConfig.Brain.Primary.BaseURL
			apiKey = w.PrimaryConfig.Brain.APIKeys.Ollama 
			
			// Eğer config'de 0'dan büyük değerler girilmişse onları kullan
			if w.PrimaryConfig.Brain.Primary.Temperature > 0 {
				temp = w.PrimaryConfig.Brain.Primary.Temperature
			}
			if w.PrimaryConfig.Brain.Primary.NumCtx > 0 {
				numCtx = w.PrimaryConfig.Brain.Primary.NumCtx
			}
		}
		
		w.WatchdogBrain = providers.NewOllama(
			baseURL,  
			watchdogModel,
			temp,     // ← Dinamik: Config'den gelen Sıcaklık
			numCtx,   // ← Dinamik: Config'den gelen Bağlam Penceresi
			apiKey,   // ← Dinamik: Config'den gelen Vast.ai Şifresi
		)
		logger.Info("🔧 [Watchdog] Watchdog modeli güncellendi: %s (URL: %s, Temp: %.2f, Ctx: %d)", watchdogModel, baseURL, temp, numCtx)
	}
	
	if primaryModel != "" {
		logger.Warn("⚠️ [Watchdog] Primary model runtime'da değiştirilemez, brain.primary.model_name kullanılır")
	}
	
	logger.Info("🔧 [Watchdog] Model ayarları güncellendi")
}

// ========================================================================
// 🆕 HELPER FONKSİYONLAR
// ========================================================================
func (w *Watchdog) sendAlert(priority string, title string, message string) {
	if w.Notification == nil {
		logger.Warn("⚠️ [Watchdog] Notification engine nil, alert gönderilemedi")
		return
	}

	switch priority {
	case "critical":
		w.Notification.SendCritical(title, message)
	case "warning":
		w.Notification.SendWarning(title, message)
	default:
		w.Notification.SendInfo(title, message)
	}
}

func (w *Watchdog) GetEscalationQueueSize() int {
	return len(w.escalationQueue)
}

func (w *Watchdog) ClearEscalationQueue() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for len(w.escalationQueue) > 0 {
		<-w.escalationQueue
	}
	logger.Debug("🧹 [Watchdog] Escalation kuyruğu temizlendi")
}
// internal/skills/kangal/notification.go
// 🚀 KANGAL - NOTIFICATION ENGINE (Bildirim Motoru)
// 📅 Oluşturulma: 2026-03-07 (Pars V5 - Kangal Edition)
// ⚠️ DİKKAT: Windows Toast, Terminal Inline ve WhatsApp entegrasyonu
// 🔧 DÜZELTME: PowerShell path fix + os import eklendi

package kangal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/config"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

// ========================================================================
// 🔔 BİLDİRİM TİPLERİ VE ÖNCELİKLERİ
// ========================================================================
// Priority: Bildirim öncelik seviyesi
type Priority string

const (
	PriorityCritical Priority = "critical" // Hemen dikkat gerektirir (DLL hatası, crash)
	PriorityWarning  Priority = "warning"  // Öneri sun (syntax error, yüksek RAM)
	PriorityInfo     Priority = "info"     // Sadece logla (dosya kaydetme)
)

// Notification: Tek bir bildirim yapısı
type Notification struct {
	ID        string
	Priority  Priority
	Title     string
	Message   string
	Timestamp time.Time
	Channel   string // "toast", "terminal", "whatsapp"
	Read      bool
}

// ========================================================================
// 📦 NOTIFICATION ENGINE YAPISI
// ========================================================================
// NotificationEngine: Bildirimleri yöneten ve dağıtan motor
type NotificationEngine struct {
	Config        *config.KangalConfig
	EventChan     chan<- string
	ctx           context.Context
	cancel        context.CancelFunc
	isRunning     bool
	mu            sync.RWMutex
	
	// Bildirim geçmişi (son 100)
	notifications []Notification
	notifyMu      sync.RWMutex
	
	// Rate limiting
	alertTracker  map[string]time.Time // Bildirim ID -> Son gönderim zamanı
	alertMu       sync.RWMutex
	
	// İstatistikler
	stats         NotificationStats
	statsMu       sync.RWMutex
	
	// WhatsApp listener (kritik alert'ler için)
	whatsappListener interface{} // *whatsapp.Listener (type assertion ile)
}

// NotificationStats: Bildirim istatistikleri
type NotificationStats struct {
	TotalSent      int
	ToastCount     int
	TerminalCount  int
	WhatsAppCount  int
	LastHourCount  int
	ByPriority     map[Priority]int
}

// ========================================================================
// 🆕 YENİ: NotificationEngine Oluşturucu
// ========================================================================
// NewNotificationEngine: Yeni bildirim motoru oluşturur
func NewNotificationEngine(cfg *config.KangalConfig, eventChan chan<- string) *NotificationEngine {
	// 🚨 DÜZELTME #1: Nil kontrolleri
	if cfg == nil {
		logger.Error("❌ [NotificationEngine] Config nil! Oluşturulamadı.")
		return nil
	}
	
	if eventChan == nil {
		logger.Error("❌ [NotificationEngine] EventChan nil! Oluşturulamadı.")
		return nil
	}
	
	logger.Info("🔔 [NotificationEngine] Bildirim motoru yapılandırılıyor...")
	
	return &NotificationEngine{
		Config:        cfg,
		EventChan:     eventChan,
		isRunning:     false,
		notifications: make([]Notification, 0, 100),
		alertTracker:  make(map[string]time.Time),
		stats: NotificationStats{
			ByPriority: make(map[Priority]int),
		},
	}
}

// ========================================================================
// 🚀 BAŞLATMA / DURDURMA
// ========================================================================
// Start: Bildirim motorunu başlatır
func (n *NotificationEngine) Start() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	
	if n.isRunning {
		logger.Warn("⚠️ [NotificationEngine] Zaten aktif, başlatma atlandı")
		return nil
	}
	
	// 🚨 DÜZELTME #2: Config enabled kontrolü
	if !n.Config.Enabled {
		logger.Debug("ℹ️ [NotificationEngine] Config'de disabled, başlatılmadı")
		return nil
	}
	
	// Context oluştur
	n.ctx, n.cancel = context.WithCancel(context.Background())
	
	n.isRunning = true
	
	// Arka planda rate limit temizliği
	go n.rateLimitCleanup()
	
	logger.Success("✅ [NotificationEngine] Bildirim motoru aktif!")
	return nil
}

// Stop: Bildirim motorunu güvenli şekilde durdurur
func (n *NotificationEngine) Stop() {
	n.mu.Lock()
	defer n.mu.Unlock()
	
	if !n.isRunning {
		logger.Debug("ℹ️ [NotificationEngine] Zaten durmuş")
		return
	}
	
	logger.Info("🛑 [NotificationEngine] Bildirim motoru durduruluyor...")
	
	if n.cancel != nil {
		n.cancel()
	}
	
	n.isRunning = false
	logger.Success("✅ [NotificationEngine] Bildirim motoru durduruldu")
}

// ========================================================================
// 📢 BİLDİRİM GÖNDERME
// ========================================================================
// SendAlert: Ana bildirim gönderme fonksiyonu (tüm kanalları yönetir)
func (n *NotificationEngine) SendAlert(priority Priority, title string, message string) {
	n.mu.RLock()
	running := n.isRunning
	n.mu.RUnlock()
	
	if !running {
		logger.Debug("ℹ️ [NotificationEngine] Bildirim atlandı (motor çalışmıyor)")
		return
	}
	
	// 🚨 DÜZELTME #3: Rate limiting kontrolü
	alertKey := fmt.Sprintf("%s:%s", priority, title)
	if !n.canSendAlert(alertKey) {
		logger.Debug("⏱️ [NotificationEngine] Rate limit nedeniyle alert atlandı: %s", alertKey)
		return
	}
	
	// 🚨 DÜZELTME #4: Quiet hours kontrolü
	if n.isQuietHours() && priority != PriorityCritical {
		logger.Debug("🌙 [NotificationEngine] Quiet hours, sadece kritik alert'ler gönderilir")
		return
	}
	
	// Bildirim objesi oluştur
	notification := Notification{
		ID:        fmt.Sprintf("notif_%d", time.Now().UnixNano()),
		Priority:  priority,
		Title:     title,
		Message:   message,
		Timestamp: time.Now(),
		Read:      false,
	}
	
	// Bildirim geçmişine ekle
	n.notifyMu.Lock()
	n.notifications = append(n.notifications, notification)
	// Son 100 bildirimi tut
	if len(n.notifications) > 100 {
		n.notifications = n.notifications[1:]
	}
	n.notifyMu.Unlock()
	
	// 🚨 DÜZELTME #5: Kanal bazlı dağıtım
	n.distributeNotification(notification)
	
	// İstatistikleri güncelle
	n.updateStats(priority)
	
	logger.Debug("🔔 [NotificationEngine] Alert gönderildi: [%s] %s", priority, title)
}

// distributeNotification: Bildirimi ilgili kanallara dağıtır
func (n *NotificationEngine) distributeNotification(notif Notification) {
	// 1. Terminal Inline (her zaman aktif)
	if n.Config.Notifications.Terminal {
		n.sendTerminalNotification(notif)
	}
	
	// 2. Windows Toast (sadece Windows'ta)
	if runtime.GOOS == "windows" && n.Config.Notifications.Toast {
		n.sendToastNotification(notif)
	}
	
	// 3. WhatsApp (sadece kritik alert'ler)
	if n.Config.Notifications.WhatsAppCritical && notif.Priority == PriorityCritical {
		n.sendWhatsAppNotification(notif)
	}
}

// ========================================================================
// 🖥️ TERMINAL BİLDİRİMLERİ
// ========================================================================
// sendTerminalNotification: Terminal inline mesaj gönderir
func (n *NotificationEngine) sendTerminalNotification(notif Notification) {
	var emoji string
	var colorCode string
	
	switch notif.Priority {
	case PriorityCritical:
		emoji = "🚨"
		colorCode = "\033[91m" // Kırmızı
	case PriorityWarning:
		emoji = "⚠️"
		colorCode = "\033[93m" // Sarı
	case PriorityInfo:
		emoji = "ℹ️"
		colorCode = "\033[94m" // Mavi
	}
	
	resetCode := "\033[0m"
	timestamp := notif.Timestamp.Format("15:04:05")
	
	terminalMsg := fmt.Sprintf("%s%s [%s] %s: %s%s",
		emoji, colorCode, timestamp, notif.Title, notif.Message, resetCode)
	
	// EventChan'a gönder (Pars event processor yakalayacak)
	select {
	case n.EventChan <- terminalMsg:
		n.statsMu.Lock()
		n.stats.TerminalCount++
		n.statsMu.Unlock()
	default:
		logger.Warn("⚠️ [NotificationEngine] EventChan dolu, terminal alert atlandı")
	}
}

// ========================================================================
// 🪟 WINDOWS TOAST BİLDİRİMLERİ
// ========================================================================
// sendToastNotification: Windows 10/11 native toast bildirim gönderir
func (n *NotificationEngine) sendToastNotification(notif Notification) {
	// 🚨 DÜZELTME #6: Sadece Windows'ta çalışır
	if runtime.GOOS != "windows" {
		return
	}
	
	var icon string
	switch notif.Priority {
	case PriorityCritical:
		icon = "error"
	case PriorityWarning:
		icon = "warning"
	case PriorityInfo:
		icon = "info"
	}
	
	// 🚨 DÜZELTME #7: PowerShell path'i bul (PATH sorunu önleme)
	psPath := findPowerShellPath()
	if psPath == "" {
		logger.Warn("⚠️ [NotificationEngine] PowerShell bulunamadı, Toast atlandı")
		// Fallback: Terminal'e gönder
		n.sendTerminalNotification(notif)
		return
	}
	
	// PowerShell ile toast bildirim gönder
	// 🚨 DÜZELTME #8: Context timeout ekle
	toastCtx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
	defer cancel()
	
	psScript := fmt.Sprintf(`
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null

$template = @"
<toast>
    <visual>
        <binding template="ToastText02">
            <text id="1">%s</text>
            <text id="2">%s</text>
        </binding>
    </visual>
    <audio src="ms-winsoundevent:Notification.%s" />
</toast>
"@

$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml($template)
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("Pars Agent").Show($toast)
`, notif.Title, notif.Message, icon)
	
	cmd := exec.CommandContext(toastCtx, psPath, "-NoProfile", "-Command", psScript)
	err := cmd.Run()
	
	if err != nil {
		logger.Debug("⚠️ [NotificationEngine] Toast gönderim hatası: %v", err)
		// Toast başarısız olsa bile terminal'e gönder (fallback)
		n.sendTerminalNotification(notif)
	} else {
		n.statsMu.Lock()
		n.stats.ToastCount++
		n.statsMu.Unlock()
		logger.Debug("✅ [NotificationEngine] Toast bildirim gönderildi")
	}
}

// 🆕 YENİ: findPowerShellPath - PowerShell executable'ını bul
func findPowerShellPath() string {
	// 1. Önce varsayılan yolda dene
	defaultPaths := []string{
		`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
		`C:\Windows\SysWOW64\WindowsPowerShell\v1.0\powershell.exe`,
	}
	
	for _, path := range defaultPaths {
		if _, err := os.Stat(path); err == nil {
			logger.Debug("🔍 [NotificationEngine] PowerShell bulundu: %s", path)
			return path
		}
	}
	
	// 2. PATH içinde dene (fallback)
	if path, err := exec.LookPath("powershell.exe"); err == nil {
		logger.Debug("🔍 [NotificationEngine] PowerShell PATH'te bulundu: %s", path)
		return path
	}
	
	// 3. Sadece "powershell" dene
	if path, err := exec.LookPath("powershell"); err == nil {
		logger.Debug("🔍 [NotificationEngine] PowerShell (generic) bulundu: %s", path)
		return path
	}
	
	// Hiçbiri bulunamadı
	return ""
}

// ========================================================================
// 📱 WHATSAPP BİLDİRİMLERİ
// ========================================================================
// sendWhatsAppNotification: Kritik alert'leri WhatsApp'a gönderir
func (n *NotificationEngine) sendWhatsAppNotification(notif Notification) {
	// 🚨 DÜZELTME #9: WhatsApp listener kontrolü
	if n.whatsappListener == nil {
		logger.Debug("⚠️ [NotificationEngine] WhatsApp listener nil, alert atlandı")
		return
	}
	
	// WhatsApp broadcast fonksiyonunu çağır (type assertion ile)
	// 🚨 DÜZELTME #10: Panic recovery ekle
	defer func() {
		if r := recover(); r != nil {
			logger.Error("🚨 [NotificationEngine] WhatsApp broadcast panic: %v", r)
		}
	}()
	
	// EventChan'a özel WhatsApp alert mesajı gönder
	whatsappMsg := fmt.Sprintf("[KANGAL KRİTİK ALERT]\n\n🚨 %s\n\n%s\n\n⏰ %s",
		notif.Title,
		notif.Message,
		notif.Timestamp.Format("15:04:05"))
	
	select {
	case n.EventChan <- whatsappMsg:
		n.statsMu.Lock()
		n.stats.WhatsAppCount++
		n.statsMu.Unlock()
		logger.Debug("📱 [NotificationEngine] WhatsApp alert gönderildi")
	default:
		logger.Warn("⚠️ [NotificationEngine] EventChan dolu, WhatsApp alert atlandı")
	}
}

// ========================================================================
// ⏱️ RATE LIMITING
// ========================================================================
// canSendAlert: Rate limiting kontrolü (aynı alert çok sık gönderilmesin)
func (n *NotificationEngine) canSendAlert(alertKey string) bool {
	n.alertMu.Lock()
	defer n.alertMu.Unlock()
	
	// Cooldown süresi (priority'ye göre)
	var cooldown time.Duration
	switch {
	case strings.Contains(alertKey, string(PriorityCritical)):
		cooldown = 1 * time.Minute // Kritik: 1 dakika
	case strings.Contains(alertKey, string(PriorityWarning)):
		cooldown = 5 * time.Minute // Uyarı: 5 dakika
	default:
		cooldown = 10 * time.Minute // Info: 10 dakika
	}
	
	if lastSent, exists := n.alertTracker[alertKey]; exists {
		if time.Since(lastSent) < cooldown {
			return false
		}
	}
	
	n.alertTracker[alertKey] = time.Now()
	return true
}

// rateLimitCleanup: Eski rate limit kayıtlarını temizler (memory leak önleme)
func (n *NotificationEngine) rateLimitCleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			n.alertMu.Lock()
			now := time.Now()
			for key, lastSent := range n.alertTracker {
				if now.Sub(lastSent) > 1*time.Hour {
					delete(n.alertTracker, key)
				}
			}
			n.alertMu.Unlock()
		}
	}
}

// ========================================================================
// 🌙 QUIET HOURS
// ========================================================================
// isQuietHours: Şu an sessiz saatler mi kontrol et
func (n *NotificationEngine) isQuietHours() bool {
	if !n.Config.QuietHours.Enabled {
		return false
	}
	
	now := time.Now()
	currentTime := now.Format("15:04")
	
	startTime := n.Config.QuietHours.Start
	endTime := n.Config.QuietHours.End
	
	// Basit string karşılaştırma (HH:MM formatı)
	if startTime <= endTime {
		// Normal aralık (örn: 23:00 - 07:00)
		return currentTime >= startTime || currentTime <= endTime
	} else {
		// Gece aralığı (örn: 23:00 - 07:00, geceyi跨越 eder)
		return currentTime >= startTime || currentTime <= endTime
	}
}

// ========================================================================
// 📊 İSTATİSTİKLER
// ========================================================================
// updateStats: Bildirim istatistiklerini güncelle
func (n *NotificationEngine) updateStats(priority Priority) {
	n.statsMu.Lock()
	defer n.statsMu.Unlock()
	
	n.stats.TotalSent++
	n.stats.LastHourCount++
	n.stats.ByPriority[priority]++
}

// IsRunning: Bildirim motorunun çalışıp çalışmadığını döndür
func (n *NotificationEngine) IsRunning() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.isRunning
}

// GetStatus: Bildirim motoru durum raporunu döndür
func (n *NotificationEngine) GetStatus() map[string]interface{} {
	n.mu.RLock()
	n.statsMu.RLock()
	n.notifyMu.RLock()
	defer n.mu.RUnlock()
	defer n.statsMu.RUnlock()
	defer n.notifyMu.RUnlock()
	
	lastNotificationTime := "Hiç bildirim gönderilmedi"
	if len(n.notifications) > 0 {
		lastNotificationTime = n.notifications[len(n.notifications)-1].Timestamp.Format("15:04:05")
	}
	
	return map[string]interface{}{
		"is_running":          n.isRunning,
		"total_sent":          n.stats.TotalSent,
		"toast_count":         n.stats.ToastCount,
		"terminal_count":      n.stats.TerminalCount,
		"whatsapp_count":      n.stats.WhatsAppCount,
		"last_hour_count":     n.stats.LastHourCount,
		"by_priority":         n.stats.ByPriority,
		"last_notification":   lastNotificationTime,
		"quiet_hours_enabled": n.Config.QuietHours.Enabled,
		"toast_enabled":       n.Config.Notifications.Toast,
		"terminal_enabled":    n.Config.Notifications.Terminal,
		"whatsapp_critical":   n.Config.Notifications.WhatsAppCritical,
	}
}

// GetRecentNotifications: Son bildirimleri döndür (debug için)
func (n *NotificationEngine) GetRecentNotifications(limit int) []Notification {
	n.notifyMu.RLock()
	defer n.notifyMu.RUnlock()
	
	if limit <= 0 || limit > len(n.notifications) {
		limit = len(n.notifications)
	}
	
	start := len(n.notifications) - limit
	if start < 0 {
		start = 0
	}
	
	return n.notifications[start:]
}

// SetWhatsAppListener: WhatsApp listener'ı ayarla (kritik alert'ler için)
func (n *NotificationEngine) SetWhatsAppListener(listener interface{}) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.whatsappListener = listener
	logger.Debug("📱 [NotificationEngine] WhatsApp listener ayarlandı")
}

// MarkAllRead: Tüm bildirimleri okundu olarak işaretle
func (n *NotificationEngine) MarkAllRead() {
	n.notifyMu.Lock()
	defer n.notifyMu.Unlock()
	
	for i := range n.notifications {
		n.notifications[i].Read = true
	}
	logger.Debug("📖 [NotificationEngine] Tüm bildirimler okundu olarak işaretlendi")
}

// ClearHistory: Bildirim geçmişini temizle
func (n *NotificationEngine) ClearHistory() {
	n.notifyMu.Lock()
	defer n.notifyMu.Unlock()
	
	count := len(n.notifications)
	n.notifications = make([]Notification, 0, 100)
	logger.Debug("🧹 [NotificationEngine] Bildirim geçmişi temizlendi: %d adet", count)
}

// ========================================================================
// 🎯 HELPER FONKSİYONLAR
// ========================================================================
// SendCritical: Kritik öncelikli bildirim gönder (shortcut)
func (n *NotificationEngine) SendCritical(title string, message string) {
	n.SendAlert(PriorityCritical, title, message)
}

// SendWarning: Uyarı öncelikli bildirim gönder (shortcut)
func (n *NotificationEngine) SendWarning(title string, message string) {
	n.SendAlert(PriorityWarning, title, message)
}

// SendInfo: Bilgi öncelikli bildirim gönder (shortcut)
func (n *NotificationEngine) SendInfo(title string, message string) {
	n.SendAlert(PriorityInfo, title, message)
}

// TestNotification: Test bildirimi gönder (debug için)
func (n *NotificationEngine) TestNotification() {
	n.SendInfo("🧪 Kangal Test", "Bildirim motoru çalışıyor! Test bildirimi başarılı.")
	logger.Success("✅ [NotificationEngine] Test bildirimi gönderildi")
}
package kangal

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/config"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

type Priority string

const (
	PriorityCritical Priority = "critical"
	PriorityWarning  Priority = "warning"
	PriorityInfo     Priority = "info"
)

type Notification struct {
	ID        string
	Priority  Priority
	Title     string
	Message   string
	Timestamp time.Time
	Channel   string
	Read      bool
}
type NotificationEngine struct {
	Config        *config.KangalConfig
	EventChan     chan<- string
	ctx           context.Context
	cancel        context.CancelFunc
	isRunning     bool
	mu            sync.RWMutex
	
	notifications []Notification
	notifyMu      sync.RWMutex
	
	alertTracker  map[string]time.Time
	alertMu       sync.RWMutex
	
	stats         NotificationStats
	statsMu       sync.RWMutex
	
	whatsappListener interface{}
}

type NotificationStats struct {
	TotalSent      int
	ToastCount     int
	TerminalCount  int
	WhatsAppCount  int
	LastHourCount  int
	ByPriority     map[Priority]int
}


func NewNotificationEngine(cfg *config.KangalConfig, eventChan chan<- string) *NotificationEngine {
	if cfg == nil || eventChan == nil {
		logger.Error("❌ [NotificationEngine] Gerekli bileşenler nil! Oluşturulamadı.")
		return nil
	}
	
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

func (n *NotificationEngine) Start() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	
	if n.isRunning {
		return nil
	}
	
	if !n.Config.Enabled {
		return nil
	}
	
	n.ctx, n.cancel = context.WithCancel(context.Background())
	n.isRunning = true
	
	go n.rateLimitCleanup()
	
	logger.Success("✅ [NotificationEngine] Bildirim motoru aktif!")
	return nil
}

func (n *NotificationEngine) Stop() {
	n.mu.Lock()
	defer n.mu.Unlock()
	
	if !n.isRunning {
		return
	}
	
	if n.cancel != nil {
		n.cancel()
	}
	
	n.isRunning = false
	logger.Success("✅ [NotificationEngine] Bildirim motoru durduruldu")
}

func (n *NotificationEngine) SendAlert(priority Priority, title string, message string) {
	n.mu.RLock()
	running := n.isRunning
	n.mu.RUnlock()
	
	if !running {
		return
	}
	
	alertKey := fmt.Sprintf("%s:%s", priority, title)
	if !n.canSendAlert(alertKey, priority) {
		return
	}
	
	if n.isQuietHours() && priority != PriorityCritical {
		logger.Debug("🌙 [NotificationEngine] Sessiz saatlerde bildirim engellendi: %s", title)
		return
	}
	
	notification := Notification{
		ID:        fmt.Sprintf("notif_%d", time.Now().UnixNano()),
		Priority:  priority,
		Title:     title,
		Message:   message,
		Timestamp: time.Now(),
		Read:      false,
	}
	
	n.notifyMu.Lock()
	n.notifications = append(n.notifications, notification)
	if len(n.notifications) > 100 {
		n.notifications = n.notifications[1:]
	}
	n.notifyMu.Unlock()
	
	n.distributeNotification(notification)
	n.updateStats(priority)
}

func (n *NotificationEngine) distributeNotification(notif Notification) {
	if n.Config.Notifications.Terminal {
		n.sendTerminalNotification(notif)
	}
	
	if runtime.GOOS == "windows" && n.Config.Notifications.Toast {
		n.sendToastNotification(notif)
	}
	
	if n.Config.Notifications.WhatsAppCritical && notif.Priority == PriorityCritical {
		n.sendWhatsAppNotification(notif)
	}
}

func (n *NotificationEngine) sendTerminalNotification(notif Notification) {
	var emoji, color string
	switch notif.Priority {
	case PriorityCritical: emoji, color = "🚨", "\033[91m"
	case PriorityWarning:  emoji, color = "⚠️", "\033[93m"
	default:               emoji, color = "ℹ️", "\033[94m"
	}
	
	terminalMsg := fmt.Sprintf("%s%s [%s] %s: %s\033[0m", 
		emoji, color, notif.Timestamp.Format("15:04:05"), notif.Title, notif.Message)
	
	select {
	case n.EventChan <- terminalMsg:
		n.statsMu.Lock()
		n.stats.TerminalCount++
		n.statsMu.Unlock()
	default:
	}
}

func (n *NotificationEngine) sendToastNotification(notif Notification) {
	if runtime.GOOS != "windows" { return }
	
	psPath := findPowerShellPath()
	if psPath == "" { return }
	
	toastCtx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
	defer cancel()
	
	icon := "info"
	if notif.Priority == PriorityCritical { icon = "error" } else if notif.Priority == PriorityWarning { icon = "warning" }
	
	psScript := fmt.Sprintf(`
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null
$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml("<toast><visual><binding template='ToastText02'><text id='1'>%s</text><text id='2'>%s</text></binding></visual><audio src='ms-winsoundevent:Notification.%s' /></toast>")
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("Pars Agent").Show($toast)`, notif.Title, notif.Message, icon)
	
	if err := exec.CommandContext(toastCtx, psPath, "-NoProfile", "-Command", psScript).Run(); err == nil {
		n.statsMu.Lock()
		n.stats.ToastCount++
		n.statsMu.Unlock()
	}
}

func findPowerShellPath() string {
	if path, err := exec.LookPath("powershell.exe"); err == nil { return path }
	return `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`
}

func (n *NotificationEngine) sendWhatsAppNotification(notif Notification) {
	if n.whatsappListener == nil { return }
	
	whatsappMsg := fmt.Sprintf("[KANGAL KRİTİK ALERT]\n\n🚨 %s\n\n%s\n\n⏰ %s",
		notif.Title, notif.Message, notif.Timestamp.Format("15:04:05"))
	
	select {
	case n.EventChan <- whatsappMsg:
		n.statsMu.Lock()
		n.stats.WhatsAppCount++
		n.statsMu.Unlock()
	default:
	}
}

func (n *NotificationEngine) canSendAlert(alertKey string, priority Priority) bool {
	n.alertMu.Lock()
	defer n.alertMu.Unlock()
	
	cooldown := 10 * time.Minute
	if priority == PriorityCritical { cooldown = 1 * time.Minute } else if priority == PriorityWarning { cooldown = 5 * time.Minute }
	
	if lastSent, exists := n.alertTracker[alertKey]; exists && time.Since(lastSent) < cooldown {
		return false
	}
	
	n.alertTracker[alertKey] = time.Now()
	return true
}

func (n *NotificationEngine) rateLimitCleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-n.ctx.Done(): return
		case <-ticker.C:
			n.alertMu.Lock()
			now := time.Now()
			for key, lastSent := range n.alertTracker {
				if now.Sub(lastSent) > 1*time.Hour { delete(n.alertTracker, key) }
			}
			n.alertMu.Unlock()
		}
	}
}

func (n *NotificationEngine) isQuietHours() bool {
	if !n.Config.QuietHours.Enabled { return false }
	
	now := time.Now().Format("15:04")
	start, end := n.Config.QuietHours.Start, n.Config.QuietHours.End
	
	if start <= end {
		return now >= start && now <= end
	}
	return now >= start || now <= end
}

func (n *NotificationEngine) updateStats(priority Priority) {
	n.statsMu.Lock()
	defer n.statsMu.Unlock()
	n.stats.TotalSent++
	n.stats.LastHourCount++
	n.stats.ByPriority[priority]++
}

func (n *NotificationEngine) IsRunning() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.isRunning
}

func (n *NotificationEngine) GetStatus() map[string]interface{} {
	n.mu.RLock()
	n.statsMu.RLock()
	defer n.mu.RUnlock()
	defer n.statsMu.RUnlock()
	
	return map[string]interface{}{
		"total_sent": n.stats.TotalSent,
		"whatsapp":   n.stats.WhatsAppCount,
		"is_running": n.isRunning,
	}
}

func (n *NotificationEngine) GetRecentNotifications(limit int) []Notification {
	n.notifyMu.RLock()
	defer n.notifyMu.RUnlock()
	if limit <= 0 || limit > len(n.notifications) { limit = len(n.notifications) }
	return n.notifications[len(n.notifications)-limit:]
}

func (n *NotificationEngine) SetWhatsAppListener(listener interface{}) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.whatsappListener = listener
}

func (n *NotificationEngine) SendCritical(t, m string) { n.SendAlert(PriorityCritical, t, m) }
func (n *NotificationEngine) SendWarning(t, m string)  { n.SendAlert(PriorityWarning, t, m) }
func (n *NotificationEngine) SendInfo(t, m string)     { n.SendAlert(PriorityInfo, t, m) }

func (n *NotificationEngine) TestNotification() {
	n.SendInfo("🧪 Kangal Test", "Bildirim motoru çalışıyor!")
}
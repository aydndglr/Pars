package kangal

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

type KangalControlTool struct {
	kangal *Kangal // Kangal instance'a referans
}


func NewKangalControlTool(k *Kangal) *KangalControlTool {
	if k == nil {
		logger.Error("❌ [KangalControlTool] Kangal instance nil! Araç oluşturulamadı.")
		return nil
	}

	return &KangalControlTool{
		kangal: k,
	}
}

func (t *KangalControlTool) Name() string {
	return "kangal_control"
}

func (t *KangalControlTool) Description() string {
	return `KANGAL BEKÇİ SİSTEMİ KONTROL ARACI. Kullanıcının Kangal'ı yönetmesini sağlar.
Aksiyonlar (action):
- "status": Kangal'ın mevcut durumunu raporlar (aktif mi, hangi model, kaç alert vb.)
- "enable": Kangal'ı aktif eder (watchdog_model config'de tanımlı olmalı)
- "disable": Kangal'ı geçici olarak devre dışı bırakır (sessiz mod)
- "sensitivity": Hassasiyet seviyesini değiştirir (low/balanced/high)
- "alerts": Son Kangal alert'lerini listeler (son 10 alert)
- "test": Test bildirimi gönderir (sistem çalışıyor mu kontrol için)
- "quiet_hours": Rahatsız etme modunu aç/kapa

Kullanıcı "Kangal'ı aç/kapat", "bekçi sistemini aktif et", "alert'leri göster" dediğinde bu aracı kullan.`
}

func (t *KangalControlTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Yapılacak işlem (status/enable/disable/sensitivity/alerts/test/quiet_hours)",
				"enum":        []string{"status", "enable", "disable", "sensitivity", "alerts", "test", "quiet_hours"},
			},
			"level": map[string]interface{}{
				"type":        "string",
				"description": "Sadece 'sensitivity' action için: low/balanced/high",
				"enum":        []string{"low", "balanced", "high"},
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Sadece 'alerts' action için: Kaç alert gösterilecek (varsayılan: 10, max: 50)",
				"default":     10,
			},
			"enable_quiet": map[string]interface{}{
				"type":        "boolean",
				"description": "Sadece 'quiet_hours' action için: true=aktif et, false=pasif et",
			},
		},
		"required": []string{"action"},
	}
}

func (t *KangalControlTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if t == nil {
		return "", fmt.Errorf("KangalControlTool nil")
	}
	if t.kangal == nil {
		return "", fmt.Errorf("Kangal instance nil")
	}
	actionRaw, ok := args["action"]
	if !ok || actionRaw == nil {
		return "", fmt.Errorf("'action' parametresi eksik")
	}

	action, ok := actionRaw.(string)
	if !ok {
		return "", fmt.Errorf("'action' parametresi string formatında olmalı")
	}

	logger.Info("🐕 [KangalControl] Action: %s", action)

	switch action {
	case "status":
		return t.getStatus()
	case "enable":
		return t.enable()
	case "disable":
		return t.disable()
	case "sensitivity":
		level, _ := args["level"].(string)
		return t.setSensitivity(level)
	case "alerts":
		limitRaw, _ := args["limit"].(float64)
		limit := int(limitRaw)
		if limit <= 0 {
			limit = 10
		}
		if limit > 50 {
			limit = 50
		}
		return t.getAlerts(limit)
	case "test":
		return t.testNotification()
	case "quiet_hours":
		enableQuiet, _ := args["enable_quiet"].(bool)
		return t.toggleQuietHours(enableQuiet)
	default:
		return "", fmt.Errorf("geçersiz action: %s (status/enable/disable/sensitivity/alerts/test/quiet_hours)", action)
	}
}

func (t *KangalControlTool) getStatus() (string, error) {
	if !t.kangal.IsRunning() {
		return "🐕 **KANGAL DURUMU:** Devre Dışı\n\nKangal bekçi sistemi şu anda aktif değil. Aktif etmek için 'enable' action'ını kullan.", nil
	}

	status := t.kangal.GetStatus()

	var sb strings.Builder
	sb.WriteString("🐕 **KANGAL BEKÇİ SİSTEMİ DURUM RAPORU** 🐕\n")
	sb.WriteString(strings.Repeat("=", 50) + "\n\n")

	sb.WriteString(fmt.Sprintf("🟢 **Durum:** Aktif\n"))
	sb.WriteString(fmt.Sprintf("⚙️ **Hassasiyet:** %s\n", t.kangal.Config.SensitivityLevel))
	sb.WriteString(fmt.Sprintf("🧠 **Watchdog Model:** %s\n", t.kangal.Config.WatchdogModel))
	sb.WriteString(fmt.Sprintf("📊 **İzlenen Uygulama:** %d adet\n", len(t.kangal.Config.TrackedApps)))
	sb.WriteString("\n")

	if windowTracker, ok := status["window_tracker"].(map[string]interface{}); ok {
		sb.WriteString(fmt.Sprintf("🪟 **Window Tracker:** %v\n", windowTracker["is_running"]))
	}
	if errorDetector, ok := status["error_detector"].(map[string]interface{}); ok {
		sb.WriteString(fmt.Sprintf("🚨 **Error Detector:** %v (Toplam: %v)\n",
			errorDetector["is_running"], errorDetector["total_detected"]))
	}
	if notification, ok := status["notification"].(map[string]interface{}); ok {
		sb.WriteString(fmt.Sprintf("🔔 **Notification Engine:** %v (Gönderilen: %v)\n",
			notification["is_running"], notification["total_sent"]))
	}
	if watchdog, ok := status["watchdog"].(map[string]interface{}); ok {
		sb.WriteString(fmt.Sprintf("🧠 **Watchdog:** %v (Analiz: %v, Escalate: %v)\n",
			watchdog["is_running"], watchdog["total_events"], watchdog["escalated_count"]))
	}
	if contextTracker, ok := status["context_tracker"].(map[string]interface{}); ok {
		sb.WriteString(fmt.Sprintf("📊 **Context Tracker:** %v (Aktif App: %s)\n",
			contextTracker["is_running"], contextTracker["active_app"]))
	}

	sb.WriteString("\n" + strings.Repeat("=", 50) + "\n")
	sb.WriteString("💡 **İpucu:** Kangal'ı kapatmak için 'disable', açmak için 'enable' action'ını kullan.")

	return sb.String(), nil
}

func (t *KangalControlTool) enable() (string, error) {
	if !t.kangal.Config.IsWatchdogEnabled() {
		return "❌ **KANGAL AKTİF EDİLEMEDİ**\n\n" +
			"⚠️ **Sebep:** `watchdog_model` config'de tanımlı değil!\n\n" +
			"🔧 **Çözüm:** `config/config.yaml` dosyasında `kangal.watchdog_model` değerini ayarla (örn: `qwen3:1.5b`).\n\n" +
			"📝 **Not:** watchdog_model boşsa Kangal sadece sınırlı özelliklerle çalışır.", nil
	}

	err := t.kangal.Start()
	if err != nil {
		if strings.Contains(err.Error(), "zaten aktif") {
			return "ℹ️ **Kangal zaten aktif!**\n\nBekçi sistemi zaten çalışıyor. Durumunu sorgulamak için 'status' action'ını kullan.", nil
		}
		return "", fmt.Errorf("Kangal aktif edilemedi: %v", err)
	}

	return "✅ **KANGAL AKTİF EDİLDİ!** 🐕\n\n" +
		"🟢 Bekçi sistemi artık proaktif izleme yapıyor.\n" +
		"🧠 Watchdog Model: " + t.kangal.Config.WatchdogModel + "\n" +
		"⚙️ Hassasiyet: " + t.kangal.Config.SensitivityLevel + "\n\n" +
		"💡 **İpucu:** Hata veya öneri alert'leri terminal ve/veya WhatsApp üzerinden iletilecek.", nil
}

func (t *KangalControlTool) disable() (string, error) {
	if !t.kangal.IsRunning() {
		return "ℹ️ **Kangal zaten devre dışı!**\n\nBekçi sistemi zaten çalışmıyor.", nil
	}

	t.kangal.stopSubModules()

	return "🌙 **KANGAL DEVRE DIŞI BIRAKILDI** 🐕\n\n" +
		"⚫ Proaktif izleme durduruldu.\n" +
		"📝 Not: Kangal'ı tekrar aktif etmek için 'enable' action'ını kullan.\n\n" +
		"💡 **İpucu:** Kısa süreli sessizlik için 'quiet_hours' action'ını kullanabilirsin.", nil
}

func (t *KangalControlTool) setSensitivity(level string) (string, error) {
	validLevels := map[string]bool{
		"low":      true,
		"balanced": true,
		"high":     true,
	}

	if level == "" {
		return "⚠️ **Hassasiyet Seviyesi Belirtilmedi**\n\n" +
			"🔧 **Kullanım:** action='sensitivity' ve level='low/balanced/high' gönder.\n\n" +
			"📊 **Seviyeler:**\n" +
			"- `low`: Sadece kritik hatalar (daha az alert)\n" +
			"- `balanced`: Kritik + uyarılar (önerilen)\n" +
			"- `high`: Tüm olaylar (çok fazla alert)", nil
	}

	if !validLevels[level] {
		return "", fmt.Errorf("geçersiz level: %s (low/balanced/high)", level)
	}

	t.kangal.SetSensitivity(level)

	levelDesc := map[string]string{
		"low":      "Sadece kritik hatalar",
		"balanced": "Kritik + uyarılar (önerilen)",
		"high":     "Tüm olaylar (detaylı izleme)",
	}

	return "✅ **HASSASİYET DEĞİŞTİRİLDİ** 🐕\n\n" +
		fmt.Sprintf("📊 Yeni Seviye: **%s**\n", level) +
		fmt.Sprintf("📝 Açıklama: %s\n\n", levelDesc[level]) +
		"💡 **İpucu:** Çok fazla alert alıyorsan 'low', daha proaktif olsun istiyorsan 'high' kullan.", nil
}

func (t *KangalControlTool) getAlerts(limit int) (string, error) {
	notification := t.kangal.GetNotification()
	if notification == nil {
		return "⚠️ **Alert Geçmişi Bulunamadı**\n\n" +
			"Notification engine aktif değil. Kangal'ın çalıştığından emin ol.", nil
	}

	alerts := notification.GetRecentNotifications(limit)

	if len(alerts) == 0 {
		return "📭 **Son Alert Yok**\n\n" +
			"Son " + fmt.Sprintf("%d", limit) + " alert içinde kayıtlı bildirim bulunamadı.\n\n" +
			"💡 **İpucu:** Test bildirimi göndermek için 'test' action'ını kullan.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔔 **SON %d KANGAL ALERT'İ** 🔔\n", len(alerts)))
	sb.WriteString(strings.Repeat("=", 50) + "\n\n")

	for i, alert := range alerts {
		emoji := "ℹ️"
		if alert.Priority == "critical" {
			emoji = "🚨"
		} else if alert.Priority == "warning" {
			emoji = "⚠️"
		}

		sb.WriteString(fmt.Sprintf("%d. %s [%s] %s\n", i+1, emoji, alert.Timestamp.Format("15:04:05"), alert.Title))
		sb.WriteString(fmt.Sprintf("   📝 %s\n", alert.Message))
		sb.WriteString(fmt.Sprintf("   📺 Kanal: %s\n\n", alert.Channel))
	}

	sb.WriteString(strings.Repeat("=", 50) + "\n")
	sb.WriteString("💡 **İpucu:** Daha fazla alert görmek için limit parametresini artır.")

	return sb.String(), nil
}

func (t *KangalControlTool) testNotification() (string, error) {
	notification := t.kangal.GetNotification()
	if notification == nil {
		return "❌ **Test Başarısız**\n\n" +
			"Notification engine aktif değil. Kangal'ın çalıştığından emin ol.", nil
	}

	notification.TestNotification()

	return "✅ **TEST BAŞARILI!** 🐕\n\n" +
		"🔔 Test bildirimi gönderildi.\n" +
		"📺 Terminal ve/veya WhatsApp üzerinden kontrol et.\n\n" +
		"💡 **İpucu:** Bildirim almadıysan config'de notification ayarlarını kontrol et.", nil
}

func (t *KangalControlTool) toggleQuietHours(enableQuiet bool) (string, error) {
	notification := t.kangal.GetNotification()
	if notification == nil {
		return "⚠️ **Quiet Hours Kontrol Edilemedi**\n\n" +
			"Notification engine aktif değil.", nil
	}

	currentQuiet := t.kangal.Config.QuietHours.Enabled

	if enableQuiet == currentQuiet {
		if enableQuiet {
			return "ℹ️ **Quiet Hours Zaten Aktif**\n\n" +
				"🌙 Rahatsız etme modu zaten açık. Sadece kritik alert'ler gönderiliyor.\n\n" +
				"💡 **İpucu:** Kapatmak için enable_quiet=false gönder.", nil
		}
		return "ℹ️ **Quiet Hours Zaten Pasif**\n\n" +
			"☀️ Rahatsız etme modu kapalı. Tüm alert'ler gönderiliyor.\n\n" +
			"💡 **İpucu:** Aktif etmek için enable_quiet=true gönder.", nil
	}

	t.kangal.Config.QuietHours.Enabled = enableQuiet
	warningNote := "\n⚠️ **Not:** Bu ayar geçicidir (Runtime). Sistemi yeniden başlattığında kalıcı olması için config.yaml dosyasını güncellemelisin.\n"

	if enableQuiet {
		return "🌙 **QUIET HOURS AKTİF EDİLDİ** 🐕\n\n" +
			"😴 Rahatsız etme modu aktif.\n" +
			"📝 Sadece KRİTİK alert'ler gönderilecek.\n" +
			fmt.Sprintf("⏰ Sessiz Saatler: %s - %s\n\n",
				t.kangal.Config.QuietHours.Start,
				t.kangal.Config.QuietHours.End) +
			warningNote +
			"💡 **İpucu:** Normal moda dönmek için enable_quiet=false gönder.", nil
	}

	return "☀️ **QUIET HOURS DEVRE DIŞI** 🐕\n\n" +
		"😃 Rahatsız etme modu kapalı.\n" +
		"📝 Tüm alert'ler (kritik + uyarı + info) gönderilecek.\n\n" +
		warningNote +
		"💡 **İpucu:** Gece modu için enable_quiet=true gönder.", nil
}

func (t *KangalControlTool) GetKangal() *Kangal {
	return t.kangal
}

func (t *KangalControlTool) IsEnabled() bool {
	if t.kangal == nil {
		return false
	}
	return t.kangal.IsRunning()
}

func (t *KangalControlTool) GetConfig() map[string]interface{} {
	if t.kangal == nil || t.kangal.Config == nil {
		return map[string]interface{}{
			"error": "Kangal veya Config nil",
		}
	}

	return map[string]interface{}{
		"enabled":           t.kangal.Config.Enabled,
		"sensitivity":       t.kangal.Config.SensitivityLevel,
		"watchdog_model":    t.kangal.Config.WatchdogModel,
		"tracked_apps":      len(t.kangal.Config.TrackedApps),
		"quiet_hours":       t.kangal.Config.QuietHours.Enabled,
		"toast_enabled":     t.kangal.Config.Notifications.Toast,
		"terminal_enabled":  t.kangal.Config.Notifications.Terminal,
		"whatsapp_critical": t.kangal.Config.Notifications.WhatsAppCritical,
	}
}

func (t *KangalControlTool) GetStatus() map[string]interface{} {
	if t == nil {
		return map[string]interface{}{
			"error": "KangalControlTool nil",
		}
	}

	return map[string]interface{}{
		"name":       t.Name(),
		"enabled":    t.IsEnabled(),
		"kangal_ok":  t.kangal != nil,
		"config":     t.GetConfig(),
		"timestamp":  time.Now().Format("15:04:05"),
	}
}
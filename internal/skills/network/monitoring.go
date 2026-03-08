package network

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

// MonitorTask: Arka planda çalışan izleme görevi
type MonitorTask struct {
	Host     string
	Interval time.Duration
	StopChan chan struct{}
}

var (
	activeMonitors = make(map[string]*MonitorTask)
	monitorMu      sync.Mutex
)

type NetworkMonitoringTool struct{}

func (t *NetworkMonitoringTool) Name() string { return "network_monitoring" }

func (t *NetworkMonitoringTool) Description() string {
	return `İLERİ SEVİYE AĞ ANALİZ VE İZLEME SERVİSİ.
- 'scan': Hedefin IP, gecikme ve kritik port (80, 443, 22, 3389 vb.) durumlarını analiz eder.
- 'monitor_start': Hedefi arka planda sürekli izlemeye alır. Zaman aşımı veya kopma olduğunda WHATSAPP üzerinden anında uyarı gönderir.
- 'monitor_stop': Aktif bir izleme görevini durdurur.`
}

func (t *NetworkMonitoringTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action":   map[string]interface{}{"type": "string", "enum": []string{"scan", "monitor_start", "monitor_stop"}},
			"host":     map[string]interface{}{"type": "string", "description": "İzlenecek/Taranacak IP veya Domain (Örn: '8.8.8.8' veya 'google.com')"},
			"interval": map[string]interface{}{"type": "integer", "description": "İzleme aralığı (Saniye cinsinden, varsayılan 60)."},
		},
		"required": []string{"action", "host"},
	}
}

func (t *NetworkMonitoringTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	action, _ := args["action"].(string)
	host, _ := args["host"].(string)

	switch action {
	case "scan":
		return t.performDetailedScan(host)

	case "monitor_start":
		intervalSec := 60
		if val, ok := args["interval"].(float64); ok {
			intervalSec = int(val)
		}
		return t.startBackgroundMonitor(host, time.Duration(intervalSec)*time.Second)

	case "monitor_stop":
		return t.stopBackgroundMonitor(host)
	}

	return "Geçersiz aksiyon balım.", nil
}

// scan: Detaylı tek seferlik analiz
func (t *NetworkMonitoringTool) performDetailedScan(host string) (string, error) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, "80"), 5*time.Second)
	latency := time.Since(start)

	status := "✅ Erişilebilir"
	if err != nil {
		status = fmt.Sprintf("❌ Erişilemez (%v)", err)
	} else {
		conn.Close()
	}

	// Yaygın portları hızlıca kontrol edelim
	ports := []string{"22", "80", "443", "3389", "8080"}
	var openPorts []string
	for _, port := range ports {
		pConn, pErr := net.DialTimeout("tcp", net.JoinHostPort(host, port), 1*time.Second)
		if pErr == nil {
			openPorts = append(openPorts, port)
			pConn.Close()
		}
	}

	return fmt.Sprintf("🔍 **AĞ ANALİZ RAPORU (%s)**\n------------------\n📍 Durum: %s\n⚡ Gecikme: %v\n🚪 Açık Portlar: %s", 
		host, status, latency, fmt.Sprintf("[%s]", fmt.Sprint(openPorts))), nil
}

// monitor_start: Arka plan döngüsünü başlatır
func (t *NetworkMonitoringTool) startBackgroundMonitor(host string, interval time.Duration) (string, error) {
	monitorMu.Lock()
	if _, exists := activeMonitors[host]; exists {
		monitorMu.Unlock()
		return fmt.Sprintf("⚠️ %s zaten izleniyor patron.", host), nil
	}

	stopChan := make(chan struct{})
	activeMonitors[host] = &MonitorTask{Host: host, Interval: interval, StopChan: stopChan}
	monitorMu.Unlock()

	// 🚀 ARKA PLAN NÖBETÇİSİ (Goroutine)
	go func() {
		logger.Action("📡 %s için otonom ağ nöbeti başladı. (Aralık: %v)", host, interval)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		lastStatus := true // Başlangıçta sağlam varsayıyoruz

		for {
			select {
			case <-ticker.C:
				// TCP Handshake ile kontrol (Ping'den daha kesin sonuç verir)
				conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, "80"), 5*time.Second)
				currentStatus := (err == nil)
				if conn != nil { conn.Close() }

				if !currentStatus && lastStatus {
					// 🚨 DURUM DEĞİŞTİ: BAĞLANTI KOPTU!
					// logger.Error çağrısı otomatik olarak WhatsApp'a bildirim fırlatır! [cite: 26, 242]
					logger.Error("❌ [AĞ KESİNTİSİ] %s sunucusuna erişim kaybedildi! Zaman aşımı veya servis kapalı.", host)
				} else if currentStatus && !lastStatus {
					// ✅ DURUM DEĞİŞTİ: BAĞLANTI GERİ GELDİ!
					logger.Success("✅ [AĞ GERİ GELDİ] %s sunucusu tekrar çevrimiçi.", host)
				}
				lastStatus = currentStatus

			case <-stopChan:
				logger.Warn("🛑 %s için ağ nöbeti sonlandırıldı.", host)
				return
			}
		}
	}()

	return fmt.Sprintf("📡 ONAY: %s sunucusu arka planda takibe alındı. Bir sorun olursa WhatsApp'tan seni dürteceğim balım!", host), nil
}

func (t *NetworkMonitoringTool) stopBackgroundMonitor(host string) (string, error) {
	monitorMu.Lock()
	defer monitorMu.Unlock()

	if task, exists := activeMonitors[host]; exists {
		close(task.StopChan)
		delete(activeMonitors, host)
		return fmt.Sprintf("✅ %s için izleme görevi durduruldu.", host), nil
	}
	return "⚠️ Bu host için aktif bir izleme bulunamadı.", nil
}
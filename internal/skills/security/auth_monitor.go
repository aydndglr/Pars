package security

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

type ThreatTracker struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	limit    int
	window   time.Duration
}

func newThreatTracker(limit int, window time.Duration) *ThreatTracker {
	return &ThreatTracker{
		attempts: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

func (t *ThreatTracker) AddAttempt(source string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	var validTimes []time.Time

	for _, tStamp := range t.attempts[source] {
		if now.Sub(tStamp) <= t.window {
			validTimes = append(validTimes, tStamp)
		}
	}

	validTimes = append(validTimes, now)
	t.attempts[source] = validTimes

	if len(validTimes) >= t.limit {
		delete(t.attempts, source)
		return true
	}

	return false
}

type AuthMonitor struct {
	EventChan    chan<- string
	ctx          context.Context
	cancel       context.CancelFunc
	bruteTracker *ThreatTracker
	lastUSBCount int
}

func NewAuthMonitor(eventChan chan<- string) *AuthMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	return &AuthMonitor{
		EventChan:    eventChan,
		ctx:          ctx,
		cancel:       cancel,
		bruteTracker: newThreatTracker(4, 3*time.Minute),
		lastUSBCount: -1,
	}
}

func (m *AuthMonitor) Start() {
	logger.Success("🛡️ [SECURITY] Siber İstihbarat (Auth & Threat) Motoru Aktif Edildi!")
	if runtime.GOOS == "windows" {
		go m.watchWindowsAuth()
	} else {
		go m.watchLinuxAuth()
	}
	go m.watchHardwareChanges()
}

func (m *AuthMonitor) Stop() {
	m.cancel()
	logger.Warn("🛡️ [SECURITY] Siber İstihbarat Kalkanları indirildi.")
}

func (m *AuthMonitor) sendAlert(msg string) {
	select {
	case m.EventChan <- msg:
	default:
		logger.Warn("⚠️ [SECURITY] EventChan dolu! Sızma alarmı sıraya alınamadı.")
	}
}

func (m *AuthMonitor) watchLinuxAuth() {
	cmd := exec.CommandContext(m.ctx, "journalctl", "-f", "-n", "0", "-q")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logger.Error("Linux Auth Monitor başlatılamadı: %v", err)
		return
	}

	if err := cmd.Start(); err != nil {
		return
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		lineLower := strings.ToLower(line)
		if strings.Contains(lineLower, "failed password") || strings.Contains(lineLower, "authentication failure") {
			source := m.extractLinuxSource(line) 
			if m.bruteTracker.AddAttempt(source) {
				msg := fmt.Sprintf("[SİBER GÜVENLİK BİLDİRİMİ] [CRITICAL]: 🚨 Brute-Force (Kaba Kuvvet) Saldırısı Tespit Edildi! Kaynak/Hedef: '%s'. Sistemde art arda başarısız şifre denemeleri var. Güvenlik duvarını (Firewall) kontrol etmemi ister misin?", source)
				m.sendAlert(msg)
			}
		}
		if strings.Contains(lineLower, "sudo:") && strings.Contains(lineLower, "incorrect password") {
			msg := fmt.Sprintf("[SİBER GÜVENLİK BİLDİRİMİ] [WARN]: ⚠️ İzinsiz Yetki Yükseltme Denemesi! Birisi 'sudo' (yönetici) yetkisi almaya çalıştı ancak şifreyi yanlış girdi. Log: %s", m.shortenLog(line))
			m.sendAlert(msg)
		}
	}
}

func (m *AuthMonitor) extractLinuxSource(logLine string) string {
	parts := strings.Split(logLine, " from ")
	if len(parts) > 1 {
		ipPart := strings.Split(parts[1], " ")[0]
		return ipPart
	}
	return "Bilinmeyen Kaynak"
}

func (m *AuthMonitor) watchWindowsAuth() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			psCmd := `Get-WinEvent -FilterHashtable @{LogName='Security'; ID=4625; StartTime=(Get-Date).AddSeconds(-15)} -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Message`
			cmd := exec.CommandContext(m.ctx, "powershell", "-NoProfile", "-Command", psCmd)
			out, _ := cmd.Output()
			
			outputStr := string(out)
			if strings.TrimSpace(outputStr) != "" {
				if m.bruteTracker.AddAttempt("Windows_Local_Auth") {
					msg := "[SİBER GÜVENLİK BİLDİRİMİ] [CRITICAL]: 🚨 Windows Kilit Ekranı / RDP Brute-Force Tespit Edildi! Birisi veya bir zararlı yazılım art arda bilgisayar şifresini deniyor."
					m.sendAlert(msg)
				}
			}

			psAdminCmd := `Get-WinEvent -FilterHashtable @{LogName='Security'; ID=4672; StartTime=(Get-Date).AddSeconds(-15)} -ErrorAction SilentlyContinue | Where-Object {$_.Message -match 'Administrator'} | Measure-Object | Select-Object -ExpandProperty Count`
			cmdAdmin := exec.CommandContext(m.ctx, "powershell", "-NoProfile", "-Command", psAdminCmd)
			outAdmin, _ := cmdAdmin.Output()
			if strings.TrimSpace(string(outAdmin)) != "0" && strings.TrimSpace(string(outAdmin)) != "" {
				m.sendAlert("[SİBER GÜVENLİK BİLDİRİMİ] [INFO]: ⚠️ Sistemde yeni bir 'Administrator' (Yönetici) oturumu açıldı veya bir yetki yükseltme (UAC) işlemi gerçekleşti. Eğer bu sen değilsen, acil müdahale et!")
			}
		}
	}
}

func (m *AuthMonitor) watchHardwareChanges() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			var currentCount int

			if runtime.GOOS == "windows" {
				cmd := exec.CommandContext(m.ctx, "wmic", "logicaldisk", "where", "drivetype=2", "get", "deviceid")
				out, err := cmd.Output()
				if err == nil {
					currentCount = strings.Count(string(out), ":") 
				}
			} else {
				cmd := exec.CommandContext(m.ctx, "bash", "-c", "lsblk -o TRAN | grep -i usb | wc -l")
				out, err := cmd.Output()
				if err == nil {
					fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &currentCount)
				}
			}

			if m.lastUSBCount == -1 {
				m.lastUSBCount = currentCount
				continue
			}

			if currentCount > m.lastUSBCount {
				msg := "[SİBER GÜVENLİK BİLDİRİMİ] [WARN]: 💾 DİKKAT! Sisteme yeni bir USB Bellek / Çıkarılabilir Donanım takıldı. Otomatik çalıştırmalar (Autorun) tehlikeli olabilir, içeriğini taramamı ister misin?"
				m.sendAlert(msg)
			}

			if currentCount < m.lastUSBCount {
				logger.Warn("🛡️ [SECURITY] Bir USB aygıtı sistemden çıkarıldı.")
			}

			m.lastUSBCount = currentCount
		}
	}
}

func (m *AuthMonitor) shortenLog(log string) string {
	if len(log) > 150 {
		return log[:147] + "..."
	}
	return log
}
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

// =====================================================================
// 🛡️ THREAT TRACKER (Kayan Zaman Pencereli Siber Tehdit Avcısı)
// =====================================================================
// Sadece hataları saymaz, "Son X dakika içindeki" hataları sayarak False-Positive'leri engeller.
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

// AddAttempt, kaynağın (IP/Kullanıcı) başarısız denemesini kaydeder ve eşik aşıldıysa true döner.
func (t *ThreatTracker) AddAttempt(source string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	var validTimes []time.Time

	// Süresi geçmiş (window dışı) eski kayıtları temizle (Memory Leak engellemesi)
	for _, tStamp := range t.attempts[source] {
		if now.Sub(tStamp) <= t.window {
			validTimes = append(validTimes, tStamp)
		}
	}

	validTimes = append(validTimes, now)
	t.attempts[source] = validTimes

	// Eşik değeri aşıldı mı?
	if len(validTimes) >= t.limit {
		// Alarm verildikten sonra bu kaynağın geçmişini sıfırla ki spama düşmesin
		delete(t.attempts, source)
		return true
	}

	return false
}

// =====================================================================
// 👁️ AUTH & THREAT MONITOR ENGINE
// =====================================================================
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
		// Aynı hedeften son 3 dakika içinde 4 hatalı giriş olursa Brute-Force sayılır!
		bruteTracker: newThreatTracker(4, 3*time.Minute),
		lastUSBCount: -1,
	}
}

func (m *AuthMonitor) Start() {
	logger.Success("🛡️ [SECURITY] Siber İstihbarat (Auth & Threat) Motoru Aktif Edildi!")

	// İşletim sistemine (OS) göre doğru kalkanları çalıştır
	if runtime.GOOS == "windows" {
		go m.watchWindowsAuth()
	} else {
		go m.watchLinuxAuth()
	}

	// USB ve Donanım izleyicisi OS bağımsız (farklı komutlarla) çalışır
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

// ---------------------------------------------------------------------
// 🐧 LİNUX SAVUNMA HATTI (Journalctl / SSH / Sudo)
// ---------------------------------------------------------------------
func (m *AuthMonitor) watchLinuxAuth() {
	// journalctl ile sistem loglarını anlık (tail -f mantığıyla) asenkron dinleriz
	// -n 0 (Sadece yeni loglar), -q (Sessiz mod)
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

		// 1. SSH Brute Force / Başarısız Şifre
		if strings.Contains(lineLower, "failed password") || strings.Contains(lineLower, "authentication failure") {
			source := m.extractLinuxSource(line) // IP veya Kullanıcı adını çeker
			if m.bruteTracker.AddAttempt(source) {
				msg := fmt.Sprintf("[SİBER GÜVENLİK BİLDİRİMİ] [CRITICAL]: 🚨 Brute-Force (Kaba Kuvvet) Saldırısı Tespit Edildi! Kaynak/Hedef: '%s'. Sistemde art arda başarısız şifre denemeleri var. Güvenlik duvarını (Firewall) kontrol etmemi ister misin?", source)
				m.sendAlert(msg)
			}
		}

		// 2. Privilege Escalation (İzinsiz Sudo / Kök Yetkisi Denemesi)
		if strings.Contains(lineLower, "sudo:") && strings.Contains(lineLower, "incorrect password") {
			msg := fmt.Sprintf("[SİBER GÜVENLİK BİLDİRİMİ] [WARN]: ⚠️ İzinsiz Yetki Yükseltme Denemesi! Birisi 'sudo' (yönetici) yetkisi almaya çalıştı ancak şifreyi yanlış girdi. Log: %s", m.shortenLog(line))
			m.sendAlert(msg)
		}
	}
}

func (m *AuthMonitor) extractLinuxSource(logLine string) string {
	// Basit bir parser: "Failed password for invalid user root from 192.168.1.5..."
	parts := strings.Split(logLine, " from ")
	if len(parts) > 1 {
		ipPart := strings.Split(parts[1], " ")[0]
		return ipPart
	}
	return "Bilinmeyen Kaynak"
}

// ---------------------------------------------------------------------
// 🪟 WINDOWS SAVUNMA HATTI (Event Viewer / 4625 / 4672)
// ---------------------------------------------------------------------
func (m *AuthMonitor) watchWindowsAuth() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	// Windows Event Loglarını PowerShell ile periyodik sorgularız
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			// Son 15 saniyedeki Başarısız Girişleri (4625) getirir
			psCmd := `Get-WinEvent -FilterHashtable @{LogName='Security'; ID=4625; StartTime=(Get-Date).AddSeconds(-15)} -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Message`
			cmd := exec.CommandContext(m.ctx, "powershell", "-NoProfile", "-Command", psCmd)
			out, _ := cmd.Output()
			
			outputStr := string(out)
			if strings.TrimSpace(outputStr) != "" {
				// Windows Event çıktısı uzundur, "Account Name" kısmını ayıklamaya çalışırız
				if m.bruteTracker.AddAttempt("Windows_Local_Auth") {
					msg := "[SİBER GÜVENLİK BİLDİRİMİ] [CRITICAL]: 🚨 Windows Kilit Ekranı / RDP Brute-Force Tespit Edildi! Birisi veya bir zararlı yazılım art arda bilgisayar şifresini deniyor."
					m.sendAlert(msg)
				}
			}

			// Olası Yetki Yükseltme: Yeni bir yönetici oturumu açılması (Event ID 4672) - Spama düşmemek için sıkı filtreli
			psAdminCmd := `Get-WinEvent -FilterHashtable @{LogName='Security'; ID=4672; StartTime=(Get-Date).AddSeconds(-15)} -ErrorAction SilentlyContinue | Where-Object {$_.Message -match 'Administrator'} | Measure-Object | Select-Object -ExpandProperty Count`
			cmdAdmin := exec.CommandContext(m.ctx, "powershell", "-NoProfile", "-Command", psAdminCmd)
			outAdmin, _ := cmdAdmin.Output()
			if strings.TrimSpace(string(outAdmin)) != "0" && strings.TrimSpace(string(outAdmin)) != "" {
				// Her 4672 virüs değildir (Sistem kendi de yapar), bu yüzden uyararak Pars'a inceleme izni veririz
				m.sendAlert("[SİBER GÜVENLİK BİLDİRİMİ] [INFO]: ⚠️ Sistemde yeni bir 'Administrator' (Yönetici) oturumu açıldı veya bir yetki yükseltme (UAC) işlemi gerçekleşti. Eğer bu sen değilsen, acil müdahale et!")
			}
		}
	}
}

// ---------------------------------------------------------------------
// 🔌 DONANIM VE USB İZLEYİCİSİ (Ayrık Sensör)
// ---------------------------------------------------------------------
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
				// Windows'ta aktif Removable Disk (USB) sayısını alır
				cmd := exec.CommandContext(m.ctx, "wmic", "logicaldisk", "where", "drivetype=2", "get", "deviceid")
				out, err := cmd.Output()
				if err == nil {
					currentCount = strings.Count(string(out), ":") // E: , F: gibi
				}
			} else {
				// Linux'ta usb disk bloklarını sayar
				cmd := exec.CommandContext(m.ctx, "bash", "-c", "lsblk -o TRAN | grep -i usb | wc -l")
				out, err := cmd.Output()
				if err == nil {
					fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &currentCount)
				}
			}

			// İlk açılışta mevcut sayıyı eşitle
			if m.lastUSBCount == -1 {
				m.lastUSBCount = currentCount
				continue
			}

			// USB Takıldı! (Kötü amaçlı yazılımlar USB ile yayılır - BadUSB)
			if currentCount > m.lastUSBCount {
				msg := "[SİBER GÜVENLİK BİLDİRİMİ] [WARN]: 💾 DİKKAT! Sisteme yeni bir USB Bellek / Çıkarılabilir Donanım takıldı. Otomatik çalıştırmalar (Autorun) tehlikeli olabilir, içeriğini taramamı ister misin?"
				m.sendAlert(msg)
			}
			// USB Çıkarıldı! (Veri hırsızlığı - Data Exfiltration sonrası)
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
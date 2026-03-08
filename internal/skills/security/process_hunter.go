package security

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/shirou/gopsutil/v3/process"
)

// =====================================================================
// 🛡️ PROCESS HUNTER (Davranışsal Süreç ve Kalıcılık Avcısı)
// =====================================================================
type ProcessHunter struct {
	EventChan       chan<- string
	ctx             context.Context
	cancel          context.CancelFunc
	alertedPIDs     map[int32]bool    // Aynı virüs için sürekli alarm vermemek (Spam Koruması)
	baselineStartup string            // Sistemin ilk açılışındaki "Temiz" başlangıç programları
	mu              sync.RWMutex
}

func NewProcessHunter(eventChan chan<- string) *ProcessHunter {
	ctx, cancel := context.WithCancel(context.Background())
	return &ProcessHunter{
		EventChan:   eventChan,
		ctx:         ctx,
		cancel:      cancel,
		alertedPIDs: make(map[int32]bool),
	}
}

func (p *ProcessHunter) Start() {
	logger.Success("🛡️ [SECURITY] Davranışsal Süreç Avcısı (Process Hunter) Aktif!")

	// 1. Sistem başlangıcındaki temiz durumu (Snapshot) hafızaya al
	p.takePersistenceSnapshot()

	// 2. Sensörleri (Asenkron) başlat
	go p.watchProcessAnomalies() // Parent-Child Anomalileri (Örn: Word -> PowerShell)
	go p.watchPersistence()      // Başlangıç/Kayıt Defteri (Registry/Cron) değişiklikleri
}

func (p *ProcessHunter) Stop() {
	p.cancel()
	logger.Warn("🛡️ [SECURITY] Süreç Avcısı kalkanları indirildi.")
}

func (p *ProcessHunter) sendAlert(msg string) {
	select {
	case p.EventChan <- msg:
	default:
		logger.Warn("⚠️ [SECURITY] EventChan dolu! Process alarmı sıraya alınamadı.")
	}
}

// ---------------------------------------------------------------------
// 🦠 1. DAVRANIŞSAL SÜREÇ (PROCESS TREE) ANALİZİ
// ---------------------------------------------------------------------
func (p *ProcessHunter) watchProcessAnomalies() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	// 🚨 Kırmızı Çizgi (Red Flags): Masum uygulamaların açmaması gereken tehlikeli uygulamalar
	// Örnek: "winword.exe" kalkıp da "powershell.exe" açamaz. Açarsa bu kesinlikle bir makro virüsüdür!
	suspiciousPairs := map[string][]string{
		"winword.exe": {"powershell.exe", "cmd.exe", "wscript.exe", "cscript.exe"},
		"excel.exe":   {"powershell.exe", "cmd.exe", "wscript.exe", "cscript.exe"},
		"msedge.exe":  {"cmd.exe", "powershell.exe"},
		"chrome.exe":  {"cmd.exe", "powershell.exe"},
		"acrobat.exe": {"cmd.exe", "powershell.exe", "bash", "sh"},
		"bash":        {"nc", "netcat", "curl", "wget"}, // Linux'ta bash içinden ters bağlantı (Reverse Shell) denemeleri
	}

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			procs, err := process.Processes()
			if err != nil {
				continue
			}

			for _, proc := range procs {
				// Daha önce uyardığımız bir virüs mü? (Spam koruması)
				p.mu.RLock()
				if p.alertedPIDs[proc.Pid] {
					p.mu.RUnlock()
					continue
				}
				p.mu.RUnlock()

				childName, err := proc.Name()
				if err != nil {
					continue
				}
				childName = strings.ToLower(childName)

				// Ebeveyn (Parent) sürecini bulalım
				parent, err := proc.Parent()
				if err != nil || parent == nil {
					continue
				}

				parentName, err := parent.Name()
				if err != nil {
					continue
				}
				parentName = strings.ToLower(parentName)

				// Bu Parent-Child ilişkisi bizim kara listemizde (suspiciousPairs) var mı?
				if targets, exists := suspiciousPairs[parentName]; exists {
					for _, target := range targets {
						if strings.Contains(childName, target) {
							// EŞLEŞME YAKALANDI! (Davranışsal Virüs Tespiti)
							msg := fmt.Sprintf("[SİBER GÜVENLİK BİLDİRİMİ] [CRITICAL]: ☠️ ŞÜPHELİ SÜREÇ (PROCESS) DAVRANIŞI! '%s' adlı masum uygulama, arka planda gizlice '%s' çalıştırmaya kalktı! Bu bir makro virüsü veya istismar (exploit) taktiğidir. PID: %d. İzin verirsen acımadan öldüreceğim (Kill)!", parentName, childName, proc.Pid)
							p.sendAlert(msg)

							// Sistemi spama boğmamak için bu PID'yi karantinaya al
							p.mu.Lock()
							p.alertedPIDs[proc.Pid] = true
							p.mu.Unlock()
						}
					}
				}
			}
		}
	}
}

// ---------------------------------------------------------------------
// ⚓ 2. KALICILIK (PERSISTENCE) AVCISI
// ---------------------------------------------------------------------
func (p *ProcessHunter) takePersistenceSnapshot() {
	p.baselineStartup = p.getStartupItems()
}

func (p *ProcessHunter) watchPersistence() {
	ticker := time.NewTicker(2 * time.Minute) // 2 dakikada bir "Yeni virüs eklendi mi?" diye bakar
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			currentStartup := p.getStartupItems()
			
			// Eğer başlangıçta olmayan yeni bir şeyler eklenmişse
			if currentStartup != p.baselineStartup && currentStartup != "" && p.baselineStartup != "" {
				msg := "[SİBER GÜVENLİK BİLDİRİMİ] [WARN]: ⚠️ SİSTEM BAŞLANGICINA YENİ BİR KAYIT EKLENDİ (Kalıcılık Algılandı)! Bir uygulama bilgisayar her açıldığında otomatik çalışmak için kendini kayıt defterine veya zamanlanmış görevlere ekledi. Lütfen başlangıç uygulamalarını kontrol et."
				p.sendAlert(msg)

				// Yeni durumu temiz kabul et (Sürekli aynı uyarıyı vermemek için)
				p.baselineStartup = currentStartup 
			}
		}
	}
}

// getStartupItems, işletim sistemine göre kritik başlangıç noktalarını (Run/Cron) metin olarak okur
func (p *ProcessHunter) getStartupItems() string {
	var out []byte

	if runtime.GOOS == "windows" {
		// Windows: Registry'deki "Run" klasörlerini okur (Zararlıların en çok sevdiği yer)
		psCmd := `Get-ItemProperty HKLM:\Software\Microsoft\Windows\CurrentVersion\Run, HKCU:\Software\Microsoft\Windows\CurrentVersion\Run -ErrorAction SilentlyContinue | Out-String`
		cmd := exec.CommandContext(p.ctx, "powershell", "-NoProfile", "-Command", psCmd)
		out, _ = cmd.Output()
	} else {
		// Linux: Kullanıcı Cron görevlerini okur
		cmd := exec.CommandContext(p.ctx, "crontab", "-l")
		out, _ = cmd.Output()
	}

	return strings.TrimSpace(string(out))
}
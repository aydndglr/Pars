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
	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

// =====================================================================
// 🌐 NETWORK & PRIVACY (Zehirli Ağ Radarı ve Mahremiyet Kalkanı)
// =====================================================================
type NetworkPrivacy struct {
	EventChan       chan<- string
	ctx             context.Context
	cancel          context.CancelFunc
	baselinePorts   map[uint32]bool
	alertedBadPorts map[uint32]bool
	mu              sync.RWMutex
}

func NewNetworkPrivacy(eventChan chan<- string) *NetworkPrivacy {
	ctx, cancel := context.WithCancel(context.Background())
	return &NetworkPrivacy{
		EventChan:       eventChan,
		ctx:             ctx,
		cancel:          cancel,
		baselinePorts:   make(map[uint32]bool),
		alertedBadPorts: make(map[uint32]bool),
	}
}

func (n *NetworkPrivacy) Start() {
	logger.Success("🛡️ [SECURITY] Zehirli Ağ Radarı ve Mahremiyet Kalkanı Aktif!")

	// Sistemin ilk anındaki açık (masum) portları hafızaya al
	n.takePortSnapshot()

	// Sensörleri (Asenkron) başlat
	go n.watchSuspiciousPorts()
	go n.watchHardwarePrivacy()
}

func (n *NetworkPrivacy) Stop() {
	n.cancel()
	logger.Warn("🛡️ [SECURITY] Ağ ve Mahremiyet Kalkanları indirildi.")
}

func (n *NetworkPrivacy) sendAlert(msg string) {
	select {
	case n.EventChan <- msg:
	default:
		logger.Warn("⚠️ [SECURITY] EventChan dolu! Ağ/Mahremiyet alarmı sıraya alınamadı.")
	}
}

// ---------------------------------------------------------------------
// 🕸️ 1. ZEHİRLİ AĞ VE PORT RADARI
// ---------------------------------------------------------------------
func (n *NetworkPrivacy) takePortSnapshot() {
	conns, err := net.Connections("tcp")
	if err == nil {
		for _, c := range conns {
			if c.Status == "LISTEN" {
				n.baselinePorts[c.Laddr.Port] = true
			}
		}
	}
}

func (n *NetworkPrivacy) watchSuspiciousPorts() {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	// Hackerların arka kapı (Backdoor) ve C2 sunucuları için sık kullandığı tehlikeli portlar
	badPorts := map[uint32]string{
		4444:  "Metasploit Default Listener",
		31337: "BackOrifice / Elite Hacker Port",
		6667:  "IRC Botnet Haberleşmesi",
		1337:  "Leech / Zararlı Yazılım Portu",
	}

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			conns, err := net.Connections("tcp")
			if err != nil {
				continue
			}

			for _, c := range conns {
				if c.Status != "LISTEN" && c.Status != "ESTABLISHED" {
					continue
				}

				port := c.Laddr.Port

				// 1. Durum: Bilinen Zararlı Port Açıldıysa
				if desc, isBad := badPorts[port]; isBad {
					n.mu.RLock()
					alreadyAlerted := n.alertedBadPorts[port]
					n.mu.RUnlock()

					if !alreadyAlerted {
						pid := c.Pid
						pName := n.getProcessName(pid)
						msg := fmt.Sprintf("[SİBER GÜVENLİK BİLDİRİMİ] [CRITICAL]: 🚨 ZEHİRLİ PORT AÇILDI! Sistemde '%s' portu (%d) açıldı. Süreç: '%s' (PID: %d). Bu bir Truva Atı (Trojan) veya Arka Kapı (Backdoor) bağlantısı olabilir! Bağlantıyı kesmemi ister misin?", desc, port, pName, pid)
						n.sendAlert(msg)

						n.mu.Lock()
						n.alertedBadPorts[port] = true
						n.mu.Unlock()
					}
				}

				// 2. Durum: Başlangıçta olmayan yepyeni bir "Dinleyen" (LISTEN) port açıldıysa (İsteğe bağlı gelişmiş analiz)
				// Not: Bunu çok sıkı tutarsak yeni açılan her uygulamada uyarır. Şimdilik sadece tehlikelilere odaklandık.
			}
		}
	}
}

func (n *NetworkPrivacy) getProcessName(pid int32) string {
	p, err := process.NewProcess(pid)
	if err != nil {
		return "Bilinmiyor"
	}
	name, err := p.Name()
	if err != nil {
		return "Bilinmiyor"
	}
	return name
}

// ---------------------------------------------------------------------
// 👁️ 2. MAHREMİYET KALKANI (Kamera & Mikrofon Gözcüsü)
// ---------------------------------------------------------------------
func (n *NetworkPrivacy) watchHardwarePrivacy() {
	ticker := time.NewTicker(5 * time.Second) // Mahremiyet çok hızlı denetlenmeli
	defer ticker.Stop()

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			if runtime.GOOS == "linux" {
				// Linux'ta kamera aygıtını (/dev/video0) kimin kullandığını kontrol eder
				cmd := exec.CommandContext(n.ctx, "fuser", "/dev/video0")
				out, err := cmd.Output()
				
				// Eğer fuser bir çıktı verirse (PID listesi), kamera MÜHÜRLENMİŞ (kullanımda) demektir!
				if err == nil && len(strings.TrimSpace(string(out))) > 0 {
					pids := strings.Fields(string(out))
					if len(pids) > 0 {
						msg := fmt.Sprintf("[SİBER GÜVENLİK BİLDİRİMİ] [WARN]: 👁️ DİKKAT! KAMERA AKTİF! Bir uygulama (PID: %s) şu an kameranı kullanıyor. Eğer görüntülü konuşmada değilsen gizliliğin ihlal ediliyor olabilir!", pids[0])
						n.sendAlert(msg)
						time.Sleep(10 * time.Minute) // Sürekli darlamamak için uyu
					}
				}
			} else if runtime.GOOS == "windows" {
				// Windows'ta kamera kullanımını Registry üzerinden takip ederiz
				// (Enterprise EDR sistemleri API Hooking yapar, bu en güvenli komut satırı alternatifidir)
				psCmd := `Get-ItemProperty -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore\webcam\NonPackaged" -ErrorAction SilentlyContinue | Select-Object -ExpandProperty LastUsedTimeStop`
				cmd := exec.CommandContext(n.ctx, "powershell", "-NoProfile", "-Command", psCmd)
				out, _ := cmd.Output()
				
				// Eğer çıktı "0" ise kamera şu an AKTİF kullanılıyor demektir.
				if strings.TrimSpace(string(out)) == "0" {
					n.sendAlert("[SİBER GÜVENLİK BİLDİRİMİ] [WARN]: 👁️ DİKKAT! KAMERA AKTİF! Arka planda bir Windows uygulaması şu an kameranı kullanıyor!")
					time.Sleep(10 * time.Minute)
				}
			}
		}
	}
}
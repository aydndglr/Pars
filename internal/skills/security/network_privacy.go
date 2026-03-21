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
	n.takePortSnapshot()
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

func (n *NetworkPrivacy) watchHardwarePrivacy() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			if runtime.GOOS == "linux" {
				cmd := exec.CommandContext(n.ctx, "fuser", "/dev/video0")
				out, err := cmd.Output()
				if err == nil && len(strings.TrimSpace(string(out))) > 0 {
					pids := strings.Fields(string(out))
					if len(pids) > 0 {
						msg := fmt.Sprintf("[SİBER GÜVENLİK BİLDİRİMİ] [WARN]: 👁️ DİKKAT! KAMERA AKTİF! Bir uygulama (PID: %s) şu an kameranı kullanıyor. Eğer görüntülü konuşmada değilsen gizliliğin ihlal ediliyor olabilir!", pids[0])
						n.sendAlert(msg)
						time.Sleep(10 * time.Minute) 
					}
				}
			} else if runtime.GOOS == "windows" {
				psCmd := `Get-ItemProperty -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore\webcam\NonPackaged" -ErrorAction SilentlyContinue | Select-Object -ExpandProperty LastUsedTimeStop`
				cmd := exec.CommandContext(n.ctx, "powershell", "-NoProfile", "-Command", psCmd)
				out, _ := cmd.Output()
				if strings.TrimSpace(string(out)) == "0" {
					n.sendAlert("[SİBER GÜVENLİK BİLDİRİMİ] [WARN]: 👁️ DİKKAT! KAMERA AKTİF! Arka planda bir Windows uygulaması şu an kameranı kullanıyor!")
					time.Sleep(10 * time.Minute)
				}
			}
		}
	}
}
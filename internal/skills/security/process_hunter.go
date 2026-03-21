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

type ProcessHunter struct {
	EventChan       chan<- string
	ctx             context.Context
	cancel          context.CancelFunc
	alertedPIDs     map[int32]bool   
	baselineStartup string        
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
	p.takePersistenceSnapshot()
	go p.watchProcessAnomalies()
	go p.watchPersistence()   
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

func (p *ProcessHunter) watchProcessAnomalies() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	suspiciousPairs := map[string][]string{
		"winword.exe": {"powershell.exe", "cmd.exe", "wscript.exe", "cscript.exe"},
		"excel.exe":   {"powershell.exe", "cmd.exe", "wscript.exe", "cscript.exe"},
		"msedge.exe":  {"cmd.exe", "powershell.exe"},
		"chrome.exe":  {"cmd.exe", "powershell.exe"},
		"acrobat.exe": {"cmd.exe", "powershell.exe", "bash", "sh"},
		"bash":        {"nc", "netcat", "curl", "wget"}, 
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
				parent, err := proc.Parent()
				if err != nil || parent == nil {
					continue
				}

				parentName, err := parent.Name()
				if err != nil {
					continue
				}
				parentName = strings.ToLower(parentName)
				if targets, exists := suspiciousPairs[parentName]; exists {
					for _, target := range targets {
						if strings.Contains(childName, target) {
							msg := fmt.Sprintf("[SİBER GÜVENLİK BİLDİRİMİ] [CRITICAL]: ☠️ ŞÜPHELİ SÜREÇ (PROCESS) DAVRANIŞI! '%s' adlı masum uygulama, arka planda gizlice '%s' çalıştırmaya kalktı! Bu bir makro virüsü veya istismar (exploit) taktiğidir. PID: %d. İzin verirsen acımadan öldüreceğim (Kill)!", parentName, childName, proc.Pid)
							p.sendAlert(msg)
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

func (p *ProcessHunter) takePersistenceSnapshot() {
	p.baselineStartup = p.getStartupItems()
}

func (p *ProcessHunter) watchPersistence() {
	ticker := time.NewTicker(2 * time.Minute) 
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			currentStartup := p.getStartupItems()
			if currentStartup != p.baselineStartup && currentStartup != "" && p.baselineStartup != "" {
				msg := "[SİBER GÜVENLİK BİLDİRİMİ] [WARN]: ⚠️ SİSTEM BAŞLANGICINA YENİ BİR KAYIT EKLENDİ (Kalıcılık Algılandı)! Bir uygulama bilgisayar her açıldığında otomatik çalışmak için kendini kayıt defterine veya zamanlanmış görevlere ekledi. Lütfen başlangıç uygulamalarını kontrol et."
				p.sendAlert(msg)
				p.baselineStartup = currentStartup 
			}
		}
	}
}

func (p *ProcessHunter) getStartupItems() string {
	var out []byte

	if runtime.GOOS == "windows" {
		psCmd := `Get-ItemProperty HKLM:\Software\Microsoft\Windows\CurrentVersion\Run, HKCU:\Software\Microsoft\Windows\CurrentVersion\Run -ErrorAction SilentlyContinue | Out-String`
		cmd := exec.CommandContext(p.ctx, "powershell", "-NoProfile", "-Command", psCmd)
		out, _ = cmd.Output()
	} else {
		cmd := exec.CommandContext(p.ctx, "crontab", "-l")
		out, _ = cmd.Output()
	}

	return strings.TrimSpace(string(out))
}
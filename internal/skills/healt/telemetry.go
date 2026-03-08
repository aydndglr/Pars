package healt

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
)

// =====================================================================
// 🛡️ ALERT TRACKER (Cooldown & Spam Koruması)
// =====================================================================
type alertTracker struct {
	lastSent map[string]time.Time
	mu       sync.Mutex
}

func newAlertTracker() *alertTracker {
	return &alertTracker{
		lastSent: make(map[string]time.Time),
	}
}

func (a *alertTracker) CanSend(key string, cooldown time.Duration) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	if last, ok := a.lastSent[key]; ok {
		if now.Sub(last) < cooldown {
			return false // Cooldown dolmadı, spama izin verme
		}
	}
	a.lastSent[key] = now
	return true
}

// =====================================================================
// ⚙️ TELEMETRY SERVICE (Ana Motor)
// =====================================================================
type TelemetryService struct {
	EventChan    chan<- string
	ctx          context.Context
	cancel       context.CancelFunc
	lastNetStat  bool
	serviceState map[string]bool
	alerts       *alertTracker

	// Snapshot için son durumları asenkron cache'leriz (Blocking engellemek için)
	mu        sync.RWMutex
	cachedCPU float64
	cachedRAM float64
}

func NewTelemetryService(eventChan chan<- string) *TelemetryService {
	ctx, cancel := context.WithCancel(context.Background())
	return &TelemetryService{
		EventChan:    eventChan,
		ctx:          ctx,
		cancel:       cancel,
		lastNetStat:  true,
		serviceState: make(map[string]bool),
		alerts:       newAlertTracker(),
	}
}

func (t *TelemetryService) Start() {
	logger.Success("🩺 [HEALT] Enterprise Grade (v3.0) Telemetri Motoru Başlatıldı!")
	go t.monitorRAM()
	go t.monitorNetwork()
	go t.monitorCPU()
	go t.monitorDisk()
	go t.monitorCriticalServices()
}

func (t *TelemetryService) Stop() {
	t.cancel()
	logger.Warn("🩺 [HEALT] Telemetri motoru güvenli bir şekilde durduruldu (Graceful Shutdown).")
}

// Güvenli Kanal Gönderimi (Kanal doluysa sistemi kilitlemez)
func (t *TelemetryService) sendEvent(msg string) {
	select {
	case t.EventChan <- msg:
		// Başarılı
	default:
		logger.Warn("⚠️ [HEALT] EventChan dolu! Goroutine kilidi önlendi. Atlanan mesaj: %s", msg)
	}
}

// ---------------------------------------------------------
// 🧠 1. RAM VE SWAP (BELLEK)
// ---------------------------------------------------------
func (t *TelemetryService) monitorRAM() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			v, err := mem.VirtualMemory()
			swap, errSwap := mem.SwapMemory()

			if err == nil {
				t.mu.Lock()
				t.cachedRAM = v.UsedPercent
				t.mu.Unlock()

				if v.UsedPercent > 90.0 && t.alerts.CanSend("ram_critical", 10*time.Minute) {
					msg := fmt.Sprintf("[SİSTEM İÇ BİLDİRİMİ] [CRITICAL]: 🚨 RAM kullanımı kritik seviyede (%%%.2f). Cihaz kilitli mi: %v.", v.UsedPercent, IsScreenLocked())
					t.sendEvent(msg)
				}
			}

			if errSwap == nil && swap.UsedPercent > 80.0 && t.alerts.CanSend("swap_warn", 15*time.Minute) {
				msg := fmt.Sprintf("[SİSTEM İÇ BİLDİRİMİ] [WARN]: ⚠️ Swap (Takas) alanı kullanımı çok yüksek (%%%.2f).", swap.UsedPercent)
				t.sendEvent(msg)
			}
		}
	}
}

// ---------------------------------------------------------
// 🌐 2. AĞ VE İNTERNET (TCP TABANLI)
// ---------------------------------------------------------
func (t *TelemetryService) monitorNetwork() {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			// SSL/HTTP overhead olmadan saf TCP bağlantı kontrolü
			conn, err := net.DialTimeout("tcp", "1.1.1.1:53", 3*time.Second)
			currentNetStat := err == nil
			if currentNetStat {
				conn.Close()
			}

			if !t.lastNetStat && currentNetStat {
				msg := fmt.Sprintf("[SİSTEM İÇ BİLDİRİMİ] [INFO]: 🌐 İnternet bağlantısı koptuktan sonra tekrar geldi. Cihaz kilitli mi: %v.", IsScreenLocked())
				t.sendEvent(msg)
			} else if t.lastNetStat && !currentNetStat {
				logger.Warn("🩺 [HEALT] İnternet bağlantısı koptu! Pars TCP Socket takibinde...")
			}

			t.lastNetStat = currentNetStat
		}
	}
}

// ---------------------------------------------------------
// ⚡ 3. CPU (SUSTAINED LOAD)
// ---------------------------------------------------------
func (t *TelemetryService) monitorCPU() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	highLoadStrikes := 0

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			percentages, err := cpu.Percent(3*time.Second, false)
			if err == nil && len(percentages) > 0 {
				avgCPU := percentages[0]

				t.mu.Lock()
				t.cachedCPU = avgCPU
				t.mu.Unlock()

				if avgCPU > 90.0 {
					highLoadStrikes++
				} else {
					highLoadStrikes = 0
				}

				if highLoadStrikes >= 4 && t.alerts.CanSend("cpu_critical", 15*time.Minute) {
					culprit := t.findTopCPUProcess()
					msg := fmt.Sprintf("[SİSTEM İÇ BİLDİRİMİ] [CRITICAL]: 🔥 Sürekli Yüksek CPU Tüketimi (%%%.2f). Sisteme eziyet eden süreç: '%s'.", avgCPU, culprit)
					t.sendEvent(msg)
					highLoadStrikes = 0
				}
			}
		}
	}
}

func (t *TelemetryService) findTopCPUProcess() string {
	procs, err := process.Processes()
	if err != nil {
		return "Bilinmiyor"
	}

	var topProcess string
	var maxCPU float64 = 0.0

	for _, p := range procs {
		// gopsutil v3'te Percent(0) non-blocking delta okuması yapar
		c, err := p.Percent(0)
		if err == nil && c > maxCPU {
			maxCPU = c
			name, _ := p.Name()
			topProcess = fmt.Sprintf("%s (PID: %d, CPU: %%%.1f)", name, p.Pid, c)
		}
	}
	return topProcess
}

// ---------------------------------------------------------
// 💽 4. DISK VE DEPOLAMA
// ---------------------------------------------------------
func (t *TelemetryService) monitorDisk() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			d, err := disk.Usage(".")
			if err == nil && d.UsedPercent > 95.0 && t.alerts.CanSend("disk_warn", 60*time.Minute) {
				freeSpaceGB := float64(d.Free) / (1024 * 1024 * 1024)
				msg := fmt.Sprintf("[SİSTEM İÇ BİLDİRİMİ] [WARN]: 💽 Disk dolmak üzere. Kullanım: %%%.2f. Kalan boş alan: %.2f GB.", d.UsedPercent, freeSpaceGB)
				t.sendEvent(msg)
			}
		}
	}
}

// ---------------------------------------------------------
// 🛠️ 5. STATEFUL SERVİS İZLEYİCİSİ (Sub-String Uyumlu)
// ---------------------------------------------------------
func (t *TelemetryService) monitorCriticalServices() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	targetServices := []string{"ollama", "docker", "nginx"}

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			procs, err := process.Processes()
			if err != nil {
				continue
			}

			runningMap := make(map[string]bool)
			for _, p := range procs {
				name, err := p.Name()
				if err == nil {
					runningMap[strings.ToLower(name)] = true
				}
			}

			for _, target := range targetServices {
				isRunning := false
				// Sub-string eşleşmesi (Linux'ta 'dockerd' vs 'docker' uyumu için)
				for runningName := range runningMap {
					if strings.Contains(runningName, target) {
						isRunning = true
						break
					}
				}

				if prevState, exists := t.serviceState[target]; exists {
					if prevState && !isRunning && t.alerts.CanSend("svc_down_"+target, 5*time.Minute) {
						msg := fmt.Sprintf("[SİSTEM İÇ BİLDİRİMİ] [CRITICAL]: 🛑 KRİTİK SERVİS ÇÖKTÜ! '%s' isimli servis aniden durdu.", target)
						t.sendEvent(msg)
					} else if !prevState && isRunning {
						msg := fmt.Sprintf("[SİSTEM İÇ BİLDİRİMİ] [INFO]: ✅ Servis Geri Geldi: '%s' isimli servis tekrar çalışmaya başladı.", target)
						t.sendEvent(msg)
						// Servis geri geldiğinde spam kilidini hemen açıyoruz
						t.alerts.mu.Lock()
						delete(t.alerts.lastSent, "svc_down_"+target)
						t.alerts.mu.Unlock()
					}
				}
				t.serviceState[target] = isRunning
			}
		}
	}
}

// ---------------------------------------------------------
// 📊 6. SNAPSHOT (Bloke Etmeyen Hızlı Rapor)
// ---------------------------------------------------------
type SystemHealthReport struct {
	CPUUsage   float64
	RAMUsage   float64
	DiskUsage  float64
	IsOnline   bool
	ReportTime time.Time
}

func (t *TelemetryService) GetSnapshot() SystemHealthReport {
	t.mu.RLock()
	cpuU := t.cachedCPU
	ramU := t.cachedRAM
	t.mu.RUnlock()

	diskInfo, _ := disk.Usage(".")
	dU := 0.0
	if diskInfo != nil {
		dU = diskInfo.UsedPercent
	}

	return SystemHealthReport{
		CPUUsage:   cpuU,
		RAMUsage:   ramU,
		DiskUsage:  dU,
		IsOnline:   t.lastNetStat,
		ReportTime: time.Now(),
	}
}
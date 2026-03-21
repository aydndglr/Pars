package kangal

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/config"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/shirou/gopsutil/v3/process"
)

type ErrorPattern struct {
	ID           string  
	Name         string  
	Keywords     []string 
	Regex        *regexp.Regexp
	Severity     string   
	Category     string   
	Suggestion   string   
	AutoFixCmd   string   
	Enabled      bool    
}

type ErrorDetector struct {
	Config        *config.KangalConfig
	EventChan     chan<- string
	ctx           context.Context
	cancel        context.CancelFunc
	isRunning     bool
	mu            sync.RWMutex
	patterns      []ErrorPattern
	patternMu     sync.RWMutex
	lastErrors    []DetectedError
	lastErrorMu   sync.RWMutex
	alertTracker  map[string]time.Time 
	alertMu       sync.RWMutex
	stats         ErrorStats
	statsMu       sync.RWMutex
}

type DetectedError struct {
	ID          string
	PatternID   string
	Message     string
	Source      string
	Severity    string
	Timestamp   time.Time
	Context     map[string]interface{}
}

type ErrorStats struct {
	TotalDetected    int
	CriticalCount    int
	WarningCount     int
	LastHourCount    int
	TopCategories    map[string]int
}


func NewErrorDetector(ctx context.Context, cfg *config.KangalConfig, eventChan chan<- string) *ErrorDetector {
	if cfg == nil {
		logger.Error("❌ [ErrorDetector] Config nil! ErrorDetector oluşturulamadı.")
		return nil
	}
	
	if eventChan == nil {
		logger.Error("❌ [ErrorDetector] EventChan nil! ErrorDetector oluşturulamadı.")
		return nil
	}
	
	logger.Info("🔍 [ErrorDetector] Hata tespit motoru yapılandırılıyor...")
	
	detector := &ErrorDetector{
		Config:       cfg,
		EventChan:    eventChan,
		ctx:          ctx,
		isRunning:    false,
		patterns:     []ErrorPattern{},
		lastErrors:   []DetectedError{},
		alertTracker: make(map[string]time.Time),
		stats: ErrorStats{
			TopCategories: make(map[string]int),
		},
	}
	
	detector.initPatterns()
	
	return detector
}

func (d *ErrorDetector) initPatterns() {
	d.patterns = []ErrorPattern{
		{
			ID:       "dll_missing",
			Name:     "Eksik DLL Dosyası",
			Keywords: []string{"dll", "0xc000007b", "0xc0000135", "loadlibrary"},
			Regex:    regexp.MustCompile(`(?i)(dll|0xc000007[bB]|0xc0000135)`),
			Severity: "critical",
			Category: "dll",
			Suggestion: "DLL hatası tespit ettim! Büyük ihtimalle Visual C++ Redistributable veya .NET Framework eksik. Yüklememi ister misin?",
			AutoFixCmd: "winget install Microsoft.VCRedist.2015+.x64",
			Enabled:  true,
		},
		{
			ID:       "process_crash",
			Name:     "Uygulama Çökmesi",
			Keywords: []string{"crash", "stopped working", "application error", "faulting module"},
			Regex:    regexp.MustCompile(`(?i)(crash|stopped working|application error|faulting module)`),
			Severity: "critical",
			Category: "crash",
			Suggestion: "Bir uygulama çöktü! Hangi program olduğunu tespit ettim. Detaylı hata raporu ister misin?",
			AutoFixCmd: "",
			Enabled:  true,
		},
		{
			ID:       "timeout",
			Name:     "Zaman Aşımı",
			Keywords: []string{"timeout", "timed out", "connection timed out", "request timeout"},
			Regex:    regexp.MustCompile(`(?i)(timeout|timed out)`),
			Severity: "warning",
			Category: "timeout",
			Suggestion: "İşlem zaman aşımına uğradı. Ağ bağlantını kontrol edeyim mi veya timeout süresini artırayım mı?",
			AutoFixCmd: "",
			Enabled:  true,
		},
		{
			ID:       "permission",
			Name:     "Erişim İzni Hatası",
			Keywords: []string{"access denied", "permission denied", "unauthorized", "elevation required"},
			Regex:    regexp.MustCompile(`(?i)(access denied|permission denied|unauthorized)`),
			Severity: "warning",
			Category: "permission",
			Suggestion: "Erişim izni hatası! Yönetici yetkisi gerekebilir. Yönetici olarak çalıştırayım mı?",
			AutoFixCmd: "",
			Enabled:  true,
		},
		{
			ID:       "network",
			Name:     "Ağ Bağlantı Hatası",
			Keywords: []string{"connection refused", "network unreachable", "no internet", "dns"},
			Regex:    regexp.MustCompile(`(?i)(connection refused|network unreachable|no internet)`),
			Severity: "warning",
			Category: "network",
			Suggestion: "Ağ bağlantı sorunu tespit ettim. İnternet bağlantını kontrol edeyim mi?",
			AutoFixCmd: "",
			Enabled:  true,
		},
		{
			ID:       "python_error",
			Name:     "Python Hatası",
			Keywords: []string{"traceback", "python error", "importerror", "module not found"},
			Regex:    regexp.MustCompile(`(?i)(traceback|importerror|module not found)`),
			Severity: "warning",
			Category: "python",
			Suggestion: "Python kodunda hata var! Eksik kütüphane olabilir. Gereksinimleri yüklememi ister misin?",
			AutoFixCmd: "pip install -r requirements.txt",
			Enabled:  true,
		},
		{
			ID:       "go_error",
			Name:     "Go Derleme Hatası",
			Keywords: []string{"build failed", "compile error", "undefined:", "cannot find package"},
			Regex:    regexp.MustCompile(`(?i)(build failed|compile error|undefined:)`),
			Severity: "warning",
			Category: "go",
			Suggestion: "Go kodunda derleme hatası! Kodu inceleyip düzeltmemi ister misin?",
			AutoFixCmd: "",
			Enabled:  true,
		},
		{
			ID:       "disk_space",
			Name:     "Disk Dolu Hatası",
			Keywords: []string{"no space left", "disk full", "insufficient space"},
			Regex:    regexp.MustCompile(`(?i)(no space left|disk full)`),
			Severity: "critical",
			Category: "disk",
			Suggestion: "Disk alanı kritik seviyede! Temizlik yapmamı ister misin?",
			AutoFixCmd: "",
			Enabled:  true,
		},
		{
			ID:       "memory",
			Name:     "Bellek Hatası",
			Keywords: []string{"out of memory", "oom", "memory allocation failed", "heap"},
			Regex:    regexp.MustCompile(`(?i)(out of memory|oom|memory allocation)`),
			Severity: "critical",
			Category: "memory",
			Suggestion: "Bellek yetersiz! Hangi process RAM'i tüketiyor bulayım mı?",
			AutoFixCmd: "",
			Enabled:  true,
		},
		{
			ID:       "generic_error",
			Name:     "Genel Hata",
			Keywords: []string{"error:", "fatal:", "exception"},
			Regex:    regexp.MustCompile(`(?i)^(error:|fatal:|exception)`),
			Severity: "info",
			Category: "generic",
			Suggestion: "Bir hata tespit ettim. Detayları incelememi ister misin?",
			AutoFixCmd: "",
			Enabled:  true,
		},
	}
	
	logger.Success("✅ [ErrorDetector] %d hata pattern'i yüklendi", len(d.patterns))
}

func (d *ErrorDetector) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	
	if d.isRunning {
		logger.Warn("⚠️ [ErrorDetector] Zaten aktif, başlatma atlandı")
		return nil
	}
	
	if !d.Config.Enabled {
		logger.Debug("ℹ️ [ErrorDetector] Config'de disabled, başlatılmadı")
		return nil
	}
	
	d.ctx, d.cancel = context.WithCancel(context.Background())
	
	d.isRunning = true
	go d.monitorEventLog()
	go d.monitorProcessCrashes()
	
	logger.Success("✅ [ErrorDetector] Hata tespit motoru aktif!")
	return nil
}

func (d *ErrorDetector) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	
	if !d.isRunning {
		logger.Debug("ℹ️ [ErrorDetector] Zaten durmuş")
		return
	}
	
	logger.Info("🛑 [ErrorDetector] Hata tespit motoru durduruluyor...")
	
	if d.cancel != nil {
		d.cancel()
	}
	
	d.isRunning = false
	logger.Success("✅ [ErrorDetector] Hata tespit motoru durduruldu")
}

func (d *ErrorDetector) monitorEventLog() {
	ticker := time.NewTicker(30 * time.Second) 
	defer ticker.Stop()
	
	logger.Debug("🔍 [ErrorDetector] Event Log monitor başlatıldı")
	
	for {
		select {
		case <-d.ctx.Done():
			logger.Debug("🛑 [ErrorDetector] Event Log monitor durduruldu")
			return
		case <-ticker.C:
			d.checkWindowsEventLog()
		}
	}
}

func (d *ErrorDetector) checkWindowsEventLog() {
	cmd := exec.CommandContext(d.ctx, "powershell", "-NoProfile", "-Command",
		`Get-WinEvent -FilterHashtable @{LogName='Application'; Level=1,2,3; StartTime=(Get-Date).AddSeconds(-30)} -MaxEvents 5 -ErrorAction SilentlyContinue | Select-Object -Property TimeCreated,Message,LevelDisplayName,ProviderName | ConvertTo-Json`)
	
	output, err := cmd.Output()
	if err != nil {
		return
	}
	
	outputStr := string(output)
	
	d.patternMu.RLock()
	for _, pattern := range d.patterns {
		if !pattern.Enabled {
			continue
		}
		
		if d.matchPattern(outputStr, pattern) {
			d.handleDetectedError(DetectedError{
				ID:        fmt.Sprintf("evt_%d", time.Now().UnixNano()),
				PatternID: pattern.ID,
				Message:   outputStr,
				Source:    "eventlog",
				Severity:  pattern.Severity,
				Timestamp: time.Now(),
				Context:   map[string]interface{}{"source": "Windows Event Log"},
			})
		}
	}
	d.patternMu.RUnlock()
}

func (d *ErrorDetector) monitorProcessCrashes() {
	ticker := time.NewTicker(10 * time.Second) 
	defer ticker.Stop()
	trackedPIDs := make(map[int32]bool)
	
	logger.Debug("🔍 [ErrorDetector] Process Crash monitor başlatıldı")
	
	for {
		select {
		case <-d.ctx.Done():
			logger.Debug("🛑 [ErrorDetector] Process Crash monitor durduruldu")
			return
		case <-ticker.C:
			procs, err := process.Processes()
			if err != nil {
				continue
			}
			
			for _, p := range procs {
				if !trackedPIDs[p.Pid] {
					name, _ := p.Name()
					if d.isTrackedProcess(name) {
						trackedPIDs[p.Pid] = true
					}
				}
			}
			
			for pid := range trackedPIDs {
				exists, _ := process.PidExists(pid)
				if !exists {
					d.handleDetectedError(DetectedError{
						ID:        fmt.Sprintf("crash_%d_%d", pid, time.Now().UnixNano()),
						PatternID: "process_crash",
						Message:   fmt.Sprintf("Process %d kayıp/crashed", pid),
						Source:    "process",
						Severity:  "critical",
						Timestamp: time.Now(),
						Context:   map[string]interface{}{"pid": pid},
					})
					delete(trackedPIDs, pid)
				}
			}
		}
	}
}

func (d *ErrorDetector) matchPattern(text string, pattern ErrorPattern) bool {
	if pattern.Regex != nil && pattern.Regex.MatchString(text) {
		return true
	}
	
	textLower := strings.ToLower(text)
	for _, keyword := range pattern.Keywords {
		if strings.Contains(textLower, strings.ToLower(keyword)) {
			return true
		}
	}
	
	return false
}

func (d *ErrorDetector) isTrackedProcess(processName string) bool {
	if len(d.Config.TrackedApps) == 0 {
		return true 
	}
	
	for _, app := range d.Config.TrackedApps {
		if strings.EqualFold(processName, app) {
			return true
		}
	}
	
	return false
}

func (d *ErrorDetector) handleDetectedError(err DetectedError) {
	if !d.canAlert(err.PatternID) {
		logger.Debug("⏱️ [ErrorDetector] Rate limit nedeniyle alert atlandı: %s", err.PatternID)
		return
	}
	
	d.lastErrorMu.Lock()
	d.lastErrors = append(d.lastErrors, err)
	if len(d.lastErrors) > 100 {
		d.lastErrors = d.lastErrors[1:]
	}
	d.lastErrorMu.Unlock()
	d.updateStats(err)
	switch err.Severity {
	case "critical":
		logger.Error("🚨 [ErrorDetector] KRİTİK HATA: %s (%s)", err.PatternID, err.Source)
		d.sendCriticalAlert(err)
	case "warning":
		logger.Warn("⚠️ [ErrorDetector] UYARI: %s (%s)", err.PatternID, err.Source)
		d.sendWarningAlert(err)
	case "info":
		logger.Debug("ℹ️ [ErrorDetector] BİLGİ: %s (%s)", err.PatternID, err.Source)
		d.logInfo(err)
	}
}

func (d *ErrorDetector) canAlert(patternID string) bool {
	d.alertMu.Lock()
	defer d.alertMu.Unlock()
	cooldown := 5 * time.Minute
	
	if lastAlert, exists := d.alertTracker[patternID]; exists {
		if time.Since(lastAlert) < cooldown {
			return false
		}
	}
	
	d.alertTracker[patternID] = time.Now()
	return true
}

func (d *ErrorDetector) updateStats(err DetectedError) {
	d.statsMu.Lock()
	defer d.statsMu.Unlock()
	d.stats.TotalDetected++
	d.stats.LastHourCount++
	d.stats.TopCategories[err.PatternID]++
	if err.Severity == "critical" {
		d.stats.CriticalCount++
	} else if err.Severity == "warning" {
		d.stats.WarningCount++
	}
}

func (d *ErrorDetector) sendCriticalAlert(err DetectedError) {
	pattern := d.getPattern(err.PatternID)
	suggestion := "Hata tespit edildi. Detaylı analiz başlatıyorum..."
	if pattern != nil {
		suggestion = pattern.Suggestion
	}
	
	alertMsg := fmt.Sprintf("🚨 [KRİTİK HATA] %s\n\n%s\n\n⏰ Zaman: %s",
		pattern.Name,
		suggestion,
		err.Timestamp.Format("15:04:05"))
	
	select {
	case d.EventChan <- alertMsg:
		logger.Debug("📢 [ErrorDetector] Kritik alert gönderildi")
	default:
		logger.Warn("⚠️ [ErrorDetector] EventChan dolu, kritik alert atlandı")
	}
}

func (d *ErrorDetector) sendWarningAlert(err DetectedError) {
	pattern := d.getPattern(err.PatternID)
	suggestion := "Uyarı tespit edildi."
	if pattern != nil {
		suggestion = pattern.Suggestion
	}
	
	alertMsg := fmt.Sprintf("⚠️ [UYARI] %s\n\n%s",
		pattern.Name,
		suggestion)
	
	select {
	case d.EventChan <- alertMsg:
		logger.Debug("📢 [ErrorDetector] Uyarı alert'i gönderildi")
	default:
		logger.Warn("⚠️ [ErrorDetector] EventChan dolu, uyarı alert'i atlandı")
	}
}

func (d *ErrorDetector) logInfo(err DetectedError) {
	logger.Debug("📝 [ErrorDetector] Hata loglandı: %s", err.PatternID)
}

func (d *ErrorDetector) getPattern(patternID string) *ErrorPattern {
	d.patternMu.RLock()
	defer d.patternMu.RUnlock()
	
	for i := range d.patterns {
		if d.patterns[i].ID == patternID {
			return &d.patterns[i]
		}
	}
	
	return nil
}

func (d *ErrorDetector) IsRunning() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.isRunning
}

func (d *ErrorDetector) GetStatus() map[string]interface{} {
	d.mu.RLock()
	d.statsMu.RLock()
	d.lastErrorMu.RLock()
	defer d.mu.RUnlock()
	defer d.statsMu.RUnlock()
	defer d.lastErrorMu.RUnlock()
	
	lastErrorTime := "Hiç hata tespit edilmedi"
	if len(d.lastErrors) > 0 {
		lastErrorTime = d.lastErrors[len(d.lastErrors)-1].Timestamp.Format("15:04:05")
	}
	
	return map[string]interface{}{
		"is_running":        d.isRunning,
		"total_detected":    d.stats.TotalDetected,
		"critical_count":    d.stats.CriticalCount,
		"warning_count":     d.stats.WarningCount,
		"last_hour_count":   d.stats.LastHourCount,
		"last_error_time":   lastErrorTime,
		"pattern_count":     len(d.patterns),
		"enabled_patterns":  d.getEnabledPatternCount(),
	}
}

func (d *ErrorDetector) getEnabledPatternCount() int {
	count := 0
	for _, p := range d.patterns {
		if p.Enabled {
			count++
		}
	}
	return count
}

func (d *ErrorDetector) SetSensitivity(level string) {
	d.patternMu.Lock()
	defer d.patternMu.Unlock()
	
	switch level {
	case "low":
		for i := range d.patterns {
			d.patterns[i].Enabled = (d.patterns[i].Severity == "critical")
		}
	case "balanced":
		for i := range d.patterns {
			d.patterns[i].Enabled = (d.patterns[i].Severity == "critical" || d.patterns[i].Severity == "warning")
		}
	case "high":
		for i := range d.patterns {
			d.patterns[i].Enabled = true
		}
	default:
		logger.Warn("⚠️ [ErrorDetector] Geçersiz sensitivity level: %s", level)
		return
	}
	
	logger.Debug("🔧 [ErrorDetector] Sensitivity değiştirildi: %s", level)
}

func (d *ErrorDetector) ProcessTerminalError(data map[string]interface{}) {
	msg, ok := data["message"].(string)
	if !ok || msg == "" {
		return
	}
	
	d.patternMu.RLock()
	for _, pattern := range d.patterns {
		if !pattern.Enabled {
			continue
		}
		
		if d.matchPattern(msg, pattern) {
			d.handleDetectedError(DetectedError{
				ID:        fmt.Sprintf("term_%d", time.Now().UnixNano()),
				PatternID: pattern.ID,
				Message:   msg,
				Source:    "terminal",
				Severity:  pattern.Severity,
				Timestamp: time.Now(),
				Context:   data,
			})
			break 
		}
	}
	d.patternMu.RUnlock()
}

func (d *ErrorDetector) ProcessCrash(data map[string]interface{}) {
	pid, ok := data["pid"].(int32)
	if !ok {
		return
	}
	
	d.handleDetectedError(DetectedError{
		ID:        fmt.Sprintf("crash_%d_%d", pid, time.Now().UnixNano()),
		PatternID: "process_crash",
		Message:   fmt.Sprintf("Process %d crashed", pid),
		Source:    "process",
		Severity:  "critical",
		Timestamp: time.Now(),
		Context:   data,
	})
}

func (d *ErrorDetector) TriggerScan() {
	logger.Debug("🔍 [ErrorDetector] Manuel tarama tetiklendi")
	d.checkWindowsEventLog()
}

func (d *ErrorDetector) GetLastErrors(limit int) []DetectedError {
	d.lastErrorMu.RLock()
	defer d.lastErrorMu.RUnlock()
	
	if limit <= 0 || limit > len(d.lastErrors) {
		limit = len(d.lastErrors)
	}
	
	start := len(d.lastErrors) - limit
	if start < 0 {
		start = 0
	}
	
	return d.lastErrors[start:]
}

func (d *ErrorDetector) GetStats() ErrorStats {
	d.statsMu.RLock()
	defer d.statsMu.RUnlock()
	
	return d.stats
}

func (d *ErrorDetector) AddCustomPattern(pattern ErrorPattern) {
	d.patternMu.Lock()
	defer d.patternMu.Unlock()
	for _, p := range d.patterns {
		if p.ID == pattern.ID {
			logger.Warn("⚠️ [ErrorDetector] Pattern ID zaten var: %s", pattern.ID)
			return
		}
	}
	
	d.patterns = append(d.patterns, pattern)
	logger.Debug("➕ [ErrorDetector] Custom pattern eklendi: %s", pattern.ID)
}

func (d *ErrorDetector) RemovePattern(patternID string) {
	d.patternMu.Lock()
	defer d.patternMu.Unlock()
	
	for i, p := range d.patterns {
		if p.ID == patternID {
			d.patterns = append(d.patterns[:i], d.patterns[i+1:]...)
			logger.Debug("➖ [ErrorDetector] Pattern çıkarıldı: %s", patternID)
			return
		}
	}
}

func (d *ErrorDetector) EnablePattern(patternID string, enabled bool) {
	d.patternMu.Lock()
	defer d.patternMu.Unlock()
	
	for i := range d.patterns {
		if d.patterns[i].ID == patternID {
			d.patterns[i].Enabled = enabled
			logger.Debug("🔧 [ErrorDetector] Pattern %s enabled: %v", patternID, enabled)
			return
		}
	}
}
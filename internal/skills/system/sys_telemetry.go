package system

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
)

type SysTelemetryTool struct{}

func (t *SysTelemetryTool) Name() string { return "sys_telemetry" }

func (t *SysTelemetryTool) Description() string {
	return "Sistemin (Windows/Linux) CPU, RAM ve Disk durumunu okur. 'Threshold Awareness' ile kritik durumları ajana raporlar. Terminal kullanmaz, doğrudan kernel verisi çeker."
}

func (t *SysTelemetryTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"health", "find_process"},
				"description": "[ZORUNLU] 'health' tüm sistemin MR'ını çeker. 'find_process' ise belirli uygulamaları bulur.",
			},
			"query": map[string]interface{}{
				"type":        "string",
				"description": "[İSTEĞE BAĞLI] Sadece 'find_process' seçiliyse zorunludur. Aranan uygulamanın adı (Örn: 'omni', 'nginx').",
			},
		},
		"required": []string{"action"},
	}
}

type HealthResult struct {
	Status       string  `json:"status"`
	CPUUsagePct  float64 `json:"cpu_usage_percent"`
	CPUAlert     string  `json:"cpu_alert"`
	TotalRAM_GB  float64 `json:"total_ram_gb"`
	UsedRAM_GB   float64 `json:"used_ram_gb"`
	RAMUsagePct  float64 `json:"ram_usage_percent"`
	RAMAlert     string  `json:"ram_alert"`
	TotalDisk_GB float64 `json:"total_disk_gb"`
	UsedDisk_GB  float64 `json:"used_disk_gb"`
	DiskUsagePct float64 `json:"disk_usage_percent"`
	DiskAlert    string  `json:"disk_alert"`
}

type ProcessInfo struct {
	PID         int32   `json:"pid"`
	Name        string  `json:"name"`
	CPUUsagePct float64 `json:"cpu_usage_percent"`
	RAMUsageMB  float64 `json:"ram_usage_mb"`
	Status      string  `json:"status"` 
}

func evaluateAlert(usage float64) string {
	if usage >= 90.0 {
		return "CRITICAL 🚨" 
	} else if usage >= 80.0 {
		return "WARNING ⚠️"
	}
	return "NORMAL ✅"
}

func normalizeStatus(s string) string {
	s = strings.ToUpper(s)
	switch s {
	case "R": return "running"
	case "S": return "sleeping"
	case "D": return "disk_sleep"
	case "Z": return "zombie"
	case "T": return "stopped"
	case "I": return "idle"
	}
	return strings.ToLower(s)
}

func (t *SysTelemetryTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	action, _ := args["action"].(string)

	switch action {
	case "health":
		return getSystemHealth(ctx)
	case "find_process":
		query, ok := args["query"].(string)
		if !ok || query == "" {
			return `{"error": "find_process işlemi için 'query' parametresi zorunludur."}`, nil
		}
		return findProcess(ctx, query)
	default:
		return `{"error": "Geçersiz action değeri. 'health' veya 'find_process' kullanın."}`, nil
	}
}

func getSystemHealth(ctx context.Context) (string, error) {
	logger.Action("📊 Telemetri: Health Engine (Kernel) Okunuyor...")
	v, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return fmt.Sprintf(`{"error": "RAM okunamadı: %v"}`, err), nil
	}
	cpuPercents, err := cpu.PercentWithContext(ctx, time.Second, false)
	cpuUsage := 0.0
	if err == nil && len(cpuPercents) > 0 {
		cpuUsage = cpuPercents[0]
	}
	diskPath := "/"
	if runtime.GOOS == "windows" {
		diskPath = os.Getenv("SystemDrive") + "\\"
		if diskPath == "\\" { diskPath = "C:\\" }
	}
	d, err := disk.UsageWithContext(ctx, diskPath)
	diskUsage := 0.0
	var totalDisk, usedDisk uint64
	if err == nil {
		diskUsage = d.UsedPercent
		totalDisk = d.Total
		usedDisk = d.Used
	}

	result := HealthResult{
		Status:       "success",
		CPUUsagePct:  mathRound(cpuUsage),
		CPUAlert:     evaluateAlert(cpuUsage),
		TotalRAM_GB:  mathRound(float64(v.Total) / (1024 * 1024 * 1024)),
		UsedRAM_GB:   mathRound(float64(v.Used) / (1024 * 1024 * 1024)),
		RAMUsagePct:  mathRound(v.UsedPercent),
		RAMAlert:     evaluateAlert(v.UsedPercent),
		TotalDisk_GB: mathRound(float64(totalDisk) / (1024 * 1024 * 1024)),
		UsedDisk_GB:  mathRound(float64(usedDisk) / (1024 * 1024 * 1024)),
		DiskUsagePct: mathRound(diskUsage),
		DiskAlert:    evaluateAlert(diskUsage),
	}

	return formatTelemetryJSON(result), nil
}

func findProcess(ctx context.Context, query string) (string, error) {
	query = strings.ToLower(query)
	logger.Action("📊 Telemetri: Process Aranıyor -> %s", query)

	processes, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return fmt.Sprintf(`{"error": "İşlemler okunamadı: %v"}`, err), nil
	}

	var results []ProcessInfo

	for _, p := range processes {
		name, err := p.NameWithContext(ctx)
		if err != nil { continue }

		if strings.Contains(strings.ToLower(name), query) {
			cpuPct, _ := p.CPUPercentWithContext(ctx)
			memInfo, _ := p.MemoryInfoWithContext(ctx)
			statusList, _ := p.StatusWithContext(ctx)

			ramMB := 0.0
			if memInfo != nil {
				ramMB = float64(memInfo.RSS) / (1024 * 1024)
			}

			statusStr := "unknown"
			if len(statusList) > 0 {
				statusStr = normalizeStatus(statusList[0])
			}

			results = append(results, ProcessInfo{
				PID:         p.Pid,
				Name:        name,
				CPUUsagePct: mathRound(cpuPct),
				RAMUsageMB:  mathRound(ramMB),
				Status:      statusStr,
			})
		}
	}

	if len(results) == 0 {
		return fmt.Sprintf(`{"message": "'%s' adında çalışan bir process bulunamadı."}`, query), nil
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].RAMUsageMB > results[j].RAMUsageMB 
	})

	if len(results) > 20 {
		results = results[:20]
	}

	return formatTelemetryJSON(results), nil
}

func mathRound(val float64) float64 {
	return math.Round(val*100) / 100
}

func formatTelemetryJSON(data interface{}) string {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return `{"error": "JSON formatlama hatası"}`
	}
	return string(b)
}
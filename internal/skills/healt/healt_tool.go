package healt

import (
	"context"
	"encoding/json"
)

// HealthCheckTool, Pars'ın telemetri motorundaki GetSnapshot verilerini okumasını sağlar
type HealthCheckTool struct {
	TelemetrySvc *TelemetryService
}

func (t *HealthCheckTool) Name() string { return "check_system_health" }

func (t *HealthCheckTool) Description() string {
	return "Sistemin anlık sağlık durumunu (CPU, RAM, Disk, İnternet) milisaniyesinde okur ve raporlar. Kullanıcı bilgisayarın/sunucunun durumunu veya sağlığını sorduğunda KESİNLİKLE bu aracı kullan."
}

func (t *HealthCheckTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{}, // Parametreye gerek yok, dümdüz çalışır
	}
}

func (t *HealthCheckTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if t.TelemetrySvc == nil {
		return `{"error": "Telemetri servisi şu an aktif değil."}`, nil
	}
	
	snapshot := t.TelemetrySvc.GetSnapshot()
	
	// Çıktıyı LLM'in harika anlayacağı JSON formatına çeviriyoruz
	b, _ := json.MarshalIndent(snapshot, "", "  ")
	return string(b), nil
}
// internal/agent/planner.go
// 🚀 DÜZELTMELER: Nil checks, Error handling, Context timeout, Path validation
// ⚠️ DİKKAT: execution.go ve helpers.go ile %100 uyumlu

package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

// 🚨 YENİ: Timeout sabiti
const PlanGenerationTimeout = 30 * time.Second

// generatePlan: Pars'in bir göreve başlamadan önce yürüttüğü niyet okuma ve stratejik düşünme aşaması.
func (a *Pars) generatePlan(ctx context.Context, input string) (string, error) {
	// 🚨 DÜZELTME #1: Nil kontrolleri
	if a == nil {
		logger.Debug("⚠️ [Planner] Pars instance nil, plan atlandı")
		return "NO_PLAN_NEEDED", nil
	}

	if a.Brain == nil {
		logger.Error("❌ [Planner] Brain nil, plan oluşturulamadı")
		return "NO_PLAN_NEEDED", fmt.Errorf("brain uninitialized")
	}

	if a.Config == nil {
		logger.Debug("⚠️ [Planner] Config nil, varsayılan değerler kullanılıyor")
	}

	if a.Skills == nil {
		logger.Debug("⚠️ [Planner] Skills nil, boş tool listesi kullanılıyor")
	}

	// 🚨 DÜZELTME #2: Input validation
	if strings.TrimSpace(input) == "" {
		logger.Debug("⚠️ [Planner] Boş input, plan atlandı")
		return "NO_PLAN_NEEDED", nil
	}

	// 🚨 DÜZELTME #3: Context timeout ekle (Planlama çok uzun sürmesin)
	planCtx, cancel := context.WithTimeout(ctx, PlanGenerationTimeout)
	defer cancel()

	// İkincil beyin durumunu kontrol et
	secondaryStatus := "Devre dışı"
	// 🚨 DÜZELTME #4: Nested nil check
	if a.Config != nil && a.Config.Brain.Secondary.Enabled {
		secondaryStatus = fmt.Sprintf("AKTİF (Model: %s)", a.Config.Brain.Secondary.ModelName)
	}

	// Araçları bellek adresi yerine isimleriyle listele
	var toolListStr string
	if a.Skills != nil {
		tools := a.Skills.ListTools()
		var toolNames []string
		for _, t := range tools {
			if t != nil {
				toolNames = append(toolNames, t.Name())
			}
		}
		toolListStr = strings.Join(toolNames, ", ")
	}

	// 🚨 DÜZELTME #5: Prompt dosya yolunu work_dir'e göre çöz
	promptPath := "prompts/Planner.txt"
	if a.Config != nil && a.Config.App.WorkDir != "" {
		promptPath = filepath.Join(a.Config.App.WorkDir, "prompts", "Planner.txt")
	}

	// Promptu dışarıdaki dosyadan okuyoruz
	promptBytes, err := os.ReadFile(promptPath)
	if err != nil {
		logger.Warn("⚠️ [Planner] Prompt dosyası bulunamadı (%s). Acil durum sohbet moduna geçiliyor. Hata: %v", promptPath, err)
		// Dosya yoksa sistemi çökertmek yerine güvenli liman olan sohbete geçelim
		return "NO_PLAN_NEEDED", nil
	}

	// 🚨 DÜZELTME #6: Dosya boş mu kontrol et
	if len(promptBytes) == 0 {
		logger.Warn("⚠️ [Planner] Prompt dosyası boş (%s)", promptPath)
		return "NO_PLAN_NEEDED", nil
	}

	// Dosyadan okunan metni argümanlarla formatla
	plannerPrompt := fmt.Sprintf(string(promptBytes), input, toolListStr, secondaryStatus)

	history := []kernel.Message{
		{
			Role:    "system",
			Content: "Sen deterministik bir niyet analiz motorusun. Kullanıcı isteği tool gerektirmeyen basit sohbet veya tek adımlık bir soruysa SADECE 'NO_PLAN_NEEDED' döndür. Eğer kod, dosya, sistem işlemi veya tool kullanımı gerektiren görev varsa Markdown checklist plan üret. Asla ikisini birden üretme ve yorum yapma.",
		},
		{Role: "user", Content: plannerPrompt},
	}

	logger.Debug("🧠 [Planner] Plan oluşturuluyor... Input: %d karakter", len(input))

	resp, err := a.Brain.Chat(planCtx, history, nil)
	if err != nil {
		logger.Error("❌ [Planner] Brain chat hatası: %v", err)
		return "NO_PLAN_NEEDED", fmt.Errorf("plan oluşturma hatası: %w", err)
	}

	plan := strings.TrimSpace(resp.Content)

	// 🚨 DÜZELTME #7: Response validation
	if plan == "" {
		logger.Warn("⚠️ [Planner] Brain boş response döndürdü")
		return "NO_PLAN_NEEDED", nil
	}

	// 🚨 DÜZELTME #8: Plan geçerli mi kontrol et (NO_PLAN_NEEDED veya checklist)
	if strings.Contains(plan, "NO_PLAN_NEEDED") {
		logger.Debug("✅ [Planner] Sohbet modu tespit edildi, plan atlandı")
		return "NO_PLAN_NEEDED", nil
	}

	// 🚨 DÜZELTME #9: Plan Markdown checklist formatında mı kontrol et (basit validation)
	if !strings.Contains(plan, "- [ ]") && !strings.Contains(plan, "- [x]") && !strings.Contains(plan, "**") {
		logger.Debug("⚠️ [Planner] Plan formatı beklenen checklist formatında değil, sohbet moduna geçiliyor")
		return "NO_PLAN_NEEDED", nil
	}

	logger.Success("📝 [Planner] Harekât planı oluşturuldu: %d karakter", len(plan))
	return plan, nil
}
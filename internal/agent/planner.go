package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/config"
	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

var BasicGreetings = []string{
	"selam", "selamlar", "merhaba", "hello", "hi", "hey",
	"nasılsın", "nasıl gidiyor", "ne haber",
	"tamam", "ok", "teşekkürler", "thanks", "sağ ol",
	"kimsin", "adın ne", "ne yaparsın",
}

var SmallModelNames = []string{
	"qwen3:1.5b", "qwen3:4b", "qwen3.5:4b", "qwen2.5:3b", "qwen2.5:7b",
	"llama3:8b", "llama3.2:3b", "llama3.2:1b", "phi3:3.8b", "mistral:7b",
	"gemma2:2b", "gemma2:9b", "codellama:7b",
}

func isSmallModel(modelName string) bool {
	modelLower := strings.ToLower(modelName)
	for _, smallModel := range SmallModelNames {
		if strings.Contains(modelLower, strings.ToLower(smallModel)) {
			return true
		}
	}

	if strings.Contains(modelLower, ":4b") || strings.Contains(modelLower, ":3b") ||
		strings.Contains(modelLower, ":2b") || strings.Contains(modelLower, ":1.5b") {
		return true
	}
	if strings.Contains(modelLower, ":7b") || strings.Contains(modelLower, ":8b") {
		return true
	}
	return false
}

func isBasicGreeting(input string) bool {
	inputLower := strings.ToLower(strings.TrimSpace(input))
	for _, greeting := range BasicGreetings {
		if inputLower == greeting ||
			inputLower == greeting+"?" ||
			inputLower == greeting+"!" ||
			inputLower == greeting+"." {
			return true
		}
	}

	return false
}

func containsTaskKeywords(input string) bool {
	inputLower := strings.ToLower(input)

	taskKeywords := []string{
		"oku", "yaz", "sil", "listele", "ara", "kaydet", "indir", "yükley",
		"dosya", "klasör", "dizin", "path", "file", "folder",
		"kod", "code", "script", "python", "go", "java", "javascript",
		"düzenle", "test", "debug", "fix", "compile", "build",
		"çalıştır", "run", "execute", "terminal", "cmd", "shell",
		"sistem", "system", "install", "kur", "update", "güncelle",
		"ssh", "github", "git", "clone", "pull", "push",
		"ağ", "network", "ping", "scan", "port",
		"veritabanı", "database", "sql", "query", "sorgu",
		"görev", "task", "iş", "plan", "schedule", "zamanla",
		"whatsapp", "mesaj", "gönder", "resim", "fotoğraf",
		"fs_", "sys_", "db_", "ssh_", "whatsapp_", "browser_",
		"dev_studio", "edit_", "delete_", "create_", "update_", "list_",
		"oracle_index", "ask_oracle", "ask_codebase",
	}

	for _, keyword := range taskKeywords {
		if strings.Contains(inputLower, keyword) {
			return true
		}
	}

	return false
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (a *Pars) generatePlan(ctx context.Context, input string) (string, error) {
	logger.Debug("🚀 [Planner] generatePlan başlatıldı")

	if a == nil {
		return "NO_PLAN_NEEDED", nil
	}
	if a.Brain == nil {
		return "NO_PLAN_NEEDED", fmt.Errorf("brain uninitialized")
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return "NO_PLAN_NEEDED", nil
	}

	logger.Debug("📥 [Planner] Input alındı: %d karakter", len(input))

	if isBasicGreeting(input) && !containsTaskKeywords(input) {
		logger.Debug("✅ [Planner] Basit selamlaşma tespit edildi → Chat Mode")
		return "NO_PLAN_NEEDED", nil
	}

	modelName := ""
	if a.Config != nil {
		modelName = a.Config.Brain.Primary.ModelName
	}

	isSmall := isSmallModel(modelName)
	planCtx, cancel := context.WithTimeout(ctx, config.PlanGenerationTimeout)
	defer cancel()

	secondaryStatus := "Devre dışı"
	if a.Config != nil && a.Config.Brain.Secondary.Enabled {
		secondaryStatus = fmt.Sprintf("AKTİF (Model: %s)", a.Config.Brain.Secondary.ModelName)
	}

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

	plannerPrompt := fmt.Sprintf(`Kullanıcı Talebi: "%s"

Mevcut Araçlar: %s
İkincil Beyin Durumu: %s

Görev: Bu talebi yerine getirmek için kullanılacak araçları belirle. 
Eğer bu bir sohbet veya bilgi sorusu ise ve araç gerektirmiyorsa SADECE "NO_PLAN_NEEDED" yaz.
Eğer bir işlem veya araç kullanımı gerekiyorsa, adım adım bir checklist oluştur.`, input, toolListStr, secondaryStatus)
/*
	systemContent := `Sen Pars'ın niyet analiz motorusun. Görevin kullanıcının isteğini analiz edip DOĞRU ÇALIŞMA MODUNU seçmek.

🧠 KARAR KRİTERLERİ:

1️⃣ CHAT MODE (SADECE NO_PLAN_NEEDED döndür):
   - Selamlaşma, kişisel sorular
   - Tool gerektirmeyen her şey

2️⃣ TASK MODE (Markdown checklist plan üret):
   - Herhangi bir işlem, tool (araç) gerektiren istekler.

⚠️ KURALLAR:
- ASLA yorum yapma, direkt çıktı ver.
- Tool ismi geçiyorsa KESİNLİKLE TASK MODE seç.`

	if isSmall {
		systemContent = `Sen niyet analiz motorusun. SADECE iki seçeneğin var:
1. CHAT MODE → SADECE 'NO_PLAN_NEEDED' yaz
2. TASK MODE → Markdown checklist plan üret

⚠️ KURAL: Tool ismi veya eylem varsa TASK MODE üret. Asla açıklama yapma.`
	}
5️⃣6️⃣7️⃣8️⃣9️⃣0️⃣1️⃣2️⃣3️⃣
	
	*/
	systemContent := `Sen Pars'ın niyet analiz motorusun. Görevin kullanıcının isteğini analiz edip DOĞRU ÇALIŞMA MODUNU seçmek.

🧠 KARAR KRİTERLERİ:

1️⃣ CHAT MODE (SADECE NO_PLAN_NEEDED döndür):
   - Selamlaşma, kişisel sorular
   - Tool gerektirmeyen her şey

2️⃣ TASK MODE (Ultra-Minimal Plan Üret):
   - Herhangi bir işlem veya tool gerektiren istekler.

4️⃣ KULLANILABİLİR ARAÇLAR
   - Kullanılabilir araçlar için db/pars_tool.db içindeki bilgileri db_query aracını kullanarak oku
   - Araçların açıklamParametre sütunundaki bilgileri kullanarak araçlar için gerekli parametreleri oluştur.

⚠️ TASK MODE FORMAT KURALLARI (ÖLÜMCÜL ÖNEMDE):
- Plan MAKSİMUM 3-4 satır olmalıdır.
- SADECE bir markdown checklist oluştur.
- ASLA başlık (##), alt başlık (###), ayırıcı çizgi (---) veya paragraf KULLANMA.
- ASLA "Amaç", "Adım 1", "Sonuç" gibi kelimeler KULLANMA.
- SADECE şu formatı birebir uygula:
  - [ ] fs_list: Klasör yapısını tara.
  - [ ] oracle_index: Dosyaları hafızaya al.`

	if isSmall {
		systemContent = `Sen niyet analiz motorusun. SADECE iki seçeneğin var:
1. CHAT MODE → SADECE 'NO_PLAN_NEEDED' yaz
2. TASK MODE → SADECE ve SADECE kullanılacak araçların 2-3 satırlık checklist'ini yaz.

⚠️ TASK KURALI: Asla başlık (##), paragraf veya açıklama yazma. SADECE maddeler halinde araçları listele. Örnek:
- [ ] fs_list: Klasörü tara.
- [ ] oracle_index: İndeksle.`
	}

	history := []kernel.Message{
		{Role: "system", Content: systemContent},
		{Role: "user", Content: plannerPrompt},
	}

	resp, err := a.Brain.Chat(planCtx, history, nil)
	if err != nil {
		return "NO_PLAN_NEEDED", fmt.Errorf("plan oluşturma hatası: %w", err)
	}

	plan := strings.TrimSpace(resp.Content)
	if plan == "" {
		return "NO_PLAN_NEEDED", nil
	}

	planLower := strings.ToLower(plan)
	logger.Debug("🔍 [Planner] Plan lowercase: '%s'", planLower)

	// 🔥 DÜZELTME 1: KESİN OVERRIDE (ÜSTE ALINDI)
	// Eğer input içinde task keyword varsa (oracle_index vb.), 
	// modelin gevezelik edip "no_plan_needed" demesini KESİNLİKLE YOK SAY!
	if containsTaskKeywords(input) {
		logger.Debug("✅ [Planner] Task keyword var, model kararı bypass ediliyor → Task Mode")
		if len(plan) > 10000 {
			plan = plan[:5000]
		}
		return plan, nil
	}

	// Eğer üstteki "keyword" kuralına takılmadıysa, modelin chat mode kararına saygı duy
	if strings.Contains(planLower, "no_plan_needed") ||
		strings.Contains(planLower, "no plan needed") ||
		strings.Contains(planLower, "chat mode") ||
		strings.Contains(planLower, "sohbet modu") {
		logger.Debug("✅ [Planner] Model chat mode karar verdi, plan atlandı")
		return "NO_PLAN_NEEDED", nil
	}

	hasChecklist := strings.Contains(plan, "- [ ]") ||
		strings.Contains(plan, "- [x]") ||
		strings.Contains(plan, "**") ||
		strings.Contains(plan, "###") ||
		strings.Contains(planLower, "1.")

	if !hasChecklist {
		if isSmall {
			if len(plan) > 10000 {
				plan = plan[:5000]
			}
			return plan, nil
		}
		return "NO_PLAN_NEEDED", nil
	}

	if len(plan) > 10000 {
		plan = plan[:5000]
	}

	return plan, nil
}

func GetPlannerStats() map[string]interface{} {
	return map[string]interface{}{
		"basic_greetings_count": len(BasicGreetings),
		"small_models_count":    len(SmallModelNames),
		"decision_mechanism":    "model_based",
		"timestamp":             time.Now().Format("15:04:05"),
	}
}
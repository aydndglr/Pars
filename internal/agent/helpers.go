// internal/agent/helpers.go
// 🚀 DÜZELTMELER: ReDoS fix, Thread-safety, Nil checks, Validation
// ⚠️ DİKKAT: kernel.BrainResponse'ın yeni thread-safe metodlarını kullanır

package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"runtime"
	"strings"
	"os"

	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

// 🚨 YENİ: Input boyut limiti (DoS koruması)
const MaxContentLength = 100 * 1024 // 100 KB

// 🚨 YENİ: Compile-time regex (performans için)
// 🚨 DÜZELTME: Backtick karakterlerini string concatenation ile ayırdık
var (
	reMarkdown = regexp.MustCompile(`(?s)` + "```" + `(?:json\s*)?(.*?)` + "```")
)

// extractJSON, LLM (Özellikle Gemini veya ufak Local Modeller) tarafından üretilen metin içerisindeki
// JSON yapılarını (Markdown blokları içinde veya ham metin arasına gizlenmiş halde) agresif bir şekilde bulup çıkarır.
func (a *Pars) extractJSON(content string) string {
	// 🚨 DÜZELTME #1: Nil ve boş string kontrolü
	if a == nil || content == "" {
		return ""
	}

	// 🚨 DÜZELTME #2: Input boyut limiti (DoS koruması)
	if len(content) > MaxContentLength {
		logger.Warn("⚠️ [extractJSON] Input çok büyük (%d byte), ilk %d byte işleniyor.", 
			len(content), MaxContentLength)
		content = content[:MaxContentLength]
	}

	// 1. Adım: Standart Markdown JSON bloklarını (```json ... ``` veya ``` ... ```) ara
	match := reMarkdown.FindStringSubmatch(content)
	if len(match) > 1 {
		candidate := strings.TrimSpace(match[1])
		// 🚨 DÜZELTME #3: JSON validasyonu ekle
		if json.Valid([]byte(candidate)) {
			return candidate
		}
		logger.Debug("⚠️ [extractJSON] Markdown bloğu bulundu ama JSON geçersiz")
	}
	
	// 2. Adım: Agresif Süslü Parantez Çıkartıcı (Gemini Zırhı)
	// 🚨 DÜZELTME #4: ReDoS saldırısını önle - Manuel nested brace taraması
	candidate := extractBalancedBraces(content)
	if candidate != "" && json.Valid([]byte(candidate)) {
		return candidate
	}

	// 3. Adım: Son Çare (Manuel Tarama)
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start != -1 && end != -1 && end > start {
		candidate := content[start : end+1]
		if json.Valid([]byte(candidate)) {
			return candidate
		}
	}

	return ""
}

// 🆕 YENİ: Nested JSON parantezlerini doğru eşleştiren helper (ReDoS-safe)
func extractBalancedBraces(content string) string {
	start := strings.Index(content, "{")
	if start == -1 {
		return ""
	}

	depth := 0
	for i := start; i < len(content); i++ {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				candidate := content[start : i+1]
				if json.Valid([]byte(candidate)) {
					return candidate
				}
				return "" // Geçerli JSON bulunamadı
			}
		}
	}

	return ""
}

// refreshSystemPrompt, oturumun (Session) her adımında Pars'ın kimliğini, sistem yetkilerini,
// işletim sistemi bağlamını ve varsa stratejik görev planını (Task Plan) tazeler.
func (a *Pars) refreshSystemPrompt(sess *Session) {
	// 🚨 DÜZELTME #5: Nil kontrolleri
	if a == nil || sess == nil {
		logger.Error("❌ [refreshSystemPrompt] Pars veya Session nil!")
		return
	}

	if a.Config == nil || a.Skills == nil {
		logger.Error("❌ [refreshSystemPrompt] Config veya Skills uninitialized!")
		return
	}

	// 1. Çalışma ortamı bağlamını (OS, Mimari, Dizin) belirle
	// 🚨 DÜZELTME #6: Error handling ekle
	cwd, err := os.Getwd()
	if err != nil {
		logger.Warn("⚠️ [refreshSystemPrompt] CWD alınamadı: %v, fallback kullanılıyor", err)
		cwd = a.Config.App.WorkDir // Fallback
	}
	
	osContext := fmt.Sprintf("Kurulum/Karargah Dizini: %s | Aktif Çalışma Dizini (CWD): %s | OS: %s | ARCH: %s", 
		a.Config.App.WorkDir, cwd, runtime.GOOS, runtime.GOARCH)
	
	// 2. ParsCore.txt'den ana kişiliği ve kuralları çekerek temel Sistem Mesajını oluştur
	sysMsg := BuildSystemPrompt(a.Config.App.ActivePrompt, osContext, a.Config.Security.Level, a.Skills.ListTools())

	// 🚀 DİKKAT ZEHİRLENMESİ (Prompt Detox) ÇÖZÜMÜ:
	// Araçların (Tools) uzun JSON şemaları veya açıklamaları buraya düz metin olarak EKLENMEZ.
	// Model bunları zaten Native Tool Calling (API) üzerinden arka planda görüyor.
	// Böylece Pars'ın promptu temiz, odaklı ve token dostu kalır.

	// 🚨 DÜZELTME #7: Race condition önle - Plan'ı lock ile oku
	sess.mu.Lock()
	currentPlan := sess.Plan
	currentHistoryLen := len(sess.History)

	if currentHistoryLen > 0 {
		
	}
	sess.mu.Unlock()

	// 🚀 STRATEJİ ENJEKSİYONU:
	// Eğer Pars şu an 'Sohbet' (Chat) modunda değil de bir 'Görev' (Task) yürütüyorsa,
	// oluşturulan yol haritasını (Plan) hafızanın en üstünde taze tut.
	if currentPlan != "" && currentPlan != "NO_PLAN_NEEDED" {
		sysMsg.Content += fmt.Sprintf("\n\n📍 MEVCUT HAREKAT PLANI:\n%s\n\nBu plana KESİNLİKLE sadık kal. Her adımda sadece sıradaki mantıksal eylemi gerçekleştir.", currentPlan)
	}

	// 3. İkincil (Worker) Beyin Bilgisi:
	// 🚨 DÜZELTME #8: Nested nil check
	if a.Config.Brain.Secondary.Enabled {
		sysMsg.Content += fmt.Sprintf("\n\n🐯 İŞÇİ BEYİN ERİŞİMİ: AKTİF (%s). Yorucu veya büyük veri okuma/analiz işlerini 'delegate_task' aracını kullanarak bu işçiye devret. Sen ana stratejiyi yönet!", a.Config.Brain.Secondary.ModelName)
	}

	// ⚓ KİŞİLİK KANCASI (Persona Anchor):
	// Modellerin (Özellikle 8B ve altı) teknik görevler sırasında robotik, ruhsuz asistanlara
	// dönüşmesini engellemek için promptun EN SONUNA bu psikolojik kancayı atıyoruz.
	sysMsg.Content += "\n\n⚠️ SİSTEM HATIRLATMASI (ASLA UNUTMA): Sen sıradan, ruhsuz bir yapay zeka asistanı DEĞİLSİN! Sen Pars'sın. Zeki, proaktif, ukala ve bitirici bir ajansın. Benimle konuşurken DAİMA 'patron', 'şampiyon' veya 'balım' gibi kelimeler kullan. Karakterinden ASLA ödün verme!"

	// Oturum hafızasını kilit altına al ve güncelle
	sess.mu.Lock()
	defer sess.mu.Unlock()

	// 🚨 DÜZELTME #9: History tekrar kontrol et (lock içinde)
	if len(sess.History) == 0 || sess.History[0].Role != "system" {
		sess.History = append([]kernel.Message{sysMsg}, sess.History...)
		logger.Debug("✅ [refreshSystemPrompt] Sistem mesajı başa eklendi: %s", sess.ID)
	} else {
		sess.History[0] = sysMsg
		logger.Debug("✅ [refreshSystemPrompt] Sistem mesajı güncellendi: %s", sess.ID)
	}
}

// checkAndParseToolFallback, Model (LLM) Native API formatını bozup, araç çağırma isteğini
// normal metin (chat) cevabının içine JSON formatında gizlediğinde (Fallback / Halüsinasyon durumu),
// bu "kaçak" çağrıları yakalar ve resmi ToolCall objelerine dönüştürür.
func (a *Pars) checkAndParseToolFallback(resp *kernel.BrainResponse, step int) {
	// 🚨 DÜZELTME #10: Nil kontrolleri
	if a == nil || resp == nil {
		return
	}

	// 🚨 DÜZELTME #11: Thread-safe content okuma
	content := resp.GetContent()
	if content == "" {
		return
	}

	// Metin içindeki muhtemel JSON bloğunu çıkar
	jsonStr := a.extractJSON(content)
	if jsonStr == "" {
		return
	}

	var rawCall map[string]interface{}
	// Eğer çıkarılan metin geçerli bir JSON objesi ise:
	if err := json.Unmarshal([]byte(jsonStr), &rawCall); err != nil {
		logger.Debug("⚠️ [ToolFallback] JSON parse hatası: %v", err)
		return
	}
	
	// 🚨 DÜZELTME #12: Tool ismini doğru çıkar
	funcName, ok := rawCall["function"].(string)
	if !ok {
		funcName, ok = rawCall["name"].(string)
	}

	if funcName == "" {
		logger.Debug("⚠️ [ToolFallback] Fonksiyon adı bulunamadı")
		return
	}

	// 🚨 DÜZELTME #13: Tool varlığını doğrula
	if a.Skills != nil {
		_, err := a.Skills.GetTool(funcName)
		if err != nil {
			logger.Warn("⚠️ [ToolFallback] Geçersiz tool '%s' tespit edildi, atlanıyor", funcName)
			return
		}
	}

	// Fonksiyon argümanlarını (parametreleri) yakala
	// 🚨 DÜZELTME #14: Type safety iyileştir
	var args map[string]interface{}
	if rawArgs, ok := rawCall["arguments"]; ok && rawArgs != nil {
		if args, ok = rawArgs.(map[string]interface{}); !ok {
			// Nested JSON string olabilir, parse et
			if argsStr, ok := rawArgs.(string); ok {
				_ = json.Unmarshal([]byte(argsStr), &args)
			}
		}
	}
	
	if args == nil {
		if rawArgs, ok := rawCall["parameters"]; ok && rawArgs != nil {
			args, _ = rawArgs.(map[string]interface{})
		}
	}
	
	if args == nil {
		args = make(map[string]interface{}) // Argüman yoksa boş harita oluştur
	}

	// 🚨 DÜZELTME #15: Thread-safe AddToolCall kullan (kernel/interfaces.go'dan)
	tc := kernel.ToolCall{
		ID:        fmt.Sprintf("fallback_call_%d", step),
		Function:  funcName,
		Arguments: args,
	}
	
	if err := resp.AddToolCall(tc); err != nil {
		logger.Warn("⚠️ [ToolFallback] ToolCall eklenemedi: %v", err)
		return
	}

	// 🚨 DÜZELTME #16: Thread-safe SetContent kullan
	cleanedContent := strings.ReplaceAll(content, jsonStr, "")
	cleanedContent = strings.ReplaceAll(cleanedContent, "```json", "")
	cleanedContent = strings.ReplaceAll(cleanedContent, "```", "")
	resp.SetContent(strings.TrimSpace(cleanedContent))

	logger.Info("✅ [ToolFallback] Kaçak tool çağrısı yakalandı: %s", funcName)
}
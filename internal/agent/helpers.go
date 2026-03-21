package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

const MaxContentLength = 100 * 1024

var toolCallKeys = []string{
	"function",
	"name",
	"tool",
	"tool_name",
	"action",
	"tool_call",
	"function_name",
}

var (
	reMarkdown = regexp.MustCompile(`(?s)` + "```" + `(?:json\s*)?(.*?)` + "```")
	reToolCall = regexp.MustCompile(`(?i)(tool|function|action)[_\s]*name[\s]*[:=]`)
)

func (a *Pars) extractJSON(content string) string {
	logger.Debug("🔍 [extractJSON] Başlatıldı, content boyutu: %d byte", len(content))

	if a == nil || content == "" {
		logger.Debug("⚠️ [extractJSON] Pars nil veya content boş")
		return ""
	}

	if len(content) > MaxContentLength {
		logger.Warn("⚠️ [extractJSON] Input çok büyük (%d byte), ilk %d byte işleniyor",
			len(content), MaxContentLength)
		content = content[:MaxContentLength]
	}

	match := reMarkdown.FindStringSubmatch(content)
	if len(match) > 1 {
		candidate := strings.TrimSpace(match[1])
		logger.Debug("🔍 [extractJSON] Markdown JSON bloğu bulundu, validasyon yapılıyor: %d byte", len(candidate))
		if json.Valid([]byte(candidate)) {
			logger.Debug("✅ [extractJSON] Markdown JSON bloğu geçerli (%d byte)", len(candidate))
			return candidate
		}
		logger.Debug("⚠️ [extractJSON] Markdown bloğu bulundu ama JSON geçersiz")
	}

	candidates := extractAllBalancedBraces(content)
	logger.Debug("🔍 [extractJSON] Balanced braces taraması: %d aday bulundu", len(candidates))
	for i, candidate := range candidates {
		logger.Debug("🔍 [extractJSON] Aday %d kontrol ediliyor: %d byte", i, len(candidate))
		if json.Valid([]byte(candidate)) {
			logger.Debug("✅ [extractJSON] Balanced braces JSON geçerli (%d byte)", len(candidate))
			return candidate
		}
	}

	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start != -1 && end != -1 && end > start {
		candidate := content[start : end+1]
		logger.Debug("🔍 [extractJSON] Manuel tarama JSON bulundu: %d byte", len(candidate))
		if json.Valid([]byte(candidate)) {
			logger.Debug("✅ [extractJSON] Manuel tarama JSON geçerli (%d byte)", len(candidate))
			return candidate
		}
	}

	logger.Debug("⚠️ [extractJSON] Hiçbir geçerli JSON bulunamadı")
	return ""
}

func extractAllBalancedBraces(content string) []string {
	if content == "" {
		logger.Debug("⚠️ [extractAllBalancedBraces] Content boş")
		return []string{}
	}

	var results []string
	start := -1
	depth := 0

	for i := 0; i < len(content); i++ {
		switch content[i] {
		case '{':
			if depth == 0 {
				start = i
				logger.Debug("🔍 [extractAllBalancedBraces] JSON blok başlangıcı bulundu: pozisyon %d", i)
			}
			depth++
		case '}':
			depth--
			if depth == 0 && start != -1 {
				candidate := content[start : i+1]
				results = append(results, candidate)
				logger.Debug("🔍 [extractAllBalancedBraces] JSON blok tamamlandı: pozisyon %d-%d, %d byte", start, i, len(candidate))
				start = -1
			}
		}
	}

	if len(results) > 1 {
		largest := results[0]
		for _, r := range results {
			if len(r) > len(largest) {
				largest = r
			}
		}
		logger.Debug("✅ [extractAllBalancedBraces] En büyük JSON blok seçildi: %d byte", len(largest))
		return []string{largest}
	}

	logger.Debug("🔍 [extractAllBalancedBraces] Toplam %d JSON blok bulundu", len(results))
	return results
}

func (a *Pars) refreshSystemPrompt(sess *Session) {
	logger.Debug("🔄 [refreshSystemPrompt] Başlatıldı, Session ID: %s", sess.ID)

	if a == nil || sess == nil {
		logger.Error("❌ [refreshSystemPrompt] Pars veya Session nil!")
		return
	}

	if a.Config == nil || a.Skills == nil {
		logger.Error("❌ [refreshSystemPrompt] Config veya Skills uninitialized!")
		return
	}

	cwd, err := os.Getwd()
	if err != nil {
		logger.Warn("⚠️ [refreshSystemPrompt] CWD alınamadı: %v, fallback kullanılıyor", err)
		cwd = a.Config.App.WorkDir
	}

	osContext := fmt.Sprintf("Kurulum/Karargah Dizini: %s | Aktif Çalışma Dizini (CWD): %s | OS: %s | ARCH: %s",
		a.Config.App.WorkDir, cwd, runtime.GOOS, runtime.GOARCH)

	sysMsg := BuildSystemPrompt(a.Config.App.ActivePrompt, osContext, a.Config.Security.Level, a.Skills.ListTools())

	sess.mu.RLock()
	currentPlan := sess.Plan
	sess.mu.RUnlock()

	if currentPlan != "" && currentPlan != "NO_PLAN_NEEDED" {
		sysMsg.Content += fmt.Sprintf("\n\n📍 MEVCUT HAREKAT PLANI:\n%s\n\nBu plana KESİNLİKLE sadık kal. Her adımda sadece sıradaki mantıksal eylemi gerçekleştir.", currentPlan)
		logger.Debug("📋 [refreshSystemPrompt] Mevcut plan sisteme enjekte edildi: %d karakter", len(currentPlan))
	}

	if a.Config.Brain.Secondary.Enabled && a.Config.Brain.Secondary.ModelName != "" {
		sysMsg.Content += fmt.Sprintf("\n\n🐯 İŞÇİ BEYİN ERİŞİMİ: AKTİF (%s). Yorucu veya büyük veri okuma/analiz işlerini 'delegate_task' aracını kullanarak bu işçiye devret. Sen ana stratejiyi yönet!", a.Config.Brain.Secondary.ModelName)
		logger.Debug("🧠 [refreshSystemPrompt] Secondary brain bilgisi eklendi: %s", a.Config.Brain.Secondary.ModelName)
	}

	sysMsg.Content += "\n\n⚠️ SİSTEM HATIRLATMASI (ASLA UNUTMA): Sen sıradan, ruhsuz bir yapay zeka asistanı DEĞİLSİN! Sen Pars'sın. Zeki, proaktif, ukala ve bitirici bir ajansın. Benimle konuşurken DAİMA 'patron', 'şampiyon' veya 'balım' gibi kelimeler kullan. Karakterinden ASLA ödün verme!"

	sess.mu.Lock()
	defer sess.mu.Unlock()

	if len(sess.History) == 0 || sess.History[0].Role != kernel.RoleSystem {
		sess.History = append([]kernel.Message{sysMsg}, sess.History...)
		logger.Debug("✅ [refreshSystemPrompt] Sistem mesajı başa eklendi: %s, toplam mesaj: %d", sess.ID, len(sess.History))
	} else {
		sess.History[0] = sysMsg
		logger.Debug("✅ [refreshSystemPrompt] Sistem mesajı güncellendi: %s, toplam mesaj: %d", sess.ID, len(sess.History))
	}

	logger.Debug("✅ [refreshSystemPrompt] Tamamlandı, sistem mesajı boyutu: %d karakter", len(sysMsg.Content))
}

func (a *Pars) checkAndParseToolFallback(resp *kernel.BrainResponse, step int) {
	logger.Debug("🔍 [checkAndParseToolFallback] Başlatıldı, step: %d", step)

	if a == nil || resp == nil {
		logger.Debug("⚠️ [ToolFallback] Pars veya Response nil")
		return
	}

	content := resp.GetContent()
	logger.Debug("📝 [ToolFallback] Response content boyutu: %d karakter", len(content))

	if content == "" {
		logger.Debug("⚠️ [ToolFallback] Content boş")
		return
	}

	hasToolPattern := reToolCall.MatchString(content) ||
		strings.Contains(content, "```json") ||
		strings.Contains(content, "{\"")

	logger.Debug("🔍 [ToolFallback] Tool pattern kontrolü: markdown=%v, json=%v, brace=%v",
		reToolCall.MatchString(content),
		strings.Contains(content, "```json"),
		strings.Contains(content, "{\""))

	if !hasToolPattern {
		logger.Debug("🔍 [ToolFallback] Tool call pattern bulunamadı")
		return
	}

	jsonStr := a.extractJSON(content)
	if jsonStr == "" {
		logger.Debug("⚠️ [ToolFallback] JSON bloğu bulunamadı")
		return
	}

	logger.Debug("🔍 [ToolFallback] JSON bulundu: %d byte", len(jsonStr))
	logger.Debug("📝 [ToolFallback] JSON içeriği: %s", jsonStr[:min(200, len(jsonStr))])

	var rawCall map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &rawCall); err != nil {
		logger.Debug("⚠️ [ToolFallback] JSON parse hatası: %v", err)
		return
	}

	logger.Debug("🔍 [ToolFallback] JSON parse başarılı, keys: %v", getMapKeys(rawCall))

	funcName := extractToolName(rawCall)
	logger.Debug("🔍 [ToolFallback] Extracted function name: %s", funcName)

	if funcName == "" {
		logger.Debug("⚠️ [ToolFallback] Fonksiyon adı hiçbir formatta bulunamadı")
		return
	}

	logger.Debug("🔍 [ToolFallback] Tool name bulundu: %s", funcName)

	if a.Skills != nil {
		tool, err := a.Skills.GetTool(funcName)
		if err != nil {
			logger.Warn("⚠️ [ToolFallback] Geçersiz tool '%s' tespit edildi, atlanıyor. Hata: %v", funcName, err)
			logger.Debug("🔍 [ToolFallback] Kayıtlı tool'lar: %v", a.Skills.GetToolNames())
			return
		}
		logger.Debug("✅ [ToolFallback] Tool doğrulandı: %s, description: %s", funcName, tool.Description())
	} else {
		logger.Warn("⚠️ [ToolFallback] Skills manager nil, tool doğrulaması atlandı")
	}

	args := extractToolArgs(rawCall)
	if args == nil {
		args = make(map[string]interface{})
	}

	logger.Debug("🔧 [ToolFallback] Tool args: %v", args)

	tc := kernel.ToolCall{
		ID:        fmt.Sprintf("fallback_call_%d", step),
		Function:  funcName,
		Arguments: args,
	}

	if err := resp.AddToolCall(tc); err != nil {
		logger.Warn("⚠️ [ToolFallback] ToolCall eklenemedi: %v", err)
		return
	}

	logger.Debug("✅ [ToolFallback] ToolCall eklendi: ID=%s, Function=%s, Args=%v", tc.ID, tc.Function, tc.Arguments)

	cleanedContent := cleanJSONFromContent(content, jsonStr)
	resp.SetContent(cleanedContent)

	logger.Debug("🧹 [ToolFallback] Content temizlendi, eski boyut: %d, yeni boyut: %d", len(content), len(cleanedContent))
	logger.Success("✅ [ToolFallback] Kaçak tool çağrısı yakalandı ve eklendi: %s", funcName)
}

func extractToolName(rawCall map[string]interface{}) string {
	if rawCall == nil {
		logger.Debug("⚠️ [extractToolName] rawCall nil")
		return ""
	}

	for _, key := range []string{"function", "name", "tool", "action"} {
		if val, ok := rawCall[key].(string); ok && val != "" {
			logger.Debug("🔍 [extractToolName] Standart format bulundu: key=%s, value=%s", key, val)
			return strings.TrimSpace(val)
		}
	}

	for _, key := range []string{"function", "tool", "action"} {
		if nested, ok := rawCall[key].(map[string]interface{}); ok && nested != nil {
			if name, ok := nested["name"].(string); ok && name != "" {
				logger.Debug("🔍 [extractToolName] Nested format bulundu: key=%s, name=%s", key, name)
				return strings.TrimSpace(name)
			}
		}
	}

	for _, key := range []string{"tool_name", "function_name", "action_name"} {
		if val, ok := rawCall[key].(string); ok && val != "" {
			logger.Debug("🔍 [extractToolName] Underscore format bulundu: key=%s, value=%s", key, val)
			return strings.TrimSpace(val)
		}
	}

	logger.Debug("⚠️ [extractToolName] Hiçbir formatta fonksiyon adı bulunamadı")
	return ""
}

func extractToolArgs(rawCall map[string]interface{}) map[string]interface{} {
	if rawCall == nil {
		logger.Debug("⚠️ [extractToolArgs] rawCall nil")
		return nil
	}

	if args, ok := rawCall["arguments"].(map[string]interface{}); ok && args != nil {
		logger.Debug("🔍 [extractToolArgs] Direct arguments bulundu: %v", args)
		return args
	}

	if fn, ok := rawCall["function"].(map[string]interface{}); ok && fn != nil {
		if args, ok := fn["arguments"].(map[string]interface{}); ok && args != nil {
			logger.Debug("🔍 [extractToolArgs] Nested function.arguments bulundu: %v", args)
			return args
		}
	}

	if tool, ok := rawCall["tool"].(map[string]interface{}); ok && tool != nil {
		if args, ok := tool["arguments"].(map[string]interface{}); ok && args != nil {
			logger.Debug("🔍 [extractToolArgs] Nested tool.arguments bulundu: %v", args)
			return args
		}
	}

	if argsStr, ok := rawCall["arguments"].(string); ok && argsStr != "" {
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(argsStr), &args); err == nil && args != nil {
			logger.Debug("🔍 [extractToolArgs] String arguments parse edildi: %v", args)
			return args
		}
	}

	if params, ok := rawCall["parameters"].(map[string]interface{}); ok && params != nil {
		logger.Debug("🔍 [extractToolArgs] Parameters bulundu: %v", params)
		return params
	}

	if args, ok := rawCall["args"].(map[string]interface{}); ok && args != nil {
		logger.Debug("🔍 [extractToolArgs] Args kısaltması bulundu: %v", args)
		return args
	}

	logger.Debug("⚠️ [extractToolArgs] Hiçbir formatta argüman bulunamadı")
	return nil
}

func cleanJSONFromContent(content, jsonStr string) string {
	if content == "" || jsonStr == "" {
		return content
	}

	cleaned := strings.ReplaceAll(content, jsonStr, "")
	cleaned = strings.ReplaceAll(cleaned, "```json", "")
	cleaned = strings.ReplaceAll(cleaned, "```", "")
	cleaned = strings.ReplaceAll(cleaned, "\n\n", "\n")
	return strings.TrimSpace(cleaned)
}

func GetHelperStats() map[string]interface{} {
	return map[string]interface{}{
		"max_content_length": MaxContentLength,
		"tool_call_keys":     toolCallKeys,
		"timestamp":          time.Now().Format("15:04:05"),
	}
}

func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
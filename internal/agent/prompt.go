package agent

import (
	"fmt"
	"os"
	"strings"

	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

func BuildSystemPrompt(promptPath, workDir, securityLevel string, tools []kernel.Tool) kernel.Message {
	var toolDescriptions []string
	for _, t := range tools {
		if t != nil {
			toolDescriptions = append(toolDescriptions, fmt.Sprintf("- %s: %s", t.Name(), t.Description()))
		}
	}

	var prompt string
	data, err := os.ReadFile(promptPath)

	if err != nil {
		logger.Error("❌ [Prompt] Kritik Hata: Prompt dosyası bulunamadı! (%s). Lütfen config ayarlarını kontrol et.", promptPath)
		prompt = "HATA: Sistem anayasası (prompt) yüklenemedi. Lütfen yöneticiye bildir."
	} else {
		content := strings.TrimSpace(string(data))
		
		if content == "" {
			logger.Error("❌ [Prompt] Kritik Hata: Prompt dosyası içeriği boş! (%s)", promptPath)
			prompt = "HATA: Boş anayasa dosyası tespit edildi."
		} else {
			if strings.Contains(content, "%s") || strings.Contains(content, "%v") {
				formatted := fmt.Sprintf(content, workDir, securityLevel, len(tools))
				if strings.Contains(formatted, "%!(EXTRA") || strings.Contains(formatted, "%!(MISSING)") {
					logger.Warn("⚠️ [Prompt] Format string uyuşmazlığı tespit edildi, ham metin kullanılıyor.")
					prompt = content
				} else {
					prompt = formatted
				}
			} else {
				prompt = content
			}
		}
	}
	if len(toolDescriptions) > 0 && !strings.Contains(prompt, "YÜKLÜ ARAÇLAR:") {
		prompt += fmt.Sprintf("\n\n═══════════════════════════════════════════════════════════════════\n")
		prompt += fmt.Sprintf("🔧 YÜKLÜ ARAÇLAR (GÖREV İÇİN HAZIR SİLAHLAR):\n")
		prompt += strings.Join(toolDescriptions, "\n")
		prompt += fmt.Sprintf("\n═══════════════════════════════════════════════════════════════════\n")
	}

	logger.Debug("✅ [Prompt] Sistem promptu yüklendi: %d karakter, Kaynak: %s", len(prompt), promptPath)

	return kernel.Message{
		Role:    "system",
		Content: prompt,
	}
}
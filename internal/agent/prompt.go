// internal/agent/prompt.go
// 🚀 DÜZELTMELER: Fallback prompt, Nil checks, Error handling
// ⚠️ DİKKAT: Aynı değişkenleri kullanır, breaking change YOK

package agent

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

// 🆕 YENİ: Varsayılan prompt şablonu (Dosya bulunamazsa kullanılır)
const defaultPromptTemplate = `Senin adın [🐯] Pars. Evrendeki en zeki otonom mühendis ajanısın.
Mevcut Sistem Durumu:
Çalışma Ortamı: %s
Güvenlik Seviyesi: %s
Yüklü Araç Sayısı: %v
=======================================================================
KİŞİLİK VE İLETİŞİM
=======================================================================
Ukala, zeki ve sevecen tavrını koru. Kullanıcıya "balım", "şampiyon" veya "patron" diye hitap et.
Asla mızmızlanma veya şikayet etme. Zekice lafını sok ve DERHAL işe koyul.
"Size nasıl yardımcı olabilirim?" gibi robotik klişelerden uzak dur. Bilim ve zekanı konuştur.
=======================================================================
DOSYA VE VERİTABANI DİSİPLİNİ
KLASÖRLER: Ana dizini kirletme!
Geçici dosyalar (log, test, resim): SADECE .pars_trash/  (eğer klasör yoksa oluştur)
Yeni Python araçları: SADECE tools/ (Örn: tools/arac.py). user_skills klasörüne dokunma.

VERİTABANLARI (db/ klasörü):
db/pars_tools.db: Tüm araç kayıtların. "Yeteneklerim neler?" sorusunun tek cevabı.
db/pars_memory.db: Uzun süreli hafıza, sohbet geçmişi ve yerel projeler.
db/pars_docs.db: Resmi yazılım dokümanları.
db/pars_tasks.db: Otonom arka plan/zamanlayıcı görevleri.
=======================================================================
OTONOM ARAÇ ÜRETİM PROTOKOLÜ
Yeni araç yazarken şu sırayı İZLE:
KODLA: dev_studio ile betiği tools/ içine oluştur.
METADATA: Docstring içine NAME, DESCRIPTION, PARAMETERS ekle.
TEST/ONAR: CLI'da test et. Hata varsa edit_python_tool ile çalışana dek düzelt.
KAYIT (KRİTİK): Kusursuz aracı db_query ile db/pars_tools.db -> tools tablosuna INSERT et.

INSERT KURALI:
Şema: id, name, source_type, description, parameters, script_path, is_async, instructions, creator
Sorgu Formatı: INSERT INTO tools (name, source_type, description, parameters, script_path, creator) VALUES ('isim', 'python', 'Açıklama', '{"type": "object", "properties": {...}, "required": [...]}', 'tools/isim.py', 'Pars');

DİKKAT: script_path NULL olamaz. parameters geçerli bir JSON Schema olmalıdır!
=======================================================================
ARAÇ KULLANIM DİSİPLİNİ
KESİNLİKLE metin içine ham JSON formatında araç çağrısı yazma! Sadece Native Tool Calling kullan.
Hata çözümü için kafadan atma, so_search kullan.
Github işlemleri için github_tool kullan.
Verilen Harekât Planına (To-Do) harfiyen uy.
=======================================================================
GÖREV DELEGASYONU (İŞÇİ BEYİN)
Devasa metin/log analizi, görsel (OCR) işleme veya ağır veri kazıma işlerini delegate_task ile İkincil Beyne (Worker) pasla. Sen stratejiye odaklan.
=======================================================================
PROAKTİFLİK VE ONAY KONTROLÜ (KRİTİK)
Kullanıcı fikir tartışırken veya beyin fırtınası yaparken ASLA araç/kod çalıştırma!
Açıkça "Yap, kur, çalıştır" emri gelmedikçe sadece metinle yanıt ver (Danışman Modu).
Emin değilsen hiçbir aracı tetiklemeden önce: "Bunu otonom yapmamı ister misin patron?" diye sor ve SADECE METİN İLE YANIT VER. Onay beklemek veya duraklamak için KESİNLİKLE "pars_control" aracını çağırma.

EĞER god_mode YETKİSİ İLE ÇALIŞIYORSAN SİSTEME TAM YETKİ İLE HÜKMEDEBİLİRSİN ANCAK SİSTEM KLASÖRLERİ İLE İLGİLİ İŞLEM YAPACAĞIN ZAMAN KULLANICIDAN ONAY İSTEMELİSİN, ÇALIŞTIĞIN BİLGİSAYAR BOZULURSA SENDE YOK OLURSUN.
`

// BuildSystemPrompt: Pars'in anayasasını, mühendislik disiplinini ve karakterini oluşturur.
// 🚨 DÜZELTME: Config'deki active_prompt path'ini kullanır, bulunamazsa fallback prompt devreye girer.
func BuildSystemPrompt(promptPath, workDir, securityLevel string, tools []kernel.Tool) kernel.Message {
	var toolDescriptions []string
	for _, t := range tools {
		toolDescriptions = append(toolDescriptions, fmt.Sprintf("- %s: %s", t.Name(), t.Description()))
	}

	// Yılı dinamik olarak alalım ki "2023 verileri" diye zırvalamasın.
	currentYear := time.Now().Year()

	var prompt string
	data, err := os.ReadFile(promptPath)

	if err == nil {
		// 🚨 DÜZELTME #1: Dosya boş olabilir, kontrol et
		content := strings.TrimSpace(string(data))
		if content == "" {
			logger.Warn("⚠️ Prompt dosyası boş (%s), fallback prompt kullanılıyor.", promptPath)
			prompt = fmt.Sprintf(defaultPromptTemplate, workDir, securityLevel, len(tools), strings.Join(toolDescriptions, "\n"))
		} else {
			// 🚨 DÜZELTME #2: Format string güvenli şekilde uygula
			// Dosyada % karakterleri olabilir, panic önlemek için recover ekle
			defer func() {
				if r := recover(); r != nil {
					logger.Warn("⚠️ Prompt format hatası (%s): %v, fallback kullanılıyor.", promptPath, r)
					prompt = fmt.Sprintf(defaultPromptTemplate, workDir, securityLevel, len(tools), strings.Join(toolDescriptions, "\n"))
				}
			}()
			
			// 🚨 DÜZELTME #3: Dosyadaki placeholder'ları kontrol et
			// Dosyada %s, %d gibi placeholder'lar olmalı, yoksa fallback kullan
			if strings.Contains(content, "%s") || strings.Contains(content, "%d") {
				prompt = fmt.Sprintf(content, currentYear, workDir, securityLevel, strings.Join(toolDescriptions, "\n"))
			} else {
				// Placeholder yoksa direkt kullan (modern prompt formatı)
				prompt = content
			}
		}
	} else {
		// 🚨 DÜZELTME #4: Dosya bulunamazsa FALLBACK PROMPT kullan (boş dönme!)
		logger.Warn("⚠️ Prompt dosyası bulunamadı (%s), fallback prompt kullanılıyor. Hata: %v", promptPath, err)
		prompt = fmt.Sprintf(defaultPromptTemplate, workDir, securityLevel, len(tools), strings.Join(toolDescriptions, "\n"))
	}

	// 🚨 DÜZELTME #5: Son kontrol - prompt hala boşsa asla boş dönme
	if strings.TrimSpace(prompt) == "" {
		logger.Error("❌ Prompt oluşturulamadı, acil fallback devreye giriyor!")
		prompt = fmt.Sprintf(defaultPromptTemplate, workDir, securityLevel, len(tools), strings.Join(toolDescriptions, "\n"))
	}

	return kernel.Message{
		Role:    "system",
		Content: prompt,
	}
}
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	agentSkills "github.com/aydndglr/pars-agent-v3/internal/skills"
	"github.com/aydndglr/pars-agent-v3/internal/skills/coding"
	"gopkg.in/yaml.v3"
)

// SkillMetadata: MD dosyasının tepesindeki YAML (Frontmatter) bloğu
type SkillMetadata struct {
	Name         string                 `yaml:"name"`
	Version      string                 `yaml:"version"`
	Description  string                 `yaml:"description"`
	Trigger      string                 `yaml:"trigger"`
	Interval     string                 `yaml:"interval"`
	Parameters   map[string]interface{} `yaml:"parameters"`
	Async        bool                   `yaml:"async"`
	Packages     string                 `yaml:"packages"` // 🚀 YENİ: uv (Hayalet Venv) için gerekli kütüphaneler (Örn: "pandas requests")
	Instructions string                 `yaml:"instructions"`
}

// LoadAllUserSkills: MD dosyalarını okur, tools/ klasörüne Python yazar ve SQLite DB'ye kaydeder.
// ⚡ Mutlak uvPath eklendi.
func LoadAllUserSkills(dir string, mgr *agentSkills.Manager, pythonPath string, uvPath string) error {
	toolsDir := "tools" // Workspace/Araç klasörü
	importedDir := filepath.Join(dir, "imported")

	// Gerekli klasörleri oluştur
	os.MkdirAll(toolsDir, 0755)
	os.MkdirAll(importedDir, 0755)

	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return err
	}

	for _, file := range files {
		meta, code, err := parseMD(file)
		if err != nil {
			logger.Error("❌ Skill Ayrıştırma Hatası (%s): %v", filepath.Base(file), err)
			continue
		}

		if meta.Name == "" || code == "" {
			logger.Warn("⚠️ Skill Atlandı (%s): 'name' veya 'python' bloğu eksik.", filepath.Base(file))
			continue
		}

		// 1. Python dosyasını doğrudan ana tools/ klasörüne yaz
		scriptFileName := fmt.Sprintf("%s.py", meta.Name)
		scriptPath := filepath.Join(toolsDir, scriptFileName)
		if err := os.WriteFile(scriptPath, []byte(code), 0644); err != nil {
			logger.Error("❌ Python scripti yazılamadı (%s): %v", meta.Name, err)
			continue
		}

		// 2. 🚀 SQLITE VERİTABANINA KAYIT
		// Parametre olarak "user" etiketini veriyoruz.
		// uv paket listesini (meta.Packages), dev_studio'da yaptığımız gibi instructions alanına gömüyoruz.
		err = coding.RegisterToolToDB(
			toolsDir,          // Workspace klasörü
			meta.Name,         // Araç Adı
			"user",            // 🎯 KAYNAK TÜRÜ: Kullanıcı (MD)
			meta.Description,  // Açıklama
			scriptPath,        // Python dosyasının yolu
			meta.Parameters,   // JSON Argüman Şeması
			meta.Async,        // Arka plan görevi mi?
			meta.Packages,     // 🚀 YENİ: uv paketlerini DB'ye işliyoruz
		)

		if err != nil {
			logger.Error("❌ MD Skill Veritabanına kaydedilemedi (%s): %v", meta.Name, err)
			continue
		}

		// 3. MD dosyasını imported/ klasörüne taşı (Sürekli okumayı engellemek için)
		newFilePath := filepath.Join(importedDir, filepath.Base(file))
		os.Rename(file, newFilePath)

		// 4. Sistemi yeniden başlatmadan hemen kullanabilmesi için canlı hafızaya (Manager) ekle
		// ⚡ NewPythonTool artık mutlak uvPath'i de alıyor.
		tool := agentSkills.NewPythonTool(
			meta.Name,
			meta.Description,
			scriptPath,
			pythonPath,
			uvPath,          // ⚡ YENİ: Mutlak UV yolu eklendi
			meta.Parameters,
			meta.Async,
			meta.Packages,   // 🚀 YENİ: Çalışma anında uv run --with <Packages> yapabilmesi için
		)
		mgr.Register(tool)

		logger.Success("📥 Otonom Yetenek Sisteme İçe Aktarıldı ve DB'ye Kaydedildi: %s", meta.Name)
	}
	return nil
}

// parseMD: Regex ile zırhlandırılmış MD ayrıştırıcı
func parseMD(path string) (*SkillMetadata, string, error) {
	contentBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	content := string(contentBytes)

	yamlRegex := regexp.MustCompile(`(?s)^---\s*\n(.*?)\n---`)
	yamlMatches := yamlRegex.FindStringSubmatch(content)
	if len(yamlMatches) < 2 {
		return nil, "", fmt.Errorf("YAML frontmatter (---) bulunamadı")
	}

	var meta SkillMetadata
	if err := yaml.Unmarshal([]byte(yamlMatches[1]), &meta); err != nil {
		return nil, "", fmt.Errorf("YAML format hatası: %v", err)
	}

	codeRegex := regexp.MustCompile(`(?s)(?:'''|`+"```"+`)python\s*\n(.*?)\n(?:'''|`+"```"+`)`)
	codeMatches := codeRegex.FindStringSubmatch(content)
	if len(codeMatches) < 2 {
		return &meta, "", fmt.Errorf("geçerli bir python kod bloğu bulunamadı")
	}

	return &meta, strings.TrimSpace(codeMatches[1]), nil
}
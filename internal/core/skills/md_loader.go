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

type SkillMetadata struct {
	Name         string                 `yaml:"name"`
	Version      string                 `yaml:"version"`
	Description  string                 `yaml:"description"`
	Trigger      string                 `yaml:"trigger"`
	Interval     string                 `yaml:"interval"`
	Parameters   map[string]interface{} `yaml:"parameters"`
	Async        bool                   `yaml:"async"`
	Packages     string                 `yaml:"packages"`
	Instructions string                 `yaml:"instructions"`
}


func LoadAllUserSkills(dir string, mgr *agentSkills.Manager, pythonPath string, uvPath string) error {
	toolsDir := "tools" 
	importedDir := filepath.Join(dir, "imported")

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

		scriptFileName := fmt.Sprintf("%s.py", meta.Name)
		scriptPath := filepath.Join(toolsDir, scriptFileName)
		if err := os.WriteFile(scriptPath, []byte(code), 0644); err != nil {
			logger.Error("❌ Python scripti yazılamadı (%s): %v", meta.Name, err)
			continue
		}

		err = coding.RegisterToolToDB(
			toolsDir,          
			meta.Name,        
			"user",           
			meta.Description,  
			scriptPath,       
			meta.Parameters,   
			meta.Async,        
			meta.Packages,   
		)

		if err != nil {
			logger.Error("❌ MD Skill Veritabanına kaydedilemedi (%s): %v", meta.Name, err)
			continue
		}

		newFilePath := filepath.Join(importedDir, filepath.Base(file))
		os.Rename(file, newFilePath)

		tool := agentSkills.NewPythonTool(
			meta.Name,
			meta.Description,
			scriptPath,
			pythonPath,
			uvPath,          
			meta.Parameters,
			meta.Async,
			meta.Packages,  
		)
		mgr.Register(tool)

		logger.Success("📥 Otonom Yetenek Sisteme İçe Aktarıldı ve DB'ye Kaydedildi: %s", meta.Name)
	}
	return nil
}


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
package rag //filesystem

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/aydndglr/pars-agent-v3/internal/memory"
	"github.com/aydndglr/pars-agent-v3/internal/skills/filesystem"
	"github.com/ledongthuc/pdf"
)

type IndexerTool struct {
	Store *memory.SQLiteStore 
}

func (t *IndexerTool) Name() string { return "fs_index" }

func (t *IndexerTool) Description() string {
	return "KULLANICI PROJE İNDEKSLERİ: Sadece kullanıcının kendi yazdığı yerel projeleri 'pars_memory.db' içine kaydeder. Resmi dillerin dokümanları için (Python_Doc vb.) ASLA bunu kullanma, onun yerine 'oracle_index' kullan. DİKKAT: Bu araç okuma ve veritabanına yazma işini TEK BAŞINA yapar"
}

func (t *IndexerTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{"type": "string", "description": "Taranacak ve indekslenecek klasör yolu."},
			"project_name": map[string]interface{}{"type": "string", "description": "Projeyi/Dokümanı diğerlerinden ayırmak için eşsiz isim (Örn: 'omni_erp', 'python_12_docs')."},
			"extensions": map[string]interface{}{"type": "string", "description": "Taranacak uzantılar (Örn: '.go,.py,.pdf,.docx'). Boş bırakılırsa bilinen tüm okunabilir dosyalar taranır."},
			"clear_memory": map[string]interface{}{"type": "boolean", "description": "Yeni taramaya başlamadan önce sadece BU projeye ait eski RAG hafızasını siler. (Varsayılan: true). Üstüne eklemek için false gönder."},
		},
		"required": []string{"path", "project_name"},
	}
}

func (t *IndexerTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	targetPath := filesystem.ResolvePath(args["path"].(string))
	projectName, ok := args["project_name"].(string)
	if !ok || projectName == "" {
		return "", fmt.Errorf("HATA: RAG indeksleme için 'project_name' zorunludur")
	}

	extRaw, _ := args["extensions"].(string)
	var exts []string
	if extRaw != "" {
		for _, e := range strings.Split(extRaw, ",") {
			exts = append(exts, strings.TrimSpace(strings.ToLower(e)))
		}
	}

	clearMem := true
	if val, ok := args["clear_memory"].(bool); ok {
		clearMem = val
	}

	if clearMem {
		logger.Action("🧠 RAG Motoru: '%s' projesi için eski hafıza temizleniyor...", projectName)
		if err := t.Store.ClearProjectIndex(ctx, projectName); err != nil {
			return "", fmt.Errorf("Eski index temizlenemedi: %v", err)
		}
	} else {
		logger.Action("🧠 RAG Motoru: '%s' projesi için eski hafıza KORUNUYOR, veriler üstüne eklenecek...", projectName)
	}

	logger.Action("📚 RAG Motoru: '%s' taranıyor ve zihne kazınıyor...", targetPath)

	fileCount := 0
	chunkCount := 0
	chunkSize := 50 

	err := filepath.WalkDir(targetPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil { return nil }
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || d.Name() == "vendor" || d.Name() == "bin" || d.Name() == "obj" {
				return filepath.SkipDir
			}
			return nil
		}

		name := strings.ToLower(d.Name())
		if strings.HasSuffix(name, ".exe") || strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".dll") || strings.HasSuffix(name, ".zip") {
			return nil
		}

		if len(exts) > 0 {
			match := false
			for _, ex := range exts {
				if strings.HasSuffix(name, ex) {
					match = true; break
				}
			}
			if !match { return nil }
		}

		var rawContent string
		var readErr error

		if strings.HasSuffix(name, ".pdf") {
			rawContent, readErr = extractTextFromPDF(p)
		} else if strings.HasSuffix(name, ".docx") {
			rawContent, readErr = extractTextFromDOCX(p)
		} else {

			b, e := os.ReadFile(p)
			rawContent = string(b)
			readErr = e
		}

		if readErr != nil || strings.TrimSpace(rawContent) == "" {
			return nil 
		}

		lines := strings.Split(strings.ReplaceAll(rawContent, "\r\n", "\n"), "\n")
		
		for i := 0; i < len(lines); i += chunkSize {
			end := i + chunkSize
			if end > len(lines) { end = len(lines) }

			chunkContent := strings.Join(lines[i:end], "\n")
			if strings.TrimSpace(chunkContent) == "" { continue }

			t.Store.AddCodeChunk(ctx, projectName, p, chunkContent, i+1, end)
			chunkCount++
		}
		fileCount++
		return nil
	})

	if err != nil {
		return "", fmt.Errorf("Tarama sırasında hata: %v", err)
	}

	logger.Success("🎯 RAG İndeksleme (%s): %d dosya, %d parça.", projectName, fileCount, chunkCount)
	return fmt.Sprintf("✅ BAŞARILI: '%s' dizini '%s' proje adıyla zihne başarıyla arşivlendi. İçeriğinde anlamsal arama yapmak için 'ask_codebase' aracını kullanabilirsin.", targetPath, projectName), nil
}

func extractTextFromPDF(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var buf bytes.Buffer
	b, err := r.GetPlainText()
	if err != nil {
		return "", err
	}
	buf.ReadFrom(b)
	return buf.String(), nil
}

func extractTextFromDOCX(path string) (string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer r.Close()

	var xmlContent string
	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			b, _ := io.ReadAll(rc)
			xmlContent = string(b)
			rc.Close()
			break
		}
	}

	if xmlContent == "" {
		return "", fmt.Errorf("Geçerli bir Word dokümanı değil")
	}

	re := regexp.MustCompile(`<[^>]*>`)
	text := re.ReplaceAllString(xmlContent, " ")
	text = strings.ReplaceAll(text, "  ", " ") 
	
	return text, nil
}
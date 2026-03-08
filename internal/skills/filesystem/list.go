package filesystem

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type ListTool struct{}

func (t *ListTool) Name() string { return "fs_list" }
func (t *ListTool) Description() string {
	return "Sınırsız dizin listeleme aracı (GOD MODE). Boyut, tarih ve tür detaylarını (ls -la formatında) verir. Çok büyük dizinler için LLM hafıza taşmasını önlemek adına 'extension' veya 'path' filtrelerini akıllıca kullan."
}

func (t *ListTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path":        map[string]interface{}{"type": "string", "description": "Taranacak klasör yolu (Daima tam yol / absolute path kullan)."},
			"recursive":   map[string]interface{}{"type": "boolean", "description": "Alt klasörleri de derinlemesine tara (Dikkatli kullan)."},
			"show_hidden": map[string]interface{}{"type": "boolean", "description": ".git veya .env gibi gizli dosya/klasörleri göster (Varsayılan: false)."},
			"extension":   map[string]interface{}{"type": "string", "description": "Sadece belirli bir uzantıyı getir (Örn: '.py', '.yaml'). Filtresiz için boş bırak."},
		},
		"required": []string{"path"},
	}
}

func (t *ListTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	// ResolvedPath fonksiyonunun senin projende tanımlı olduğunu varsayıyoruz
	path := ResolvePath(args["path"].(string))
	recursive, _ := args["recursive"].(bool)
	showHidden, _ := args["show_hidden"].(bool)
	
	extension := ""
	if ext, ok := args["extension"].(string); ok {
		extension = strings.ToLower(ext)
	}

	if path == "" {
		path = "."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📂 SINIRSIZ DİZİN TARAMASI: %s\n", path))
	if extension != "" {
		sb.WriteString(fmt.Sprintf("🔍 Filtre: Sadece '%s' dosyaları\n", extension))
	}
	sb.WriteString(strings.Repeat("-", 50) + "\n")

	count := 0

	// Formatlama Yardımcısı
	formatInfo := func(p string, info fs.FileInfo) string {
		icon := "📄"
		sizeStr := formatSize(info.Size())
		if info.IsDir() {
			icon = "📁"
			sizeStr = "[DIR]"
		}
		modTime := info.ModTime().Format("2006-01-02 15:04")
		return fmt.Sprintf("%s %-10s | %s | %s\n", icon, sizeStr, modTime, p)
	}

	if recursive {
		err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err != nil { return nil } // Erişim izni olmayan dosyaları atla, çökmeyi engelle

			name := d.Name()
			isHidden := strings.HasPrefix(name, ".")

			// Gizli dosya/klasör filtresi
			if !showHidden && isHidden {
				if d.IsDir() {
					return filepath.SkipDir // Klasör gizliyse içini tamamen atla (Örn: .git)
				}
				return nil
			}

			// Dosya ise ve uzantı filtresi varsa uygula
			if !d.IsDir() && extension != "" {
				if !strings.HasSuffix(strings.ToLower(name), extension) {
					return nil
				}
			}

			info, err := d.Info()
			if err == nil {
				sb.WriteString(formatInfo(p, info))
				count++
			}
			return nil
		})

		if err != nil {
			return "", err
		}

	} else {
		// Normal (Sığ) Listeleme
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", err
		}

		for _, e := range entries {
			name := e.Name()
			isHidden := strings.HasPrefix(name, ".")

			if !showHidden && isHidden {
				continue
			}

			if !e.IsDir() && extension != "" {
				if !strings.HasSuffix(strings.ToLower(name), extension) {
					continue
				}
			}

			info, err := e.Info()
			if err == nil {
				sb.WriteString(formatInfo(name, info))
				count++
			}
		}
	}

	if count == 0 {
		sb.WriteString("📭 Bu dizinde kriterlere uygun dosya bulunamadı.\n")
	} else {
		sb.WriteString(fmt.Sprintf("\n✅ Toplam %d dosya/klasör başarıyla listelendi.\n", count))
	}

	return sb.String(), nil
}

// Byte cinsinden boyutu MB, KB olarak okunabilir hale getirir
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
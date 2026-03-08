package coding

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

type ToolDeleter struct {
	WorkspaceDir string
}

func NewDeleter(workspaceDir string) *ToolDeleter {
	return &ToolDeleter{WorkspaceDir: workspaceDir}
}

func (d *ToolDeleter) Name() string { return "delete_python_tool" }

func (d *ToolDeleter) Description() string {
	return "Gereksiz, hatalı veya artık kullanılmayan bir Python aracını sistemden (hem diskten hem de veritabanından) TAMAMEN VE KALICI OLARAK siler."
}

func (d *ToolDeleter) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"filename": map[string]interface{}{"type": "string", "description": "Silinecek dosya adı (Örn: script.py)."},
		},
		"required": []string{"filename"},
	}
}

func (d *ToolDeleter) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	// 1. Esnek Parametre Yakalama
	filename, ok := args["filename"].(string)
	if !ok || strings.TrimSpace(filename) == "" {
		filename, _ = args["name"].(string)
	}

	filename = strings.TrimSpace(filename)
	if filename == "" {
		return "", fmt.Errorf("HATA: 'filename' parametresi eksik! Neyi sileceğimi belirtmelisin.")
	}

	if !strings.HasSuffix(filename, ".py") { 
		filename += ".py" 
	}

	// 🛡️ GÜVENLİK ZIRHI: Dizin Atlama (Path Traversal) Saldırılarını Önle
	// Kullanıcı veya LLM "../../gizli_dosya.py" gönderse bile sadece "gizli_dosya.py" kısmını alır.
	filename = filepath.Base(filename)
	fullPath := filepath.Join(d.WorkspaceDir, filename)

	// 2. Diskteki Dosyayı Sil
	if err := os.Remove(fullPath); err != nil {
		if os.IsNotExist(err) {
			logger.Warn("⚠️ Dosya diskte bulunamadı ama veritabanında kalmış olabilir, temizliğe devam ediliyor: %s", filename)
		} else {
			return "", fmt.Errorf("Dosya silinemedi: %v", err)
		}
	} else {
		logger.Action("🗑️ Dosya diskten silindi: %s", filename)
	}
	
	// 3. Veritabanından (pars_core.db) Sil
	toolName := strings.TrimSuffix(filename, ".py")
	err := RemoveToolFromDB(d.WorkspaceDir, toolName)
	if err != nil {
		logger.Warn("⚠️ Araç DB'den silinirken bir sorun oluştu (zaten silinmiş olabilir): %v", err)
	}

	return fmt.Sprintf("✅ BAŞARILI: '%s' sistemden (disk ve veritabanı) tamamen silindi.", filename), nil
}
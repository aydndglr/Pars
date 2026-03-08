package coding

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

// ToolEditor, mevcut bir Python aracını (tool) güvenli bir şekilde düzenlemek,
// belirli parçalarını değiştirmek veya baştan yazmak için kullanılan cerrahi modüldür.
// Artık 'uv' tabanlı ışık hızında bağımlılık yönetimi kullanır.
type ToolEditor struct {
	WorkspaceDir string
	PipPath      string // Geriye dönük uyumluluk için tutuluyor, ancak artık 'uv' kullanılıyor.
	PythonPath   string // Geriye dönük uyumluluk için tutuluyor, ancak artık 'uv' kullanılıyor.
	UvPath       string // ⚡ Mutlak 'uv' yolu, dış dünya bağımlılığını keser.
}

// NewEditor, belirtilen çalışma alanı ile yeni bir düzenleyici başlatır.
func NewEditor(workspaceDir, pipPath, pythonPath, uvPath string) *ToolEditor {
	return &ToolEditor{
		WorkspaceDir: workspaceDir,
		PipPath:      pipPath,
		PythonPath:   pythonPath,
		UvPath:       uvPath,
	}
}

func (e *ToolEditor) Name() string { return "edit_python_tool" }

func (e *ToolEditor) Description() string {
	return "Mevcut bir Python aracını GÜVENLİ ŞEKİLDE günceller. Hata çıkarsa otomatik rollback (geri alma) yapar. 'replace' ile küçük değişiklikler, 'write' ile baştan yazma yapabilirsin. Gerekirse 'install' ile paket ekle ve kesinlikle ÇALIŞTIR (run)."
}

// Parameters, LLM'in düzenleme işlemlerini nasıl göndereceğini tanımlayan şemadır.
func (e *ToolEditor) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"filename": map[string]interface{}{
				"type":        "string",
				"description": "Düzenlenecek mevcut dosya adı (örn: word_counter.py)",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Aracın veritabanında saklanacak güncel açıklaması.",
			},
			"actions": map[string]interface{}{
				"type":        "array",
				"description": "Sırasıyla yapılacak güncelleme adımları. ZORUNLUDUR.",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"step":            map[string]interface{}{"type": "string", "enum": []string{"write", "replace", "install", "run"}},
						"code":            map[string]interface{}{"type": "string", "description": "[WRITE] GÜNCEL Python kodunun TAMAMI."},
						"search_text":     map[string]interface{}{"type": "string", "description": "[REPLACE] Değiştirilecek olan MEVCUT kod parçası."},
						"replace_text":    map[string]interface{}{"type": "string", "description": "[REPLACE] 'search_text' yerine yazılacak YENİ kod parçası."},
						"tool_parameters": map[string]interface{}{"type": "object", "description": "[WRITE/REPLACE] Aracın argümanları değiştiyse YENİ kullanım şemasını JSON olarak ver."},
						"packages":        map[string]interface{}{"type": "string", "description": "[INSTALL] Yeni eklenecek veya güncellenecek paket adları (Örn: 'requests pandas'). uv ile anında izole edilir."},
						"command":         map[string]interface{}{"type": "string", "description": "[RUN] Terminal test komutu (Örn: 'python word_counter.py'). Sistem bunu otomatik olarak uv run komutuna çevirir."},
						"timeout_sec":     map[string]interface{}{"type": "number", "description": "[RUN] Testin maksimum çalışma süresi (Saniye). Varsayılan: 180"},
						"env":             map[string]interface{}{"type": "object", "description": "[RUN] Çalışma ortamı için çevresel değişkenler (Environment Variables)."},
					},
					"required": []string{"step"},
				},
			},
		},
		"required": []string{"filename", "actions"},
	}
}

// secureCheckPython, düzenlenmiş kodu diske yazmadan önce AST üzerinden zararlı
// sistem erişimleri (arka kapılar) barındırıp barındırmadığını test eder.
func (e *ToolEditor) secureCheckPython(ctx context.Context, code string) error {
	systemTools := []string{
		"browser", "sys_exec", "fs_read", "fs_list", "fs_write",
		"fs_delete", "dev_studio", "edit_python_tool", "delete_python_tool", "pars_control", "delegate_task",
	}
	forbiddenStr := strings.Join(systemTools, ",")

	// 🚀 GELİŞMİŞ AST ZIRHI
	checkerScript := `
import ast, sys
forbidden = set(sys.argv[1].split(','))
try:
    source = sys.stdin.read()
    tree = ast.parse(source)
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            for alias in node.names:
                if alias.name.split('.')[0] in forbidden: sys.exit(f"🚨 YASAKLI KÜTÜPHANE: {alias.name}")
        elif isinstance(node, ast.ImportFrom):
            if node.module and node.module.split('.')[0] in forbidden: sys.exit(f"🚨 YASAKLI KÜTÜPHANE: {node.module}")
        elif isinstance(node, ast.Call):
            if isinstance(node.func, ast.Name) and node.func.id in ('__import__', 'eval', 'exec', 'globals', 'locals'):
                sys.exit(f"🚨 GÜVENLİK İHLALİ: '{node.func.id}' sandbox koruması nedeniyle kullanılamaz.")
            if isinstance(node.func, ast.Name) and node.func.id == 'getattr':
                 sys.exit(f"🚨 GÜVENLİK İHLALİ: 'getattr' ile dinamik metod çağrımı yasaktır.")
    sys.exit(0)
except SyntaxError as e:
    sys.exit(f"🚨 SYNTAX HATASI: {e}")
except Exception as e:
    sys.exit(f"🚨 BEKLENMEYEN HATA: {e}")
`
	chkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// ⚡ HANTAL PYTHON YERİNE IŞIK HIZINDA UV ÇAĞRISI (MUTLAK YOL İLE)
	cmd := exec.CommandContext(chkCtx, e.UvPath, "run", "python", "-c", checkerScript, forbiddenStr)
	cmd.Stdin = strings.NewReader(code)
	out, err := cmd.CombinedOutput()
	
	if err != nil {
		if strings.Contains(err.Error(), "executable file not found") {
			return fmt.Errorf("KRİTİK HATA: Sistemde izole 'uv' paketi bulunamadı! Yol: %s", e.UvPath)
		}
		return fmt.Errorf(strings.TrimSpace(string(out)))
	}
	return nil
}

// Execute, belirtilen güncelleme eylemlerini (actions) sırasıyla yürütür ve
// hata durumunda sistemi son çalışan yedek versiyona döndürür (Rollback).
func (e *ToolEditor) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	filename, ok := args["filename"].(string)
	if !ok || filename == "" {
		return "", fmt.Errorf("HATA: 'filename' parametresi eksik! Hangi dosyayı düzenleyeceğini belirtmelisin.")
	}

	actionsRaw, ok := args["actions"].([]interface{})
	if !ok {
		return "", fmt.Errorf("HATA: 'edit_python_tool' için 'actions' dizisi (array) gereklidir.")
	}

	if !strings.HasSuffix(filename, ".py") {
		filename += ".py"
	}
	filename = filepath.Base(filename)
	fullPath := filepath.Join(e.WorkspaceDir, filename)

	// 1. DOSYA VARLIK KONTROLÜ
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return "", fmt.Errorf("HATA: '%s' adında bir dosya yok. Sıfırdan araç yapmak için 'dev_studio' kullanmalısın.", filename)
	}

	// ==========================================
	// 🛡️ YEDEKLEME (BACKUP) SİSTEMİ
	// ==========================================
	backupCode, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("Güvenlik hatası: Mevcut dosyanın yedeği alınamadı: %v", err)
	}
	logger.Action("🛡️ [%s] Yedek (Backup) hafızaya alındı. Ameliyat başlıyor...", filename)

	var report strings.Builder
	report.WriteString(fmt.Sprintf("🛠️ PARS GÜVENLİ GÜNCELLEME (UV EDITION) RAPORU\n%s\n", strings.Repeat("=", 40)))

	var updatedToolParams map[string]interface{}
	var uvWithArgs string
	var finalInstallPackages string

	// 2. MAKRO ADIMLARINI İŞLE
	for i, actRaw := range actionsRaw {
		act, ok := actRaw.(map[string]interface{})
		if !ok {
			continue
		}

		step, _ := act["step"].(string)

		switch step {
		case "replace":
			searchText, _ := act["search_text"].(string)
			replaceTextRaw, _ := act["replace_text"].(string)

			if tp, ok := act["tool_parameters"].(map[string]interface{}); ok {
				updatedToolParams = tp
			}

			if searchText == "" {
				e.rollback(fullPath, backupCode)
				return "", fmt.Errorf("Adım %d [replace]: 'search_text' boş olamaz. Rollback yapıldı.", i+1)
			}

			replaceText := cleanMarkdown(replaceTextRaw)
			currentContent, _ := os.ReadFile(fullPath)
			contentStr := string(currentContent)

			if !strings.Contains(contentStr, searchText) {
				e.rollback(fullPath, backupCode)
				return "", fmt.Errorf("Adım %d [replace] HATA: 'search_text' dosya içinde bulunamadı. Tam olarak eşleştiğinden emin ol.", i+1)
			}

			newContent := strings.Replace(contentStr, searchText, replaceText, 1)

			if err := e.secureCheckPython(ctx, newContent); err != nil {
				e.rollback(fullPath, backupCode)
				return fmt.Sprintf("🚨 GÜVENLİK İHLALİ VEYA SYNTAX HATASI YAKALANDI (ROLLBACK YAPILDI)!\n%v", err), nil
			}

			logger.Action("📝 [%d] Kod noktası '%s' içinde değiştiriliyor...", i+1, filename)
			if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
				e.rollback(fullPath, backupCode)
				return "", fmt.Errorf("Yazma hatası. Rollback yapıldı: %v", err)
			}

			report.WriteString(fmt.Sprintf("✅ Adım %d [replace]: Belirtilen kod parçası başarıyla değiştirildi ve AST testini geçti.\n", i+1))

		case "write":
			rawCode, _ := act["code"].(string)
			
			if tp, ok := act["tool_parameters"].(map[string]interface{}); ok {
				updatedToolParams = tp
			}

			if rawCode == "" {
				e.rollback(fullPath, backupCode)
				return "", fmt.Errorf("Adım %d [write]: Kod içeriği boş. Rollback yapıldı.", i+1)
			}

			code := cleanMarkdown(rawCode)

			if err := e.secureCheckPython(ctx, code); err != nil {
				e.rollback(fullPath, backupCode)
				return fmt.Sprintf("🚨 GÜVENLİK İHLALİ VEYA SYNTAX HATASI YAKALANDI (ROLLBACK YAPILDI)!\n%v", err), nil
			}

			finalCode := formatPythonCode(filename, "Güncellenmiş Otonom Araç", code)

			logger.Action("📝 [%d] Yeni kod '%s' üzerine zırhlanarak yazılıyor...", i+1, filename)
			if err := os.WriteFile(fullPath, []byte(finalCode), 0644); err != nil {
				e.rollback(fullPath, backupCode)
				return "", fmt.Errorf("Yazma hatası. Rollback yapıldı: %v", err)
			}

			report.WriteString(fmt.Sprintf("✅ Adım %d [write]: Yeni kod diske güvenle yazıldı ve AST testini geçti.\n", i+1))

		case "install":
			packages, _ := act["packages"].(string)
			if packages == "" {
				continue
			}

			// 🛡️ Paket Güvenliği
			systemTools := []string{"browser", "sys_exec", "dev_studio", "edit_python_tool", "delete_python_tool", "pars_control"}
			pkgList := strings.Fields(packages)
			var withFlags []string

			for _, pkg := range pkgList {
				for _, tool := range systemTools {
					if pkg == tool {
						errStr := fmt.Sprintf("🚨 KURAL İHLALİ: '%s' bir Go SİSTEM ARACIDIR, PyPI kütüphanesi DEĞİLDİR!", tool)
						e.rollback(fullPath, backupCode)
						return report.String(), fmt.Errorf(errStr)
					}
				}
				withFlags = append(withFlags, "--with", pkg)
			}

			uvWithArgs = strings.Join(withFlags, " ")
			finalInstallPackages = packages
			logger.Action("📦 [%d] UV paketleri hazırlandı: %s", i+1, packages)
			report.WriteString(fmt.Sprintf("✅ Adım %d [install]: Paketler (%s) hayalet venv için ayarlandı.\n", i+1, packages))

		case "run":
			command, _ := act["command"].(string)
			if command == "" {
				continue
			}

			timeoutSec := 180 // ⚡ Varsayılan 180 saniyeye çıkarıldı
			if ts, ok := act["timeout_sec"].(float64); ok && ts > 0 {
				timeoutSec = int(ts)
			}

			// ⚡ Otonom UV Komutu Enjeksiyonu (Mutlak Yol ve Tırnak Koruması ile)
			uvExec := fmt.Sprintf("\"%s\"", e.UvPath)
			finalRunCmd := command
			
			if strings.HasPrefix(finalRunCmd, "uv ") {
				finalRunCmd = strings.Replace(finalRunCmd, "uv ", uvExec+" ", 1)
			} else if strings.HasPrefix(finalRunCmd, "python ") {
				if uvWithArgs != "" {
					finalRunCmd = strings.Replace(finalRunCmd, "python ", uvExec+" run "+uvWithArgs+" python ", 1)
				} else {
					finalRunCmd = strings.Replace(finalRunCmd, "python ", uvExec+" run python ", 1)
				}
			} else {
				if uvWithArgs != "" {
					finalRunCmd = uvExec + " run " + uvWithArgs + " " + finalRunCmd
				} else {
					finalRunCmd = uvExec + " run " + finalRunCmd
				}
			}

			logger.Action("🧪 [%d] Güncellenmiş kod test ediliyor: %s (Timeout: %d sn)", i+1, finalRunCmd, timeoutSec)

			runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
			
			var cmd *exec.Cmd
			if runtime.GOOS == "windows" {
				cmd = exec.CommandContext(runCtx, "cmd", "/c", finalRunCmd)
			} else {
				cmd = exec.CommandContext(runCtx, "sh", "-c", finalRunCmd)
			}
			
			// VENV yolu enjeksiyonu kaldırıldı, UV kendi işini kendisi halleder. Sadece Environment Variable'lar eklenir.
			cmd.Env = os.Environ()
			if envMap, ok := act["env"].(map[string]interface{}); ok {
				for k, v := range envMap {
					cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%v", k, v))
				}
			}

			cmd.Dir = e.WorkspaceDir

			out, err := cmd.CombinedOutput()
			cancel()

			outputStr := strings.TrimSpace(string(out))

			// ==========================================
			// 🚨 TEST BAŞARISIZ -> ROLLBACK VE ANTI-DÖNGÜ
			// ==========================================
			if err != nil {
				e.rollback(fullPath, backupCode)

				if len(outputStr) > 1000 {
					outputStr = "...(önceki loglar)...\n" + outputStr[len(outputStr)-1000:]
				}

				systemPrompt := fmt.Sprintf(`🚨 GÜNCELLEME TESTİ BAŞARISIZ (ROLLBACK YAPILDI)!
Yazdığın yeni kod (%s) çalışırken çöktüğü için ESKİ ÇALIŞAN KODA geri dönüldü.

💻 Go Sistem Hatası: %v
💻 Terminal Hata Çıktısı:
%s

🧠 [SİSTEM YÖNERGESİ - KRİTİK]:
1. Hata mesajını dikkatlice oku. Sorun syntax'ta mı yoksa timeout mu yedin?
2. Çözümden %%100 emin olduktan sonra 'edit_python_tool' aracını tekrar kullan!`, filename, err, outputStr)

				report.WriteString(fmt.Sprintf("❌ GÜNCELLEME PATLADI. Rollback devrede. Go Hatası: %v\n", err))
				return systemPrompt, nil
			}

			// ==========================================
			// 🚀 BAŞARILI İSE VERİTABANI KAYDINI GÜNCELLE
			// ==========================================
			desc, _ := args["description"].(string)
			if desc == "" {
				desc = "Pars ToolEditor ile revize edilmiş otonom araç."
			}
			
			toolName := strings.TrimSuffix(filename, ".py")
			scriptPath := filepath.Join(e.WorkspaceDir, filename)
			
			// Güncellenmiş parametrelerle DB'ye tekrar yaz (UV Paketleri 'instructions' alanına yazılıyor)
			err = RegisterToolToDB(e.WorkspaceDir, toolName, "pars", desc, scriptPath, updatedToolParams, false, finalInstallPackages)
			if err != nil {
				logger.Error("❌ Güncellenen araç DB'ye yazılamadı: %v", err)
			} else {
				logger.Success("🎯 Yetenek Veritabanında Güncellendi: %s", toolName)
			}

			report.WriteString(fmt.Sprintf("✅ Adım %d [run]: Test Başarılı.\nTerminal Çıktısı:\n%s\n", i+1, outputStr))
		}
	}

	report.WriteString(strings.Repeat("-", 40) + "\n🏁 Araç Başarıyla Güncellendi ve Testi Geçti!")
	logger.Success("✏️ %s başarıyla revize edildi.", filename)
	return report.String(), nil
}

// rollback, hatalı bir güncelleme (syntax hatası, test çökmesi) durumunda
// hafızaya alınmış olan eski çalışan kodu diske geri yazar.
func (e *ToolEditor) rollback(path string, backup []byte) {
	logger.Warn("⚠️ ROLLBACK TETİKLENDİ: %s eski çalışan haline döndürülüyor.", filepath.Base(path))
	os.WriteFile(path, backup, 0644)
}
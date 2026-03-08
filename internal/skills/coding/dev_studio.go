package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

// DevStudioTool, Pars'ın kendi kendine Python kodu yazmasını, gerekli kütüphaneleri
// 'uv' (Hayalet Venv) ile anında kurmasını ve bu kodları test ederek veritabanına yeni bir yetenek olarak eklemesini sağlar.
type DevStudioTool struct {
	WorkspaceDir string
	PipPath      string // Geriye dönük uyumluluk için tutuluyor, ancak artık 'uv' kullanılıyor.
	PythonPath   string // Geriye dönük uyumluluk için tutuluyor, ancak artık 'uv' kullanılıyor.
	UvPath       string // ⚡ Mutlak 'uv' yolu, dış dünya bağımlılığını keser.
}

// NewDevStudio, çalışma alanı ve izole Python yolları ile yeni bir geliştirme ortamı oluşturur.
func NewDevStudio(workspaceDir, pipPath, pythonPath, uvPath string) *DevStudioTool {
	return &DevStudioTool{
		WorkspaceDir: workspaceDir,
		PipPath:      pipPath,
		PythonPath:   pythonPath,
		UvPath:       uvPath,
	}
}

func (t *DevStudioTool) Name() string { return "dev_studio" }

func (t *DevStudioTool) Description() string {
	return `
	OTONOM GELİŞTİRME ORTAMI (IDE). Sıfırdan Python kodu yazmak, kütüphane kurmak ve kodu GERÇEKTE çalıştırıp test etmek için kullan.
	🚨 KRİTİK KURAL: Bu aracı çağırırken ASLA Python kodu (örn: default_api.dev_studio()) yazma! Bu native bir araçtır, 
	sadece geçerli ve düzgün escape edilmiş (kaçış dizileri olan) bir JSON objesi göndererek çağır.
	`
}

// Parameters, aracın LLM tarafından anlaşılabilmesi için gerekli JSON şemasını tanımlar.
func (t *DevStudioTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"write_filename": map[string]interface{}{
				"type":        "string",
				"description": "[1. ADIM - ZORUNLU] Oluşturulacak dosya adı (Örn: tools/outlook_reader.py)",
			},
			"write_code": map[string]interface{}{
				"type":        "string",
				"description": "[1. ADIM - ZORUNLU] Python kodunun tamamı. 🚨 KURAL: JSON formatını bozmamak için kod içindeki yeni satırları '\\n', çift tırnakları '\\\"' olarak escape et.",
			},
			"tool_parameters_json": map[string]interface{}{
				"type":        "string",
				"description": "[1. ADIM - İSTEĞE BAĞLI] Aracın dışarıdan alacağı parametrelerin JSON şemasının STRING hali (Örn: '{\"limit\": {\"type\": \"integer\"}}').",
			},
			"install_packages": map[string]interface{}{
				"type":        "string",
				"description": "[2. ADIM - İSTEĞE BAĞLI] Kurulacak paketleri boşlukla ayırarak yaz (Örn: 'pandas requests'). Bu paketler 'uv' ile anında geçici (hayalet) ortama dahil edilir. Gerekmiyorsa boş bırak.",
			},
			"run_command": map[string]interface{}{
				"type":        "string",
				"description": "[3. ADIM - İSTEĞE BAĞLI] Kodu test etmek için terminal komutu (Örn: 'python tools/outlook_reader.py'). Sistem bunu otomatik olarak 'uv run' komutuna çevirecektir.",
			},
			"timeout_sec": map[string]interface{}{
				"type":        "integer",
				"description": "[3. ADIM - İSTEĞE BAĞLI] Test komutunun maksimum çalışma süresi (saniye cinsinden). Varsayılan 180 sn. Uzun sürecek ağ işlemlerinde bu süreyi artır.",
			},
		},
		"required": []string{"write_filename", "write_code"},
	}
}

// secureCheckPython, AST (Soyut Sözdizimi Ağacı) kullanarak Python kodunu çalıştırılmadan önce statik olarak analiz eder.
// Tehlikeli sistem çağrılarını, yasaklı içe aktarmaları ve dinamik kod çalıştırma hilelerini (eval, exec) engeller.
func (t *DevStudioTool) secureCheckPython(ctx context.Context, code string) error {
	systemTools := []string{
		"browser", "sys_exec", "fs_read", "fs_list", "fs_write",
		"fs_delete", "dev_studio", "edit_python_tool", "delete_python_tool", "pars_control", "delegate_task",
	}
	forbiddenStr := strings.Join(systemTools, ",")

	// 🚀 GELİŞMİŞ AST ZIRHI: Getattr ve builtins hileleri engellendi!
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
                sys.exit(f"🚨 GÜVENLİK İHLALİ: '{node.func.id}' fonksiyonu sandbox koruması nedeniyle kullanılamaz.")
            # getattr hilesini engelleme (örn: getattr(__builtins__, 'eval'))
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
	cmd := exec.CommandContext(chkCtx, t.UvPath, "run", "python", "-c", checkerScript, forbiddenStr)
	cmd.Stdin = strings.NewReader(code)
	out, err := cmd.CombinedOutput()
	
	if err != nil {
		if strings.Contains(err.Error(), "executable file not found") {
			return fmt.Errorf("KRİTİK HATA: Sistemde izole 'uv' paketi bulunamadı! Yol: %s", t.UvPath)
		}
		return fmt.Errorf(strings.TrimSpace(string(out)))
	}
	return nil
}

// cleanMarkdown, LLM'in kaçınılmaz olarak kodun etrafına sardığı markdown bloklarını temizler.
func cleanMarkdown(code string) string {
	code = strings.TrimSpace(code)
	code = strings.TrimPrefix(code, "```python")
	code = strings.TrimPrefix(code, "```")
	code = strings.TrimSuffix(code, "```")
	return strings.TrimSpace(code)
}

// Execute, otonom geliştirme döngüsünün 3 aşamasını (Yazma, UV Hazırlık, Test) tek bir zincirde yürütür.
func (t *DevStudioTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	var report strings.Builder
	report.WriteString(fmt.Sprintf("💻 PARS DEV STUDIO (UV EDITION) RAPORU\n%s\n", strings.Repeat("=", 35)))

	os.MkdirAll(t.WorkspaceDir, 0755)
	var lastToolParams map[string]interface{}

	// ==========================================
	// 🚀 ADIM 1: YAZMA (WRITE PHASE)
	// ==========================================
	writeFilename, _ := args["write_filename"].(string)
	writeCode, _ := args["write_code"].(string)

	if writeFilename == "" || writeCode == "" {
		return "", fmt.Errorf("HATA: 'write_filename' ve 'write_code' alanları zorunludur!")
	}

	if tpStr, ok := args["tool_parameters_json"].(string); ok && tpStr != "" {
		json.Unmarshal([]byte(tpStr), &lastToolParams)
	}

	code := cleanMarkdown(writeCode)

	if err := t.secureCheckPython(ctx, code); err != nil {
		errStr := fmt.Sprintf("KURAL İHLALİ VEYA SYNTAX HATASI:\n%v\nNot: Go sistem araçlarını Python içine import edemezsin.", err)
		report.WriteString(fmt.Sprintf("❌ Adım 1 [write]: %s\n", errStr))
		return report.String(), fmt.Errorf(errStr)
	}

	finalCode := formatPythonCode(writeFilename, "Otonom Araç", code)
	fullPath := filepath.Join(t.WorkspaceDir, filepath.Base(writeFilename))
	
	logger.Action("📝 [1] Zırhlı kod yazılıyor: %s", fullPath)

	if err := os.WriteFile(fullPath, []byte(finalCode), 0644); err != nil {
		return "", fmt.Errorf("Adım 1 [write] Dosya yazılamadı: %v", err)
	}
	report.WriteString(fmt.Sprintf("✅ Adım 1 [write]: %s AST Güvenlik Testinden geçti ve diske kaydedildi.\n", writeFilename))

	// ==========================================
	// 🚀 ADIM 2: UV HAYALET VENV HAZIRLIĞI
	// ==========================================
	installPackages, _ := args["install_packages"].(string)
	var uvWithArgs string

	if installPackages != "" {
		systemTools := []string{"browser", "dev_studio", "pars_control", "delegate_task"}
		pkgList := strings.Fields(installPackages)
		var withFlags []string

		for _, pkg := range pkgList {
			for _, tool := range systemTools {
				if pkg == tool {
					errStr := fmt.Sprintf("🚨 KURAL İHLALİ: '%s' bir Python kütüphanesi DEĞİLDİR!", tool)
					report.WriteString(fmt.Sprintf("❌ Adım 2 [uv_prep]: %s\n", errStr))
					return report.String(), fmt.Errorf(errStr)
				}
			}
			withFlags = append(withFlags, "--with", pkg)
		}
		
		uvWithArgs = strings.Join(withFlags, " ")
		logger.Info("⏳ [2] UV PAKETLERİ HAZIRLANDI: %s", installPackages)
		report.WriteString(fmt.Sprintf("✅ Adım 2 [uv_prep]: Paketler (%s) hayalet venv için ayarlandı.\n", installPackages))
	} else {
		report.WriteString("✅ Adım 2 [uv_prep]: Ekstra kütüphane gerekmedi.\n")
	}

	// ==========================================
	// 🚀 ADIM 3: TEST VE KAYIT (RUN PHASE)
	// ==========================================
	runCommand, _ := args["run_command"].(string)
	if runCommand != "" {
		timeoutSec := 180 // ⚡ Varsayılan süreyi 60'tan 180'e çıkardık (Ağ darboğazını engellemek için)
		if val, ok := args["timeout_sec"].(float64); ok {
			timeoutSec = int(val)
		}
		
		// ⚡ Otonom UV Komutu Enjeksiyonu (Mutlak Yol ve Tırnak Koruması ile)
		uvExec := fmt.Sprintf("\"%s\"", t.UvPath) // Boşluk içeren yollar için koruma
		finalRunCmd := runCommand
		
		if strings.HasPrefix(finalRunCmd, "uv ") {
			// Eğer komut 'uv ' ile başlıyorsa, bunu mutlak yol ile değiştirelim
			finalRunCmd = strings.Replace(finalRunCmd, "uv ", uvExec+" ", 1)
		} else if strings.HasPrefix(finalRunCmd, "python ") {
			if uvWithArgs != "" {
				finalRunCmd = strings.Replace(finalRunCmd, "python ", uvExec+" run "+uvWithArgs+" python ", 1)
			} else {
				finalRunCmd = strings.Replace(finalRunCmd, "python ", uvExec+" run python ", 1)
			}
		} else {
			// Ne uv ne python ise, başa ekle
			if uvWithArgs != "" {
				finalRunCmd = uvExec + " run " + uvWithArgs + " " + finalRunCmd
			} else {
				finalRunCmd = uvExec + " run " + finalRunCmd
			}
		}

		logger.Info("⏳ [3] TEST MOTORU ÇALIŞIYOR: '%s' (Timeout: %d sn)", finalRunCmd, timeoutSec)
		runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(runCtx, "cmd", "/c", finalRunCmd)
		} else {
			cmd = exec.CommandContext(runCtx, "sh", "-c", finalRunCmd)
		}

		cmd.Dir = t.WorkspaceDir

		out, err := cmd.CombinedOutput()
		cancel()

		outputStr := strings.TrimSpace(string(out))

		if err != nil {
			systemPrompt := fmt.Sprintf(`🚨 KOD TEST SIRASINDA ÇÖKTÜ!
Yazdığın kod çalışırken hata verdi.

💻 Go Sistem Durumu: %v
💻 Terminal Çıktısı (Traceback):
%s

🧠 [SİSTEM YÖNERGESİ]:
1. Hatayı incele. Eksik kütüphane mi, syntax mı, env eksiği mi?
2. 'edit_python_tool' aracını kullanarak dosyayı onar ve tekrar test et.`, err, outputStr)

			report.WriteString("❌ Adım 3 [run]: KOD ÇÖKTÜ.\n")
			return systemPrompt, nil
		}

		// 🚀 SQLITE KAYIT İŞLEMİ
		toolName := strings.TrimSuffix(filepath.Base(writeFilename), ".py")
		scriptPath := filepath.Join(t.WorkspaceDir, filepath.Base(writeFilename))
		desc := "Pars Dev Studio ile otonom olarak yazılmış ve test edilmiş araç."
		
		// Paketleri DB'ye kaydediyoruz ki runner (utils.go) aracı çalıştırırken "--with" parametrelerini bilsin.
		err = RegisterToolToDB(t.WorkspaceDir, toolName, "pars", desc, scriptPath, lastToolParams, false, installPackages)
		if err != nil {
			logger.Error("❌ Araç SQLite Veritabanına kaydedilemedi: %v", err)
		} else {
			logger.Success("🎯 Yeni Yetenek Veritabanına Eklendi: %s", toolName)
		}

		report.WriteString(fmt.Sprintf("✅ Adım 3 [run]: Başarılı.\nTerminal Çıktısı:\n%s\n", outputStr))
	}

	report.WriteString(strings.Repeat("-", 35) + "\n🏁 Dev Studio (UV) Makrosu başarıyla tamamlandı.")
	return report.String(), nil
}
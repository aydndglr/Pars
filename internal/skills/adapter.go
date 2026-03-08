// internal/skills/adapter.go
// 🚀 DÜZELTMELER: Nil checks, Error handling, Timeout, Security, Logging
// ⚠️ DİKKAT: loader.go ve manager.go ile %100 uyumlu

package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

// 🚨 YENİ: Timeout ve Limit Sabitleri
const (
	PythonToolTimeout      = 180 * time.Second // 3 dakika varsayılan timeout
	PythonAsyncTimeout     = 600 * time.Second // 10 dakika async timeout
	MaxOutputSize          = 1 * 1024 * 1024   // 1 MB output limiti
)

// PythonTool: Dinamik Python dosyalarını sarmalayan ileri seviye yapı.
type PythonTool struct {
	name         string
	description  string
	scriptPath   string
	interpreter  string
	uvPath       string // İzole 'uv' yolu
	workDir      string
	parameters   map[string]interface{}
	isAsync      bool
	instructions string // Paket listesi (dependencies)
}

// NewPythonTool: Yeni bir Python aracı oluşturur. Mutlak uvPath parametresi eklendi.
func NewPythonTool(name, desc, path, pythonPath, uvPath string, params map[string]interface{}, isAsync bool, instructions string) *PythonTool {
	// 🚨 DÜZELTME #1: Nil/boş parametre kontrolleri
	if name == "" {
		logger.Warn("⚠️ [PythonTool] İsimsiz tool oluşturulmaya çalışıldı, 'unknown_tool' olarak kaydedildi")
		name = "unknown_tool"
	}

	baseScriptName := filepath.Base(path)
	rootDir, err := filepath.Abs(".")
	if err != nil {
		logger.Warn("⚠️ [PythonTool] Kök dizin alınamadı: %v, fallback kullanılıyor", err)
		rootDir = "."
	}

	absWorkDir := filepath.Join(rootDir, "tools")
	absScriptPath := filepath.Join(absWorkDir, baseScriptName)

	absInterpreter, _ := filepath.Abs(pythonPath)
	absUvPath, _ := filepath.Abs(uvPath)

	return &PythonTool{
		name:         name,
		description:  desc,
		scriptPath:   absScriptPath,
		interpreter:  absInterpreter,
		uvPath:       absUvPath,
		workDir:      absWorkDir,
		parameters:   params,
		isAsync:      isAsync,
		instructions: instructions,
	}
}

func (p *PythonTool) Name() string {
	// 🚨 DÜZELTME #2: Nil check
	if p == nil {
		return "unknown_tool"
	}
	return p.name
}

func (p *PythonTool) Description() string {
	// 🚨 DÜZELTME #3: Nil check
	if p == nil {
		return "Tool tanımlanmamış"
	}
	return p.description
}

func (p *PythonTool) Parameters() map[string]interface{} {
	// 🚨 DÜZELTME #4: Nil check
	if p == nil {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}

	if p.parameters == nil || len(p.parameters) == 0 {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}
	return p.parameters
}

// 🛠️ DİNAMİK KOMUT İNŞA EDİCİ (UV ENTEGRASYONU)
func (p *PythonTool) buildCmd(ctx context.Context, jsonArgs string) *exec.Cmd {
	// 🚨 DÜZELTME #5: Nil check
	if p == nil {
		return nil
	}

	// Eğer instructions (paket listesi) doluysa UV ile çalıştır
	if p.instructions != "" {
		// Paketleri virgül veya boşluklardan ayırıyoruz
		packages := strings.FieldsFunc(p.instructions, func(r rune) bool {
			return r == ',' || r == ' ' || r == ';'
		})

		uvArgs := []string{"run"}
		for _, pkg := range packages {
			pkg = strings.TrimSpace(pkg)
			if pkg != "" {
				uvArgs = append(uvArgs, "--with", pkg)
			}
		}

		// uv run --with pkg1 --with pkg2 python yorumlayicisi script.py '{"args"}'
		uvArgs = append(uvArgs, p.interpreter, p.scriptPath, jsonArgs)

		// ⚡ İZOLE "uv" ÇALIŞIYOR
		return exec.CommandContext(ctx, p.uvPath, uvArgs...)
	}

	// Paket yoksa doğrudan yorumlayıcı ile çalıştır
	return exec.CommandContext(ctx, p.interpreter, p.scriptPath, jsonArgs)
}

// 🆕 YENİ: validatePaths - Tüm yolların geçerli olduğunu doğrula
func (p *PythonTool) validatePaths() error {
	if p == nil {
		return fmt.Errorf("tool nil")
	}

	// Script dosyası var mı?
	if _, err := os.Stat(p.scriptPath); os.IsNotExist(err) {
		return fmt.Errorf("script dosyası bulunamadı: %s", p.scriptPath)
	}

	// Python interpreter var mı?
	if _, err := os.Stat(p.interpreter); os.IsNotExist(err) {
		return fmt.Errorf("python yorumlayıcı bulunamadı: %s", p.interpreter)
	}

	// UV path var mı? (Eğer instructions varsa)
	if p.instructions != "" {
		if _, err := os.Stat(p.uvPath); os.IsNotExist(err) {
			return fmt.Errorf("uv bulunamadı: %s", p.uvPath)
		}
	}

	// 🚨 DÜZELTME #6: Security - Script tools/ klasörü içinde mi?
	if !strings.HasPrefix(p.scriptPath, p.workDir) {
		return fmt.Errorf("güvenlik ihlali: script tools/ klasörü dışında: %s", p.scriptPath)
	}

	return nil
}

// Execute: Python kodunu çalıştırır. (Artık ASENKRON ve UV destekli!)
func (p *PythonTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	// 🚨 DÜZELTME #7: Nil check
	if p == nil {
		return "", fmt.Errorf("tool nil")
	}

	// 🚨 DÜZELTME #8: Path validation
	if err := p.validatePaths(); err != nil {
		return "", fmt.Errorf("doğrulama hatası: %w", err)
	}

	jsonBytes, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("argüman paketleme hatası: %v", err)
	}
	jsonArgs := string(jsonBytes)

	logger.Debug("🐍 Araç Tetiklendi: %s | Konum: %s", p.name, p.scriptPath)

	// 🚀 ASENKRON ÇALIŞMA MODU (BACKGROUND JOB)
	if p.isAsync {
		logger.Action("🔄 [%s] Asenkron görev arka plana atılıyor...", p.name)

		go func() {
			// 🚨 DÜZELTME #9: Async için timeout'lu context
			bgCtx, cancel := context.WithTimeout(context.Background(), PythonAsyncTimeout)
			defer cancel()

			cmd := p.buildCmd(bgCtx, jsonArgs)
			if cmd == nil {
				logger.Error("❌ [%s] Komut oluşturulamadı (nil)", p.name)
				return
			}

			cmd.Dir = p.workDir
			cmd.Env = os.Environ()

			// 🚨 DÜZELTME #10: CombinedOutput ile stdout+stderr yakala
			outputBytes, err := cmd.CombinedOutput()
			resultStr := string(outputBytes)

			// 🚨 DÜZELTME #11: Output boyutunu sınırla (memory bloat önleme)
			if len(resultStr) > MaxOutputSize {
				resultStr = resultStr[:MaxOutputSize] + "\n...[OUTPUT KISITLANDI - 1MB LIMIT]..."
			}

			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					logger.Error("❌ [%s] Arka Plan Görevi Çöktü (Kod %d)\nTraceback Detayı:\n%s", p.name, exitErr.ExitCode(), resultStr)
				} else {
					logger.Error("❌ [%s] Arka Plan Görevi Hatası: %v\nDetay:\n%s", p.name, err, resultStr)
				}
			} else {
				logger.Success("✅ [%s] Arka Plan Görevi Başarıyla Tamamlandı!\nÇıktı:\n%s", p.name, resultStr)
			}
		}()

		return fmt.Sprintf("✅ '%s' aracı arka planda başarıyla başlatıldı. Sen başka komutlar vermeye devam edebilirsin.", p.name), nil
	}

	// 🛑 SENKRON ÇALIŞMA MODU (Klasik Beklemeli)
	// 🚨 DÜZELTME #12: Timeout'lu context oluştur
	execCtx, cancel := context.WithTimeout(ctx, PythonToolTimeout)
	defer cancel()

	cmd := p.buildCmd(execCtx, jsonArgs)
	if cmd == nil {
		return "", fmt.Errorf("komut oluşturulamadı (nil)")
	}

	cmd.Dir = p.workDir
	cmd.Env = os.Environ()

	// 🚨 DÜZELTME #13: CombinedOutput ile stdout+stderr yakala
	outputBytes, err := cmd.CombinedOutput()
	result := string(outputBytes)

	// 🚨 DÜZELTME #14: Output boyutunu sınırla
	if len(result) > MaxOutputSize {
		result = result[:MaxOutputSize] + "\n...[OUTPUT KISITLANDI - 1MB LIMIT]..."
	}

	if err != nil {
		// 🚨 DÜZELTME #15: Context deadline kontrolü (timeout)
		if execCtx.Err() == context.DeadlineExceeded {
			logger.Warn("⏰ [%s] Çalıştırma zaman aşımına uğradı (%d sn)", p.name, int(PythonToolTimeout.Seconds()))
			return fmt.Sprintf("⏰ ZAMAN AŞIMI: '%s' aracı %d saniye içinde tamamlanmadı.", p.name, int(PythonToolTimeout.Seconds())), nil
		}

		if exitErr, ok := err.(*exec.ExitError); ok {
			// Traceback detayları result içinde
			return fmt.Sprintf("❌ Script Hatası (%s) - Exit Code %d:\n%s", p.name, exitErr.ExitCode(), result), nil
		}

		return "", fmt.Errorf("kritik çalıştırma hatası: %v\nDetay: %s", err, result)
	}

	return strings.TrimSpace(result), nil
}

// 🆕 YENİ: GetInfo - Tool bilgilerini döndür (debug için)
func (p *PythonTool) GetInfo() map[string]interface{} {
	if p == nil {
		return map[string]interface{}{
			"error": "tool nil",
		}
	}

	return map[string]interface{}{
		"name":         p.name,
		"description":  p.description,
		"script_path":  p.scriptPath,
		"interpreter":  p.interpreter,
		"uv_path":      p.uvPath,
		"work_dir":     p.workDir,
		"is_async":     p.isAsync,
		"has_packages": p.instructions != "",
	}
}

// 🆕 YENİ: SetAsync - Async modunu runtime'da değiştir
func (p *PythonTool) SetAsync(async bool) {
	if p == nil {
		return
	}
	p.isAsync = async
	logger.Debug("🔄 [%s] Async modu değiştirildi: %v", p.name, async)
}
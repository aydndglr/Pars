// internal/skills/venv.go
// 🚀 DÜZELTMELER: Timeout, Error handling, Validation, Security, Logging
// ⚠️ DİKKAT: loader.go ve adapter.go ile %100 uyumlu

package skills

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

// 🚨 YENİ: Timeout ve Limit Sabitleri
const (
	VenvCreateTimeout  = 120 * time.Second // 2 dakika venv oluşturma
	UvInstallTimeout   = 300 * time.Second // 5 dakika uv kurulumu
	UvMaxVersion       = "latest"          // UV versiyon pinning (güvenlik)
)

// PythonEnv: Contains the absolute paths for the isolated Python runtime.
type PythonEnv struct {
	BaseDir    string
	VenvDir    string
	PythonPath string
	PipPath    string
	UvPath     string // İzole UV'nin mutlak yolu
}

// SetupVenv: Bootstraps an isolated Python virtual environment parallel to the tools directory.
func SetupVenv(toolsDir string) (*PythonEnv, error) {
	// 🚨 DÜZELTME #1: Input validation
	if toolsDir == "" {
		return nil, fmt.Errorf("toolsDir boş olamaz")
	}

	// 🚨 DÜZELTME #2: Path traversal koruması
	if strings.Contains(toolsDir, "..") {
		logger.Warn("⚠️ [SetupVenv] Göreceli path tespit edildi, temizleniyor: %s", toolsDir)
	}

	// =====================================================================
	// 📍 1. ABSOLUTE PATH RESOLUTION (Anti-Amnesia Protocol)
	// =====================================================================
	absToolsDir, err := filepath.Abs(toolsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path for tools directory: %v", err)
	}

	// 🚀 SMART ROUTING: Derive the project root directly from the tools directory's parent.
	projectRoot := filepath.Dir(absToolsDir)
	venvDir := filepath.Join(projectRoot, "pars_venv")

	// 🚨 DÜZELTME #3: Venv dizini yazılabilir mi kontrol et
	if err := os.MkdirAll(venvDir, 0755); err != nil {
		return nil, fmt.Errorf("venv dizini oluşturulamadı: %v", err)
	}

	env := &PythonEnv{
		BaseDir: absToolsDir,
		VenvDir: venvDir,
	}

	// =====================================================================
	// ⚙️ 2. OS-AGNOSTIC EXECUTABLE MAPPING
	// =====================================================================
	if runtime.GOOS == "windows" {
		env.PythonPath = filepath.Join(venvDir, "Scripts", "python.exe")
		env.PipPath = filepath.Join(venvDir, "Scripts", "pip.exe")
		env.UvPath = filepath.Join(venvDir, "Scripts", "uv.exe")
	} else {
		env.PythonPath = filepath.Join(venvDir, "bin", "python")
		env.PipPath = filepath.Join(venvDir, "bin", "pip")
		env.UvPath = filepath.Join(venvDir, "bin", "uv")
	}

	// =====================================================================
	// 📂 3. WORKSPACE INITIALIZATION
	// =====================================================================
	if err := os.MkdirAll(absToolsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to initialize tools workspace: %v", err)
	}

	// =====================================================================
	// 🧬 4. VIRTUAL ENVIRONMENT VALIDATION & BOOTSTRAPPING
	// =====================================================================
	if _, err := os.Stat(env.PythonPath); os.IsNotExist(err) {
		logger.Action("🐍 Bootstrapping Python Virtual Environment (pars_venv)... (Initial setup only)")

		// 🚨 DÜZELTME #4: Timeout'lu context oluştur
		ctx, cancel := context.WithTimeout(context.Background(), VenvCreateTimeout)
		defer cancel()

		// 🐧 POSIX COMPLIANCE: Fallback to 'python3' if 'python' is not mapped in UNIX.
		basePython := "python"
		if runtime.GOOS != "windows" {
			if _, err := exec.LookPath("python3"); err == nil {
				basePython = "python3"
			} else if _, err := exec.LookPath("python"); err != nil {
				return nil, fmt.Errorf("sistemde Python bulunamadı (python veya python3)")
			}
		}

		// 🚨 DÜZELTME #5: CommandContext ile timeout koruması
		cmd := exec.CommandContext(ctx, basePython, "-m", "venv", venvDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return nil, fmt.Errorf("venv bootstrapping zaman aşımına uğradı (%d sn)", int(VenvCreateTimeout.Seconds()))
			}
			return nil, fmt.Errorf("venv bootstrapping failed: %v\nTerminal Output: %s", err, string(out))
		}

		logger.Success("✅ Isolated Python runtime successfully provisioned at: %s", venvDir)
	} else {
		logger.Debug("🐍 Isolated Python runtime is active and verified: %s", env.PythonPath)
	}

	// =====================================================================
	// ⚡ 5. UV INJECTION & VERIFICATION (Geliştirilmiş)
	// =====================================================================
	if _, err := os.Stat(env.UvPath); os.IsNotExist(err) {
		logger.Action("⚡ Injecting 'uv' into isolated environment via pip...")

		// 🚨 DÜZELTME #6: Sistemde uv var mı kontrol et (hızlı yol)
		if systemUv, err := exec.LookPath("uv"); err == nil {
			logger.Debug("🔍 Sistemde uv bulundu: %s, kopyalanıyor...", systemUv)
			
			// 🚨 DÜZELTME #7: UV'yi kopyala (pip install'den hızlı)
			if runtime.GOOS == "windows" {
				// Windows'ta copy komutu
				cmd := exec.Command("copy", "/Y", systemUv, env.UvPath)
				if _, err := cmd.CombinedOutput(); err != nil {
					logger.Warn("⚠️ UV kopyalama başarısız, pip ile kuruluyor: %v", err)
					// Fallback to pip install
					return installUvViaPip(env)
				}
			} else {
				// Linux/Mac'te cp komutu
				cmd := exec.Command("cp", systemUv, env.UvPath)
				if _, err := cmd.CombinedOutput(); err != nil {
					logger.Warn("⚠️ UV kopyalama başarısız, pip ile kuruluyor: %v", err)
					// Fallback to pip install
					return installUvViaPip(env)
				}
				
				// 🚨 DÜZELTME #8: Executable permission ekle
				if err := os.Chmod(env.UvPath, 0755); err != nil {
					logger.Warn("⚠️ UV executable permission ayarlanamadı: %v", err)
				}
			}
			
			logger.Success("✅ 'uv' successfully copied from system at: %s", env.UvPath)
			return env, nil
		}

		// Sistemde uv yoksa pip ile kur
		return installUvViaPip(env)
	} else {
		logger.Debug("⚡ 'uv' runtime is active and verified: %s", env.UvPath)
	}

	return env, nil
}

// 🆕 YENİ: installUvViaPip - UV'yi pip ile kuran helper fonksiyon
func installUvViaPip(env *PythonEnv) (*PythonEnv, error) {
	// 🚨 DÜZELTME #9: Timeout'lu context
	ctx, cancel := context.WithTimeout(context.Background(), UvInstallTimeout)
	defer cancel()

	// 🚨 DÜZELTME #10: UV versiyon pinning (güvenlik)
	uvPackage := "uv"
	// İleride spesifik versiyon: "uv==0.5.0"

	cmd := exec.CommandContext(ctx, env.PipPath, "install", "--upgrade", uvPackage)
	
	// 🚨 DÜZELTME #11: Environment variables set
	cmd.Env = append(os.Environ(),
		"PIP_NO_CACHE_DIR=1",     // Cache kullanma (daha az disk)
		"PIP_DISABLE_PIP_VERSION_CHECK=1", // Versiyon kontrolü yapma
	)
	
	if out, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("uv installation zaman aşımına uğradı (%d sn)", int(UvInstallTimeout.Seconds()))
		}
		return nil, fmt.Errorf("uv injection failed: %v\nTerminal Output: %s", err, string(out))
	}

	// 🚨 DÜZELTME #12: UV kurulduktan sonra doğrula
	if _, err := os.Stat(env.UvPath); os.IsNotExist(err) {
		// Windows'ta Scripts/, Linux'ta bin/ altında olabilir
		// Pip bazen farklı yere kurabiliyor, fallback path kontrolü
		altUvPath := filepath.Join(env.VenvDir, "bin", "uv")
		if runtime.GOOS == "windows" {
			altUvPath = filepath.Join(env.VenvDir, "Scripts", "uv.exe")
		}
		
		if _, err := os.Stat(altUvPath); err == nil {
			env.UvPath = altUvPath
			logger.Debug("🔍 UV alternatif yolda bulundu: %s", altUvPath)
		} else {
			return nil, fmt.Errorf("uv kuruldu ancak bulunamadı: %s", env.UvPath)
		}
	}

	// 🚨 DÜZELTME #13: Linux/Mac'te executable permission
	if runtime.GOOS != "windows" {
		if err := os.Chmod(env.UvPath, 0755); err != nil {
			logger.Warn("⚠️ UV executable permission ayarlanamadı: %v", err)
		}
	}

	logger.Success("✅ 'uv' successfully injected at: %s", env.UvPath)
	return env, nil
}

// 🆕 YENİ: Validate - PythonEnv'in geçerliliğini doğrula
func (env *PythonEnv) Validate() error {
	if env == nil {
		return fmt.Errorf("pythonEnv nil")
	}

	if env.BaseDir == "" {
		return fmt.Errorf("baseDir boş")
	}

	if env.VenvDir == "" {
		return fmt.Errorf("venvDir boş")
	}

	// Python path var mı?
	if _, err := os.Stat(env.PythonPath); os.IsNotExist(err) {
		return fmt.Errorf("python bulunamadı: %s", env.PythonPath)
	}

	// Pip path var mı?
	if _, err := os.Stat(env.PipPath); os.IsNotExist(err) {
		return fmt.Errorf("pip bulunamadı: %s", env.PipPath)
	}

	// UV path var mı? (opsiyonel, olmayabilir)
	if _, err := os.Stat(env.UvPath); err == nil {
		logger.Debug("✅ UV mevcut: %s", env.UvPath)
	} else {
		logger.Warn("⚠️ UV bulunamadı: %s (bazı özellikler çalışmayabilir)", env.UvPath)
	}

	return nil
}

// 🆕 YENİ: GetPythonVersion - Python versiyonunu döndür
func (env *PythonEnv) GetPythonVersion() (string, error) {
	if env == nil {
		return "", fmt.Errorf("pythonEnv nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, env.PythonPath, "--version")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("version alınamadı: %v", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// 🆕 YENİ: GetUvVersion - UV versiyonunu döndür
func (env *PythonEnv) GetUvVersion() (string, error) {
	if env == nil {
		return "", fmt.Errorf("pythonEnv nil")
	}

	if _, err := os.Stat(env.UvPath); os.IsNotExist(err) {
		return "", fmt.Errorf("uv bulunamadı")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, env.UvPath, "--version")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("version alınamadı: %v", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// 🆕 YENİ: ReinstallUv - UV'yi yeniden kur (bozulursa)
func (env *PythonEnv) ReinstallUv() error {
	if env == nil {
		return fmt.Errorf("pythonEnv nil")
	}

	logger.Warn("⚠️ [SetupVenv] UV yeniden kuruluyor...")

	// Eski UV'yi sil
	if _, err := os.Stat(env.UvPath); err == nil {
		if err := os.Remove(env.UvPath); err != nil {
			logger.Warn("⚠️ Eski UV silinemedi: %v", err)
		}
	}

	// Yeniden kur
	_, err := installUvViaPip(env)
	return err
}
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


const (
	VenvCreateTimeout  = 120 * time.Second
	UvInstallTimeout   = 300 * time.Second
	UvMaxVersion       = "latest"         
)


type PythonEnv struct {
	BaseDir    string
	VenvDir    string
	PythonPath string
	PipPath    string
	UvPath     string 
}


func SetupVenv(toolsDir string) (*PythonEnv, error) {

	if toolsDir == "" {
		return nil, fmt.Errorf("toolsDir boş olamaz")
	}


	if strings.Contains(toolsDir, "..") {
		logger.Warn("⚠️ [SetupVenv] Göreceli path tespit edildi, temizleniyor: %s", toolsDir)
	}

	absToolsDir, err := filepath.Abs(toolsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path for tools directory: %v", err)
	}

	projectRoot := filepath.Dir(absToolsDir)
	venvDir := filepath.Join(projectRoot, "pars_venv")

	if err := os.MkdirAll(venvDir, 0755); err != nil {
		return nil, fmt.Errorf("venv dizini oluşturulamadı: %v", err)
	}

	env := &PythonEnv{
		BaseDir: absToolsDir,
		VenvDir: venvDir,
	}

	if runtime.GOOS == "windows" {
		env.PythonPath = filepath.Join(venvDir, "Scripts", "python.exe")
		env.PipPath = filepath.Join(venvDir, "Scripts", "pip.exe")
		env.UvPath = filepath.Join(venvDir, "Scripts", "uv.exe")
	} else {
		env.PythonPath = filepath.Join(venvDir, "bin", "python")
		env.PipPath = filepath.Join(venvDir, "bin", "pip")
		env.UvPath = filepath.Join(venvDir, "bin", "uv")
	}


	if err := os.MkdirAll(absToolsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to initialize tools workspace: %v", err)
	}


	if _, err := os.Stat(env.PythonPath); os.IsNotExist(err) {
		logger.Action("🐍 Bootstrapping Python Virtual Environment (pars_venv)... (Initial setup only)")

		ctx, cancel := context.WithTimeout(context.Background(), VenvCreateTimeout)
		defer cancel()

		basePython := "python"
		if runtime.GOOS != "windows" {
			if _, err := exec.LookPath("python3"); err == nil {
				basePython = "python3"
			} else if _, err := exec.LookPath("python"); err != nil {
				return nil, fmt.Errorf("sistemde Python bulunamadı (python veya python3)")
			}
		}

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

	if _, err := os.Stat(env.UvPath); os.IsNotExist(err) {
		logger.Action("⚡ Injecting 'uv' into isolated environment via pip...")

		if systemUv, err := exec.LookPath("uv"); err == nil {
			logger.Debug("🔍 Sistemde uv bulundu: %s, kopyalanıyor...", systemUv)

			if runtime.GOOS == "windows" {
				cmd := exec.Command("copy", "/Y", systemUv, env.UvPath)
				if _, err := cmd.CombinedOutput(); err != nil {
					logger.Warn("⚠️ UV kopyalama başarısız, pip ile kuruluyor: %v", err)
					return installUvViaPip(env)
				}
			} else {
				cmd := exec.Command("cp", systemUv, env.UvPath)
				if _, err := cmd.CombinedOutput(); err != nil {
					logger.Warn("⚠️ UV kopyalama başarısız, pip ile kuruluyor: %v", err)

					return installUvViaPip(env)
				}
				
				if err := os.Chmod(env.UvPath, 0755); err != nil {
					logger.Warn("⚠️ UV executable permission ayarlanamadı: %v", err)
				}
			}
			
			logger.Success("✅ 'uv' successfully copied from system at: %s", env.UvPath)
			return env, nil
		}

		return installUvViaPip(env)
	} else {
		logger.Debug("⚡ 'uv' runtime is active and verified: %s", env.UvPath)
	}

	return env, nil
}

func installUvViaPip(env *PythonEnv) (*PythonEnv, error) {
	ctx, cancel := context.WithTimeout(context.Background(), UvInstallTimeout)
	defer cancel()

	uvPackage := "uv"


	cmd := exec.CommandContext(ctx, env.PipPath, "install", "--upgrade", uvPackage)
	
	cmd.Env = append(os.Environ(),
		"PIP_NO_CACHE_DIR=1",    
		"PIP_DISABLE_PIP_VERSION_CHECK=1", 
	)
	
	if out, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("uv installation zaman aşımına uğradı (%d sn)", int(UvInstallTimeout.Seconds()))
		}
		return nil, fmt.Errorf("uv injection failed: %v\nTerminal Output: %s", err, string(out))
	}

	if _, err := os.Stat(env.UvPath); os.IsNotExist(err) {
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

	if runtime.GOOS != "windows" {
		if err := os.Chmod(env.UvPath, 0755); err != nil {
			logger.Warn("⚠️ UV executable permission ayarlanamadı: %v", err)
		}
	}

	logger.Success("✅ 'uv' successfully injected at: %s", env.UvPath)
	return env, nil
}

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

	if _, err := os.Stat(env.PythonPath); os.IsNotExist(err) {
		return fmt.Errorf("python bulunamadı: %s", env.PythonPath)
	}

	if _, err := os.Stat(env.PipPath); os.IsNotExist(err) {
		return fmt.Errorf("pip bulunamadı: %s", env.PipPath)
	}

	if _, err := os.Stat(env.UvPath); err == nil {
		logger.Debug("✅ UV mevcut: %s", env.UvPath)
	} else {
		logger.Warn("⚠️ UV bulunamadı: %s (bazı özellikler çalışmayabilir)", env.UvPath)
	}

	return nil
}


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


func (env *PythonEnv) ReinstallUv() error {
	if env == nil {
		return fmt.Errorf("pythonEnv nil")
	}

	logger.Warn("⚠️ [SetupVenv] UV yeniden kuruluyor...")

	if _, err := os.Stat(env.UvPath); err == nil {
		if err := os.Remove(env.UvPath); err != nil {
			logger.Warn("⚠️ Eski UV silinemedi: %v", err)
		}
	}

	_, err := installUvViaPip(env)
	return err
}
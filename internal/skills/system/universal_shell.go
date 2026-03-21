package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/config"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

type UniversalShellTool struct {
	Config *config.Config
}

func (t *UniversalShellTool) Name() string { return "sys_exec" } 

func (t *UniversalShellTool) Description() string {
	return "Evrensel terminal. Sistem ortamını (Windows/Linux) otomatik algılar. Kendi kendine WSL komutları da çalıştırabilirsin. Kırmızı Bölgeler (Windows klasörü, /etc vb.) klasör kalkanı ile korunmaktadır."
}

func (t *UniversalShellTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "Çalıştırılacak komut. Windows için PowerShell/CMD, Linux için Bash syntax'ı kullan.",
			},
			"work_dir": map[string]interface{}{
				"type":        "string",
				"description": "Komutun çalıştırılacağı dizin.",
			},
			"timeout": map[string]interface{}{
				"type":        "integer",
				"description": "Saniye cinsinden zaman aşımı (Varsayılan 60).",
			},
			"env": map[string]interface{}{
				"type":        "object",
				"description": "Komuta özel geçici ortam değişkenleri.",
			},
			"background": map[string]interface{}{
				"type":        "boolean",
				"description": "🚀 Efsanevi yetenek: Komutu arka planda çalıştırır (Fire and Forget). Sunucu başlatma, ağ taraması gibi uzun sürecek işlemlerde sistemi kilitlememek için BUNU KESİNLİKLE TRUE YAP.",
				"default":     false,
			},
		},
		"required": []string{"command"},
	}
}

type ShellResult struct {
	Status    string `json:"status"`
	ExitCode  int    `json:"exit_code"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Truncated bool   `json:"truncated"`
	AgentHint string `json:"agent_hint,omitempty"`
}

type ShellRingBuffer struct {
	buffer    []byte
	max       int
	Truncated bool
}

func (r *ShellRingBuffer) Write(p []byte) (n int, err error) {
	if len(r.buffer)+len(p) > r.max {
		r.Truncated = true
		overflow := (len(r.buffer) + len(p)) - r.max
		if overflow < len(r.buffer) {
			r.buffer = r.buffer[overflow:]
		} else {
			r.buffer = []byte{}
			p = p[len(p)-r.max:]
		}
	}
	r.buffer = append(r.buffer, p...)
	return len(p), nil
}

func checkDirectoryShield(cmd string) error {
	cmdLower := strings.ToLower(cmd)
	
	destructiveWords := []string{"rm ", "del ", "format ", "rmdir ", "rd ", "mv ", "cp ", ">", ">>", "chmod", "chown", "Remove-Item"}
	isDestructive := false
	for _, word := range destructiveWords {
		if strings.Contains(cmdLower, word) {
			isDestructive = true
			break
		}
	}

	if !isDestructive {
		return nil 
	}

	winRedZones := []string{`c:\windows`, `c:\program files`, `c:\boot`}
	linRedZones := []string{` /etc`, ` /var`, ` /boot`, ` /sbin`, ` /root`, ` /sys`}

	if runtime.GOOS == "windows" {
		for _, zone := range winRedZones {
			if strings.Contains(cmdLower, zone) {
				return fmt.Errorf("KLASÖR KALKANI AKTİF: '%s' sistem klasöründe silme veya üzerine yazma işlemi yasaktır! (Sadece okuma yapabilirsin)", zone)
			}
		}
	} else {
		for _, zone := range linRedZones {
			if strings.Contains(cmdLower, zone) {
				return fmt.Errorf("KLASÖR KALKANI AKTİF: '%s' sistem klasöründe silme veya üzerine yazma işlemi yasaktır! (Sadece okuma yapabilirsin)", zone)
			}
		}
		if strings.Contains(cmdLower, "rm -rf / ") || strings.HasSuffix(cmdLower, "rm -rf /") {
			return fmt.Errorf("KLASÖR KALKANI AKTİF: Kök dizin (/) silinemez!")
		}
	}
	return nil
}

func getWindowsShell() string {
	sysRoot := os.Getenv("SystemRoot")
	if sysRoot == "" { sysRoot = "C:\\Windows" }
	path := filepath.Join(sysRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if _, err := os.Stat(path); err == nil { return path }
	return "powershell.exe"
}


func (t *UniversalShellTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	cmdStr, _ := args["command"].(string)
	result := ShellResult{Status: "success", ExitCode: 0}

	securityLevel := "standard"
	if t.Config != nil {
		securityLevel = t.Config.Security.Level
	}

	shieldErr := checkDirectoryShield(cmdStr)
	
	if shieldErr != nil {
		if securityLevel != "god_mode" {

			logger.Warn("🛡️ %s", shieldErr.Error())
			result.Status = "security_blocked"
			result.ExitCode = -1
			result.Stderr = shieldErr.Error()
			result.AgentHint = "Sistem kırmızı bölgelerine yazma/silme yapamazsın. Lütfen işlemi kendi çalışma klasöründe gerçekleştir veya sadece okuma komutları (dir/ls, cat/type) kullan. (Tam yetki için god_mode gereklidir)"
			return formatShellJSON(result), nil
		} else {
			logger.Warn("⚠️ GOD MODE BÖLGESİ: Tehlikeli komut algılandı ancak yetki tam olduğu için izin verildi -> %s", cmdStr)
		}
	}


	timeoutSec := 60
	if val, ok := args["timeout"].(float64); ok { timeoutSec = int(val) }
	if timeoutSec > 300 { timeoutSec = 300 }

	isBackground := false
	if bg, ok := args["background"].(bool); ok {
		isBackground = bg
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	if !isBackground {
		defer cancel()
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		logger.Action("⚡ WinTerm (Universal Shell): Komut icra ediliyor...")
		psCommand := fmt.Sprintf("$OutputEncoding = [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; %s", cmdStr)
		
		if isBackground {
			cmd = exec.Command(getWindowsShell(), "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", psCommand)
		} else {
			cmd = exec.CommandContext(execCtx, getWindowsShell(), "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", psCommand)
		}
	} else {
		logger.Action("⚡ LinuxTerm (Universal Shell): Komut icra ediliyor...")
		if isBackground {
			cmd = exec.Command("bash", "-c", cmdStr)
		} else {
			cmd = exec.CommandContext(execCtx, "bash", "-c", cmdStr)
		}
	}

	cmd.Env = os.Environ()
	if envVars, ok := args["env"].(map[string]interface{}); ok {
		for k, v := range envVars {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%v", k, v))
		}
	}

	if workDir, ok := args["work_dir"].(string); ok && workDir != "" {
		cleanPath := filepath.Clean(workDir)
		if stat, err := os.Stat(cleanPath); err == nil && stat.IsDir() {
			cmd.Dir = cleanPath
		} else {
			result.Status = "path_error"
			result.ExitCode = -1
			result.Stderr = "Çalışma dizini bulunamadı: " + cleanPath
			if isBackground { cancel() } 
			return formatShellJSON(result), nil
		}
	}

	if isBackground {
		err := cmd.Start()
		if err != nil {
			result.Status = "error"
			result.ExitCode = 1
			result.Stderr = "Arka plan işlemi başlatılamadı: " + err.Error()
			cancel() 
			return formatShellJSON(result), nil
		}

		go func() {
			_ = cmd.Wait()
			cancel() 
		}()

		result.Status = "background_running"
		result.ExitCode = 0
		result.Stdout = fmt.Sprintf("✅ İşlem arka planda başlatıldı (PID: %d).", cmd.Process.Pid)
		result.AgentHint = "DİKKAT: Komut şu an arka planda asenkron çalışıyor. Bu aracın çıktısı işlemi tamamladığını GÖSTERMEZ. İşlemin bitip bitmediğini log dosyasından veya process (ps/tasklist) listesinden ayrıca kontrol etmelisin."
		
		logger.Success("🚀 İşlem arka planda fırlatıldı! Pars diğer görevlere devam ediyor (PID: %d)", cmd.Process.Pid)
		return formatShellJSON(result), nil
	}

	stdoutBuf := &ShellRingBuffer{max: 100 * 1024}
	stderrBuf := &ShellRingBuffer{max: 50 * 1024}
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	go func() {
		<-execCtx.Done()
		if execCtx.Err() == context.DeadlineExceeded && cmd.Process != nil {
			logger.Warn("🛑 TIMEOUT: PID %d yok ediliyor!", cmd.Process.Pid)
			if runtime.GOOS == "windows" {
				_ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(cmd.Process.Pid)).Run()
			} else {
				_ = cmd.Process.Kill()
			}
		}
	}()

	err := cmd.Run()

	result.Stdout = string(stdoutBuf.buffer)
	result.Stderr = string(stderrBuf.buffer)
	result.Truncated = stdoutBuf.Truncated || stderrBuf.Truncated

	if execCtx.Err() == context.DeadlineExceeded {
		result.Status = "timeout"
		result.ExitCode = 124
		result.Stderr = "İşlem süresi aşıldı."
		result.AgentHint = "Eğer bu uzun sürecek bir sunucu işlemiyse, 'background': true parametresini kullanarak arka planda çalıştırmalısın."
		return formatShellJSON(result), nil
	}

	if err != nil {
		result.Status = "error"
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			if result.ExitCode == 5 || strings.Contains(strings.ToLower(result.Stderr), "permission denied") {
				result.AgentHint = "Erişim Engellendi (Access Denied). Yönetici/Sudo yetkisi gerekebilir veya Klasör Kalkanı işlemi bloklamış olabilir."
			} else if result.ExitCode == 127 || (result.ExitCode == 1 && strings.Contains(result.Stderr, "is not recognized")) {
				result.AgentHint = "Komut bulunamadı. Yazım hatası yapmış olabilirsin veya sistemin PATH'inde ekli değil."
			}
		} else {
			result.ExitCode = 1
			result.Stderr += "\nSistem Hatası: " + err.Error()
		}
	}

	return formatShellJSON(result), nil
}

func formatShellJSON(data ShellResult) string {
	b, _ := json.MarshalIndent(data, "", "  ")
	return string(b)
}
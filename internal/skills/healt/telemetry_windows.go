//go:build windows

package healt

import (
	"os/exec"
	"strings"
)

// IsScreenLocked Windows'ta oturumun kilitli olup olmadığını kontrol eder.
func IsScreenLocked() bool {
	// LogonUI.exe çalışıyorsa kilit ekranındayız demektir
	cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq LogonUI.exe")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "LogonUI.exe")
}
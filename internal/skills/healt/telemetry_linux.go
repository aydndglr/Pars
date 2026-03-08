//go:build linux

package healt

import (
	"os/exec"
)

// IsScreenLocked Linux'ta oturumun kilitli olup olmadığını kontrol eder.
// Sunucularda genelde masaüstü olmadığı için varsayılan olarak kilitli (headless) sayabiliriz
// veya SSH oturumlarını kontrol edebiliriz.
func IsScreenLocked() bool {
	// GNOME/KDE gibi masaüstü ortamları için ekran koruyucu kontrolü
	cmd := exec.Command("gnome-screensaver-command", "-q")
	out, err := cmd.Output()
	if err == nil && len(out) > 0 {
		return true // Ekran koruyucu aktif
	}
	
	// Sunucu ise (Masaüstü ortamı yoksa) her zaman WhatsApp'tan atması için true dönüyoruz
	return true 
}
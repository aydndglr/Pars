//go:build !windows

package main

// Linux ve MacOS'ta daemon ve servis süreçleri zaten arka planda (systemd ile) çalışır.
// Masaüstü penceresi gibi bir kavram olmadığı için bu fonksiyonların içi boş kalır.
func hideConsole() {
	// İşlem yok
}

// Linux'ta terminal kurulumu otomatik yapılır, ekstra tahsis gerekmez.
func initConsole() {
	// İşlem yok
}
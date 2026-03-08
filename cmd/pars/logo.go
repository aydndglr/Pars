package main

import (
	"fmt"
	"strings"
)

// printFixedHeader: Sadece ilk açılışta logoyu basar.
// Kaydırma bölgesini (12;r) kaldırdık! Artık mouse tekerleğiyle geçmişe dilediğin kadar çıkabilirsin.
func printFixedHeader() {
	neonGreen := "\033[92m"
	reset := "\033[0m"
	logo := "Proaktif Akıl [PA]🐯[RS] Rasyonel Sistem"
	
	// Ekranı temizle, logoyu bas ve bırak (Scrollback bozulmaz)
	fmt.Print("\033[H\033[2J")
	fmt.Printf("%s%s%s\n", neonGreen, logo, reset)
	fmt.Println("\033[90m--------------------------------------------------\033[0m")
	
	// Varsayılan pencere başlığını ayarla
	SetTaskStatus("") 
}

// 🚀 YENİ: SetTaskStatus
// Görev durumunu hem ekrana şık bir rozet olarak basar hem de terminal penceresinin başlığına (Title Bar) sabitler.
func SetTaskStatus(taskName string) {
	if taskName == "" {
		// Görev yoksa başlığı normale döndür
		fmt.Print("\033]0;[PA]🐯[RS] - Beklemede...\007")
		return
	}

	// 1. PENCERE BAŞLIĞI BÜYÜSÜ (Title Bar Injection)
	// Sen sayfayı ne kadar aşağı/yukarı kaydırırsan kaydır, terminalin en üstünde bu başlık hep sabit kalacak!
	title := fmt.Sprintf("[PA]🐯[RS] - ⚡ AKTİF GÖREV: %s", taskName)
	fmt.Printf("\033]0;%s\007", title)

	// 2. TERMİNAL İÇİ ROZET (Görsel Uyarı)
	// Logların arasına sarı/siyah renkli "Enterprise" bir dikkat bloğu basıyoruz.
	fmt.Printf("\n\033[43;30m ⚡ DEVAM EDEN GÖREV \033[0m \033[93m%s\033[0m\n", taskName)
	fmt.Println("\033[90m" + strings.Repeat("-", 50) + "\033[0m")
}
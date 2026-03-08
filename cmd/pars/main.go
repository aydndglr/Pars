package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/ipc"
)

func main() {
	// =====================================================================
	// 🚀 SİBER KALKAN: Ctrl+C (SIGINT) Yakalayıcı
	// =====================================================================
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		// Ctrl+C basıldığında işletim sistemini durdurup son sözümüzü söylüyoruz!
		fmt.Print("\n\033[1A\033[2K") // Ekranda çıkan o çirkin "^C" yazısını siler
		fmt.Println("\n\033[92m🐯 Pars :\033[0m Görüşürüz patron! Evrenin bana ihtiyacı var... 🚀")
		time.Sleep(500 * time.Millisecond) // Mesajı okuman için yarım saniye mühlet
		os.Exit(0)
	}()

	// =====================================================================
	// 🚀 WINDOWS GÖREV ZAMANLAYICI & GO RUN KORUMASI (SADECE DAEMON/SETUP)
	// =====================================================================
	if len(os.Args) > 1 && (os.Args[1] == "--daemon" || os.Args[1] == "--setup") {
		exePath, err := os.Executable()
		if err == nil && !strings.Contains(exePath, "go-build") && !strings.Contains(exePath, "Temp") {
			os.Chdir(filepath.Dir(exePath))
		}
	}

	// =====================================================================
	// 🚀 AŞAMA 1: CLI ROUTER (TRAFİK POLİSİ)
	// =====================================================================
	if len(os.Args) > 1 {
		args := os.Args[1:]

		if args[0] == "--setup" {
			runTerminalSetup()
			return
		} else if args[0] == "--daemon" {
			startDaemon()
			return
		}

		// ⚡ Tek Seferlik Komut (Örn: "pars sistem durumunu kontrol et" veya "pars --learn-docs ...")
		// Tüm gereksiz --task ve --cmd parametre avcıları silindi!
		argStr := strings.Join(args, " ")
		
		// Tek seferlik komutlara özel tek kullanımlık frekans ID'si üret
		tempID := fmt.Sprintf("CMD-%d", time.Now().UnixNano()%1000000)
		
		result, err := ipc.SendCommand(tempID, argStr)

		if err != nil {
			fmt.Printf("\n❌ HATA: %v\n", err)
			fmt.Println("\033[90m(pars henüz uyanmamış. Önce arkada çalışması lazım.)\033[0m\n")
		} else {
			// Başarılıysa zaten arkadaki logger "data:" ile basmış olmayabilir (tek seferlik olduğu için)
			// O yüzden nihai cevabı ekrana tertemiz basıyoruz
			fmt.Printf("\n\033[36m🐯 pars:\033[0m %s\n\n", result)
		}
		return
	}

	// 💻 Hiçbir parametre yoksa, klasik boş İstemciyi (CLI) başlat
	startInteractiveCLI()
}
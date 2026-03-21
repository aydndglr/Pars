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
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

func main() {
	logger.Info("Pars Core başlatılıyor...")

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		logger.Info("SIGINT/SIGTERM sinyali alındı, sistem güvenli şekilde durduruluyor.")
		fmt.Print("\n\033[1A\033[2K")
		fmt.Println("\n\033[92m🐯 Pars :\033[0m Görüşürüz patron! Evrenin bana ihtiyacı var... 🚀")
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()

	if len(os.Args) > 1 && (os.Args[1] == "--daemon" || os.Args[1] == "--setup") {
		logger.Debug("Daemon/Setup parametresi tespit edildi, çalışma dizini kontrol ediliyor.")
		exePath, err := os.Executable()
		if err == nil && !strings.Contains(exePath, "go-build") && !strings.Contains(exePath, "Temp") {
			os.Chdir(filepath.Dir(exePath))
			logger.Debug("Çalışma dizini güncellendi: %s", filepath.Dir(exePath))
		} else if err != nil {
			logger.Error("Çalıştırılabilir dosya yolu alınırken hata: %v", err)
		}
	}

	if len(os.Args) > 1 {
		args := os.Args[1:]
		logger.Debug("Gelen argümanlar işleniyor: %v", args)

		if args[0] == "--setup" {
			logger.Info("Kurulum (--setup) süreci başlatılıyor.")
			runTerminalSetup()
			return
		} else if args[0] == "--daemon" {
			logger.Info("Arka plan servisi (--daemon) başlatılıyor.")
			startDaemon()
			return
		}

		argStr := strings.Join(args, " ")
		tempID := fmt.Sprintf("CMD-%d", time.Now().UnixNano()%1000000)

		logger.Info("IPC üzerinden komut gönderiliyor. ID: %s, Komut: %s", tempID, argStr)
		result, err := ipc.SendCommand(tempID, argStr)

		if err != nil {
			logger.Error("IPC komut gönderimi başarısız oldu: %v", err)
			fmt.Printf("\n❌ HATA: %v\n", err)
			fmt.Println("\033[90m(pars henüz uyanmamış. Önce arkada çalışması lazım.)\033[0m\n")
		} else {
			logger.Info("IPC komut yanıtı başarıyla alındı.")
			fmt.Printf("\n\033[36m🐯 pars:\033[0m %s\n\n", result)
		}
		return
	}

	logger.Info("İnteraktif CLI modu başlatılıyor.")
	startInteractiveCLI()
}
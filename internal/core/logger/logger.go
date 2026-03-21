package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorCyan   = "\033[36m"
	ColorPurple = "\033[35m"
	ColorWhite  = "\033[37m"
)

type LogHook func(level, message string)
type HookEntry struct {
	ID   int
	Hook LogHook
}

type hookJob struct {
	level   string
	message string
}

var (
	debugMode      bool
	logFile        *os.File
	multiWriter    io.Writer
	publishHooks   []HookEntry
	hookMu         sync.RWMutex
	hookIDCounter  int = 0
	hookJobChan  chan hookJob
	hookStopChan chan struct{}
	workerOnce   sync.Once
)

func startHookWorker() {
	hookJobChan = make(chan hookJob, 1000) 
	hookStopChan = make(chan struct{})

	go func() {
		for {
			select {
			case job := <-hookJobChan:
				hookMu.RLock()
				hooksToCall := make([]LogHook, 0, len(publishHooks))
				for _, entry := range publishHooks {
					if entry.Hook != nil {
						hooksToCall = append(hooksToCall, entry.Hook)
					}
				}
				hookMu.RUnlock()
				for _, hook := range hooksToCall {
					hook(job.level, job.message)
				}
			case <-hookStopChan:
				return
			}
		}
	}()
}

func AddOutputHook(hook LogHook) int {
	if hook == nil {
		return -1
	}
	
	hookMu.Lock()
	hookIDCounter++
	entry := HookEntry{
		ID:   hookIDCounter,
		Hook: hook,
	}
	publishHooks = append(publishHooks, entry)
	
	currentID := hookIDCounter
	currentTotal := len(publishHooks)
	hookMu.Unlock()
	
	Debug("📡 [Logger] Hook eklendi: ID=%d, Toplam=%d", currentID, currentTotal)
	return currentID
}

func RemoveOutputHook(id int) bool {
	if id <= 0 {
		return false
	}
	
	hookMu.Lock()
	for i, entry := range publishHooks {
		if entry.ID == id {
			publishHooks = append(publishHooks[:i], publishHooks[i+1:]...)
			currentTotal := len(publishHooks)
			hookMu.Unlock()
			
			Debug("🗑️ [Logger] Hook silindi: ID=%d, Kalan=%d", id, currentTotal)
			return true
		}
	}
	hookMu.Unlock()
	
	Warn("⚠️ [Logger] Silinecek hook bulunamadı: ID=%d", id)
	return false
}

func GetHookCount() int {
	hookMu.RLock()
	defer hookMu.RUnlock()
	return len(publishHooks)
}

func ClearAllHooks() {
	hookMu.Lock()
	count := len(publishHooks)
	publishHooks = nil
	hookMu.Unlock()
	
	Debug("🧹 [Logger] Tüm hook'lar temizlendi: %d adet", count)
}

func Setup(debug bool, logDir string) {
	debugMode = debug
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Printf("⚠️ Log dizini oluşturulamadı: %v\n", err)
		return
	}

	path := filepath.Join(logDir, "pars_system.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("⚠️ Log dosyası açılamadı: %v\n", err)
		return
	}
	logFile = f
	workerOnce.Do(func() {
		startHookWorker()
	})
	
	Debug("📝 [Logger] Log sistemi başlatıldı: %s", path)
}

func logMessage(color, level, msg string) {
	timestamp := time.Now().Format("15:04:05")
	consoleMsg := fmt.Sprintf("%s[%-7s]%s %s %s\n", color, level, ColorReset, timestamp, msg)
	fileMsg := fmt.Sprintf("[%s] [%-7s] %s\n", time.Now().Format("2006-01-02 15:04:05"), level, msg)

	isError := (level == "ERROR" || level == "ALERT")

	// Konsol Çıktısı: Sadece önemli olaylar gösterilsin (INFO ve DEBUG gizlendi, terminal temizlendi)
	showOnConsole := false
	switch level {
	case "ACTION", "WARN", "ERROR", "ALERT", "SUCCESS":
		showOnConsole = true
	}

	if showOnConsole {
		fmt.Print(consoleMsg)
	}

	// DOSYA LOGLAMA: SADECE HATALAR (Kritik optimizasyon: Gereksiz I/O engellendi)
	if isError && logFile != nil {
		_, err := logFile.WriteString(fileMsg)
		if err != nil {
			fmt.Printf("⚠️ [Logger] Dosya yazma hatası: %v\n", err)
		}
	}

	// IPC / Stream için hook mekanizması (Terminal UI'ın bozulmaması için INFO/DEBUG hariç devam ediyor)
	if level == "ACTION" || level == "ALERT" || level == "ERROR" || level == "WARN" || level == "SUCCESS" {
		cleanMsg := strings.ReplaceAll(msg, "\033", "") 
		if hookJobChan != nil {
			select {
			case hookJobChan <- hookJob{level: level, message: cleanMsg}:
			default:
				if isError {
					fmt.Println("⚠️ [Logger] Uyarı: Log kuyruğu dolu, bildirim atlandı!")
				}
			}
		}
	}
}

func Info(format string, v ...interface{})    { logMessage(ColorBlue, "INFO", fmt.Sprintf(format, v...)) }
func Success(format string, v ...interface{}) { logMessage(ColorGreen, "SUCCESS", fmt.Sprintf(format, v...)) }
func Action(format string, v ...interface{})  { logMessage(ColorPurple, "ACTION", fmt.Sprintf(format, v...)) }
func Warn(format string, v ...interface{})    { logMessage(ColorYellow, "WARN", fmt.Sprintf(format, v...)) }
func Error(format string, v ...interface{})   { logMessage(ColorRed, "ERROR", fmt.Sprintf(format, v...)) }
func Debug(format string, v ...interface{})   { logMessage(ColorCyan, "DEBUG", fmt.Sprintf(format, v...)) }
func Alert(format string, v ...interface{})   { logMessage(ColorRed, "ALERT", fmt.Sprintf(format, v...)) }

func Close() {
	ClearAllHooks()
	if hookStopChan != nil {
		close(hookStopChan)
	}

	if logFile != nil {
		err := logFile.Close()
		if err != nil {
			fmt.Printf("⚠️ [Logger] Log dosyası kapatılamadı: %v\n", err)
		}
		logFile = nil
	}
	// Son mesaj konsola özel gönderiliyor
	fmt.Println("🛑 [Logger] Logger kapatıldı")
}
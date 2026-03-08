// internal/core/logger/logger.go
// 🚀 DÜZELTME V3: RWMutex Deadlock TAMAMEN ÇÖZÜLDÜ
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

// ========================================================================
// 🎨 TERMINAL COLOR CODES
// ========================================================================
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

// LogHook: Log mesajlarını dinleyen callback fonksiyonu
type LogHook func(level, message string)

// HookEntry - Hook'u ID ile takip etmek için yapı
type HookEntry struct {
	ID   int
	Hook LogHook
}

var (
	debugMode       bool
	logFile         *os.File
	multiWriter     io.Writer
	publishHooks    []HookEntry
	hookMu          sync.RWMutex
	hookIDCounter   int = 0
)

// AddOutputHook: Yeni hook ekler ve benzersiz ID döner
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
	
	// 🚀 KİLİDİ LOG YAZMADAN ÖNCE AÇIYORUZ! (Deadlock'u önler)
	hookMu.Unlock()
	
	Debug("📡 [Logger] Hook eklendi: ID=%d, Toplam=%d", currentID, currentTotal)
	return currentID
}

// RemoveOutputHook - Hook'u ID ile sil
func RemoveOutputHook(id int) bool {
	if id <= 0 {
		return false
	}
	
	hookMu.Lock()
	for i, entry := range publishHooks {
		if entry.ID == id {
			// Slice'dan sil
			publishHooks = append(publishHooks[:i], publishHooks[i+1:]...)
			currentTotal := len(publishHooks)
			
			// 🚀 KİLİDİ AÇ
			hookMu.Unlock()
			
			Debug("🗑️ [Logger] Hook silindi: ID=%d, Kalan=%d", id, currentTotal)
			return true
		}
	}
	// Bulunamadıysa kilidi aç ve dön
	hookMu.Unlock()
	
	Warn("⚠️ [Logger] Silinecek hook bulunamadı: ID=%d", id)
	return false
}

// GetHookCount - Debug için hook sayısı
func GetHookCount() int {
	hookMu.RLock()
	defer hookMu.RUnlock()
	return len(publishHooks)
}

// ClearAllHooks - Tüm hook'ları temizle
func ClearAllHooks() {
	hookMu.Lock()
	count := len(publishHooks)
	publishHooks = nil
	// 🚀 KİLİDİ AÇ
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
	
	Debug("📝 [Logger] Log sistemi başlatıldı: %s", path)
}

func logMessage(color, level, msg string) {
	timestamp := time.Now().Format("15:04:05")
	
	// Terminal için Renkli Format
	consoleMsg := fmt.Sprintf("%s[%-7s]%s %s %s\n", color, level, ColorReset, timestamp, msg)
	
	// Dosya Saklama için Sade Format
	fileMsg := fmt.Sprintf("[%s] [%-7s] %s\n", time.Now().Format("2006-01-02 15:04:05"), level, msg)

	showOnConsole := false
	switch level {
	case "ACTION", "WARN", "ERROR", "ALERT", "SUCCESS":
		showOnConsole = true
	case "DEBUG", "INFO":
		showOnConsole = debugMode
	}

	// 1. Konsola Yaz
	if showOnConsole {
		fmt.Print(consoleMsg)
	}

	// 2. Dosyaya Yaz
	if logFile != nil {
		_, err := logFile.WriteString(fileMsg)
		if err != nil {
			fmt.Printf("⚠️ [Logger] Dosya yazma hatası: %v\n", err)
		}
	}

	// 3. Hook'lara Gönder (Kilit Yönetimi)
	cleanMsg := strings.ReplaceAll(msg, "\033", "") 
	
	hookMu.RLock()
	hooksToCall := make([]LogHook, 0, len(publishHooks))
	for _, entry := range publishHooks {
		if entry.Hook != nil {
			hooksToCall = append(hooksToCall, entry.Hook)
		}
	}
	hookMu.RUnlock()
	
	// Hook'ları kilit dışında çağır (deadlock önleme)
	for _, hook := range hooksToCall {
		if level == "ACTION" || level == "ALERT" || level == "ERROR" || level == "WARN" || level == "SUCCESS" {
			go hook(level, cleanMsg)
		}
	}
}

// Standart Arayüzler
func Info(format string, v ...interface{})    { logMessage(ColorBlue, "INFO", fmt.Sprintf(format, v...)) }
func Success(format string, v ...interface{}) { logMessage(ColorGreen, "SUCCESS", fmt.Sprintf(format, v...)) }
func Action(format string, v ...interface{})  { logMessage(ColorPurple, "ACTION", fmt.Sprintf(format, v...)) }
func Warn(format string, v ...interface{})    { logMessage(ColorYellow, "WARN", fmt.Sprintf(format, v...)) }
func Error(format string, v ...interface{})   { logMessage(ColorRed, "ERROR", fmt.Sprintf(format, v...)) }
func Debug(format string, v ...interface{})   { logMessage(ColorCyan, "DEBUG", fmt.Sprintf(format, v...)) }
func Alert(format string, v ...interface{})   { logMessage(ColorRed, "ALERT", fmt.Sprintf(format, v...)) }

func Close() {
	ClearAllHooks()
	if logFile != nil {
		err := logFile.Close()
		if err != nil {
			fmt.Printf("⚠️ [Logger] Log dosyası kapatılamadı: %v\n", err)
		}
		logFile = nil
	}
	Debug("🛑 [Logger] Logger kapatıldı")
}
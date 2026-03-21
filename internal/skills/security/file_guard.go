package security

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/fsnotify/fsnotify"
)

type FileGuard struct {
	EventChan    chan<- string
	ctx          context.Context
	cancel       context.CancelFunc
	canaryFiles  []string
	criticalDocs map[string]string 
	mu           sync.RWMutex
	ignoreEvents sync.Map       
}

func NewFileGuard(eventChan chan<- string) *FileGuard {
	ctx, cancel := context.WithCancel(context.Background())
	return &FileGuard{
		EventChan:    eventChan,
		ctx:          ctx,
		cancel:       cancel,
		criticalDocs: make(map[string]string),
	}
}

func (f *FileGuard) Start() {
	logger.Success("🛡️ [SECURITY] FIM (Dosya Bütünlüğü) ve Ransomware Kapanı Aktif!")
	f.initCriticalFiles()
	f.deployCanaryFiles()
	go f.watchFIM()       
	go f.watchCanaries()
}

func (f *FileGuard) Stop() {
	f.cancel()
	for _, canary := range f.canaryFiles {
		os.Remove(canary)
	}
	logger.Warn("🛡️ [SECURITY] Dosya Kalkanları indirildi, Yem dosyalar temizlendi.")
}

func (f *FileGuard) sendAlert(msg string) {
	select {
	case f.EventChan <- msg:
	default:
		logger.Warn("⚠️ [SECURITY] EventChan dolu! Dosya alarmı sıraya alınamadı.")
	}
}

func (f *FileGuard) initCriticalFiles() {
	var targets []string

	if runtime.GOOS == "windows" {
		sysRoot := os.Getenv("SystemRoot")
		if sysRoot == "" {
			sysRoot = `C:\Windows`
		}
		targets = append(targets, filepath.Join(sysRoot, `System32\drivers\etc\hosts`))
	} else {
		targets = append(targets, "/etc/passwd", "/etc/hosts")
	}

	for _, path := range targets {
		hash, err := f.calculateSHA256(path)
		if err == nil {
			f.criticalDocs[path] = hash
		}
	}
}

func (f *FileGuard) watchFIM() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-f.ctx.Done():
			return
		case <-ticker.C:
			f.mu.Lock()
			for path, oldHash := range f.criticalDocs {
				newHash, err := f.calculateSHA256(path)
				
				if err != nil && !os.IsNotExist(err) {
					continue
				}

				if os.IsNotExist(err) {
					msg := fmt.Sprintf("[SİBER GÜVENLİK BİLDİRİMİ] [CRITICAL]: 🛑 KRİTİK SİSTEM DOSYASI SİLİNDİ! '%s' dosyası artık yok. Bu bir rootkit veya zararlı yazılım faaliyeti olabilir!", path)
					f.sendAlert(msg)
					delete(f.criticalDocs, path) 
				} else if newHash != oldHash {
					msg := fmt.Sprintf("[SİBER GÜVENLİK BİLDİRİMİ] [CRITICAL]: ⚠️ DOSYA BÜTÜNLÜĞÜ BOZULDU! '%s' dosyasının içeriği izinsiz değiştirildi (Hash değişti). Bir virüs arka kapı (backdoor) yerleştirmiş olabilir!", path)
					f.sendAlert(msg)
					f.criticalDocs[path] = newHash 
				}
			}
			f.mu.Unlock()
		}
	}
}

func (f *FileGuard) calculateSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func (f *FileGuard) deployCanaryFiles() {

	baseDir, _ := os.Getwd()
	tmpDir := os.TempDir()

	f.canaryFiles = []string{
		filepath.Join(baseDir, "!_000_DO_NOT_DELETE_passwords.txt"),
		filepath.Join(tmpDir, "financial_backup_2026.docx"), 
	}

	for _, canary := range f.canaryFiles {
		err := os.WriteFile(canary, []byte("SYSTEM_SECURE_VAULT_DO_NOT_MODIFY\nENCRYPTED_KEY=8A9B2C"), 0644)
		if err != nil {
			logger.Warn("Yem dosya oluşturulamadı: %s", canary)
		}
	}
}

func (f *FileGuard) watchCanaries() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Error("Fidye Yazılımı Kapanı başlatılamadı: %v", err)
		return
	}
	defer watcher.Close()
	dirsToWatch := make(map[string]bool)
	for _, canary := range f.canaryFiles {
		dirsToWatch[filepath.Dir(canary)] = true
	}

	for dir := range dirsToWatch {
		_ = watcher.Add(dir)
	}

	for {
		select {
		case <-f.ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			cleanPath := filepath.Clean(event.Name)
			if _, ignored := f.ignoreEvents.Load(cleanPath); ignored {
				continue
			}
			isCanary := false
			for _, canary := range f.canaryFiles {
				if cleanPath == filepath.Clean(canary) {
					isCanary = true
					break
				}
			}

			if isCanary {
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
					msg := fmt.Sprintf("[SİBER GÜVENLİK BİLDİRİMİ] [CRITICAL]: ☠️ FİDYE YAZILIMI (RANSOMWARE) SALDIRISI ALGILANDI! Yem dosyaya (%s) müdahale edildi! Eylem: %s. Sistemi acil güvenli moda almamı ister misin?", event.Name, event.Op.String())
					f.sendAlert(msg)
					if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
						go func(path string) {
							f.ignoreEvents.Store(path, true)
							defer time.AfterFunc(3*time.Second, func() {
								f.ignoreEvents.Delete(path)
							})
							
							time.Sleep(1 * time.Second) 
							_ = os.WriteFile(path, []byte("SYSTEM_SECURE_VAULT_DO_NOT_MODIFY"), 0644)
						}(cleanPath)
					}
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			logger.Warn("Watcher hatası: %v", err)
		}
	}
}
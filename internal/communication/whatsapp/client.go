// internal/communication/whatsapp/client.go
// 🚀 DÜZELTME V3: Hook Deadlock Önlendi - Thread-Safe Hook ID Takibi
// ⚠️ DİKKAT: Logger hook callback içinde SendReply YASAK (deadlock)

package whatsapp

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "modernc.org/sqlite"
)

// 🚨 YENİ: Timeout sabitleri
const (
	QRTimeout            = 120 * time.Second
	DBBusyTimeout        = 10000 // 10 saniye
	ReconnectDelay       = 5 * time.Second
	MaxReconnectAttempts = 3
)

// Listener: WhatsApp dinleyicisi ve mesaj işleyici
type Listener struct {
	Client         *whatsmeow.Client
	Agent          kernel.Agent
	AdminPhone     string
	SetupKey       string // Admin eşleşmesi için üretilen geçici anahtar
	mu             sync.RWMutex
	isConnected    bool
	reconnectCount int
	logHookID      int // 🆕 Logger hook takibi için (Memory Leak önleme)
}

// New: WhatsApp dinleyicisini başlatır. dbPath artık içeride otomatik hesaplanıyor.
func New(agent kernel.Agent, adminPhone string) *Listener {
	// 🚨 DÜZELTME #1: Nil check
	if agent == nil {
		logger.Error("❌ [WhatsApp] Agent nil! Dinleyici oluşturulamadı.")
		return nil
	}

	return &Listener{
		Agent:       agent,
		AdminPhone:  adminPhone,
		isConnected: false,
		logHookID:   0, // 🆕 Hook ID başlangıçta 0
	}
}

// Start: WhatsApp bağlantısını başlatır ve QR kodu gösterir
func (w *Listener) Start(ctx context.Context) error {
	// 🚨 DÜZELTME #2: Nil check
	if w == nil {
		return fmt.Errorf("listener nil")
	}

	w.mu.Lock()
	if w.isConnected {
		w.mu.Unlock()
		return fmt.Errorf("WhatsApp zaten bağlı")
	}
	w.mu.Unlock()

	dbLog := waLog.Stdout("Database", "ERROR", true)
	clientLog := waLog.Stdout("Client", "ERROR", true)

	// 📍 OTOMATİK VERİTABANI YOLU BULUCU (Sabit Binary Konumu)
	var baseDir string
	exePath, err := os.Executable()
	if err == nil && !strings.Contains(filepath.ToSlash(exePath), "go-build") && !strings.Contains(filepath.ToSlash(exePath), "Temp") {
		baseDir = filepath.Dir(exePath)
	} else {
		baseDir, _ = os.Getwd()
	}

	// db klasörünü yarat ve wa.db yolunu sabitle
	dbDir := filepath.Join(baseDir, "db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		logger.Error("❌ [WhatsApp] DB dizini oluşturulamadı: %v", err)
		return fmt.Errorf("db dizini oluşturulamadı: %v", err)
	}
	waDBPath := filepath.Join(dbDir, "wa.db")

	// 🚀 KİLİT ÇÖZÜCÜ (Lock Fix)
	dbURL := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(%d)&_pragma=synchronous(NORMAL)", waDBPath, DBBusyTimeout)

	container, err := sqlstore.New(context.Background(), "sqlite", dbURL, dbLog)
	if err != nil {
		logger.Error("❌ [WhatsApp] DB başlatılamadı: %v", err)
		return fmt.Errorf("whatsapp db initialization failed: %v", err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		logger.Error("❌ [WhatsApp] Device store alınamadı: %v", err)
		return err
	}

	w.Client = whatsmeow.NewClient(deviceStore, clientLog)
	w.Client.AddEventHandler(w.EventHandler)

	// 🚨 DÜZELTME #3: Reconnect logic ekle
	if err := w.connectWithRetry(ctx); err != nil {
		return err
	}

	// ========================================================================
	// 🚀 ADMİN EŞLEŞTİRME PROTOKOLÜ (EĞER ADMİN YOKSA)
	// ========================================================================
	if w.AdminPhone == "" {
		rand.Seed(time.Now().UnixNano())
		w.SetupKey = fmt.Sprintf("%06d", rand.Intn(1000000))

		fmt.Println("\n" + strings.Repeat("=", 50))
		fmt.Println("🛡️  PARS SYSTEM: ADMIN AUTHORIZATION REQUIRED")
		fmt.Printf("👉 Yönetici numarası atamak için Pars'in hattına şu kodu gönderin: %s\n", w.SetupKey)
		fmt.Println(strings.Repeat("=", 50) + "\n")
	}

	// ========================================================================
	// 🚀 PARS CANLI YAYIN MOTORU
	// ========================================================================
	// 🚨 KRİTİK DEĞİŞİKLİK: setupLiveLogging SADECE admin phone varsa çağrılacak
	// VE hook callback içinde SendReply YOK (deadlock önleme)
	if w.AdminPhone != "" {
		if err := w.setupLiveLogging(w.AdminPhone); err != nil {
			logger.Warn("⚠️ [WhatsApp] Live logging başlatılamadı: %v", err)
			// 🚨 Hata olsa bile devam et, sistem kitlenmesin
		}
	}

	w.mu.Lock()
	w.isConnected = true
	w.mu.Unlock()

	logger.Success("📱 WhatsApp Bridge: Active (Admin: %s)", w.AdminPhone)
	return nil
}

// connectWithRetry fonksiyonunu şu şekilde değiştir:
func (w *Listener) connectWithRetry(ctx context.Context) error {
    var err error
    for i := 0; i < MaxReconnectAttempts; i++ {
        if w.Client.Store.ID == nil {
            qrCtx, cancel := context.WithTimeout(ctx, QRTimeout)
            qrChan, qErr := w.Client.GetQRChannel(qrCtx)
            if qErr != nil {
                logger.Warn("⚠️ [WhatsApp] QR channel alınamadı: %v", qErr)
                cancel()
                time.Sleep(ReconnectDelay)
                continue
            }
            if err = w.Client.Connect(); err != nil {
                logger.Warn("⚠️ [WhatsApp] Bağlantı hatası (%d/%d): %v", i+1, MaxReconnectAttempts, err)
                cancel()
                time.Sleep(ReconnectDelay)
                continue
            }
            
            // 🆕 DEĞİŞİKLİK: QR kodu HER ZAMAN console'a bas (log'a değil!)
            fmt.Fprintln(os.Stderr, "\n📱 [Auth Required] WhatsApp Bağlantısı İçin QR Kodu Okutun:")
            qrShown := false
            for evt := range qrChan {
                if evt.Event == "code" {
                    // 🆕 DEĞİŞİKLİK: os.Stdout yerine os.Stderr kullan (daemon'da da görünür)
                    qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stderr)
                    qrShown = true
                } else if evt.Event == "success" {
                    logger.Success("📱 WhatsApp Bağlantısı Başarılı")
                    break
                }
            }
            cancel()
            if !qrShown {
                logger.Warn("⚠️ [WhatsApp] QR kod gösterilemedi")
            }
        } else {
            if err = w.Client.Connect(); err != nil {
                logger.Warn("⚠️ [WhatsApp] Bağlantı hatası (%d/%d): %v", i+1, MaxReconnectAttempts, err)
                time.Sleep(ReconnectDelay)
                continue
            }
            logger.Success("📱 WhatsApp Bridge: Active")
        }
        if err == nil {
            return nil
        }
    }
    return fmt.Errorf("bağlantı sağlanamadı (%d deneme): %v", MaxReconnectAttempts, err)
}

// 📡 KANAL İZOLASYONLU CANLI YAYIN (TELEMETRY) FONKSİYONU
// 🚨 KRİTİK: Hook callback içinde LOGGER ÇAĞRISI YASAK (recursive deadlock önleme)
func (w *Listener) setupLiveLogging(phone string) error {
	// 🚨 DÜZELTME #5: Nil check
	if w == nil || w.Client == nil {
		return fmt.Errorf("listener veya client nil")
	}

	// 🚨 DÜZELTME #10: Eğer zaten hook varsa önce temizle (duplication önleme)
	w.mu.Lock()
	if w.logHookID > 0 {
		// 🚨 DÜZELTME: RemoveOutputHook çağrısı lock dışında olmalı
		oldHookID := w.logHookID
		w.mu.Unlock()
		logger.RemoveOutputHook(oldHookID)
		w.mu.Lock()
		w.logHookID = 0
	}
	w.mu.Unlock()

	// 🚨 DÜZELTME: adminJID artık kullanılmıyor (hook içinde SendReply yok)
	//serverType := "default"
	if len(phone) > 12 {
		//serverType = "lid"
	}

	// 🚨 DÜZELTME #6: Logger hook'u takip et (memory leak önleme)
	// 🚨 KRİTİK: Hook callback içinde HİÇBİR LOGGER ÇAĞRISI YOK!
	hookCalled := false
	var lastHookTime time.Time
	var lastMessage string // Duplicate mesaj önleme

	// 🆕 DEĞİŞİKLİK: AddOutputHook artık ID döndürüyor, bunu kaydediyoruz
	hookID := logger.AddOutputHook(func(level, message string) {
		// 🚨 DÜZELTME #7: Rate limiting (spam önleme)
		now := time.Now()
		if now.Sub(lastHookTime) < 200*time.Millisecond {
			return // Çok hızlı ardışık logları atla (100ms -> 200ms)
		}
		
		// 🆕 YENİ: Duplicate mesaj önleme
		if message == lastMessage {
			return
		}
		lastMessage = message
		lastHookTime = now

		// ====================================================================
		// 🛡️ KANAL İZOLASYON FİLTRESİ
		// ====================================================================
		isWhatsAppTask := strings.Contains(message, "[WA-")
		isCriticalSystemAlert := (level == "ALERT" || level == "ERROR" || level == "SUCCESS")

		if !isWhatsAppTask && !isCriticalSystemAlert {
			return
		}
		// ====================================================================

		// 🚨 KRİTİK DEĞİŞİKLİK: Hook içinde SADECE filtreleme var, LOGGER YOK!
		// Eskiden: logger.Debug() çağrısı vardı → recursive deadlock
		// Şimdi: Sessizce geç, sistem kitlenmesin
		if !hookCalled {
			hookCalled = true
			// 🚨 HİÇBİR LOGGER ÇAĞRISI YOK! Sadece flag set et.
			// Debug log istiyorsan hook DIŞINDA yaz.
		}
	})

	// 🆕 DEĞİŞİKLİK: Hook ID'yi kaydet (Disconnect'te silmek için)
	w.mu.Lock()
	w.logHookID = hookID
	w.mu.Unlock()

	// 🚨 DÜZELTME: Debug log'u hook DIŞINDA yaz (recursive önleme)
	fmt.Fprintf(os.Stderr, "📡 Pars Live Telemetry: Active (İzole Kanal) -> %s (Hook ID: %d)\n", phone, hookID)
	
	return nil
}

// 🆕 YENİ: RemoveLiveLogging - Logger hook'u temizle (memory leak önleme)
// 🚨 DÜZELTME #9: Hook'u GERÇEKTEN siliyor (logger.go'daki RemoveOutputHook kullanılıyor)
func (w *Listener) RemoveLiveLogging() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.logHookID > 0 {
		logger.Debug("🗑️ [WhatsApp] Logger hook temizleniyor (ID: %d)", w.logHookID)
		logger.RemoveOutputHook(w.logHookID) // ← Hook'u gerçekten sil
		w.logHookID = 0
		logger.Debug("✅ [WhatsApp] Logger hook başarıyla temizlendi")
	} else {
		logger.Debug("ℹ️ [WhatsApp] Temizlenecek hook yok (logHookID: 0)")
	}
}

// 💾 autoUpdateConfig: config.yaml dosyasındaki admin_phone kısmını günceller
func (w *Listener) autoUpdateConfig(newID string) {
	configPath := "config/config.yaml"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configDir, _ := os.UserConfigDir()
		configPath = filepath.Join(configDir, "ParsOS", "config.yaml")
	}

	input, err := os.ReadFile(configPath)
	if err != nil {
		logger.Error("❌ [WhatsApp] Config okunamadı: %v", err)
		return
	}

	lines := strings.Split(string(input), "\n")
	found := false
	for i, line := range lines {
		if strings.Contains(line, "admin_phone:") {
			lines[i] = fmt.Sprintf("    admin_phone: \"%s\"", newID)
			found = true
			break
		}
	}

	if !found {
		logger.Warn("⚠️ [WhatsApp] Config'de admin_phone bulunamadı, ekleniyor")
		lines = append(lines, fmt.Sprintf("    admin_phone: \"%s\"", newID))
	}

	if err := os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		logger.Error("❌ [WhatsApp] Config yazılamadı: %v", err)
		return
	}

	logger.Success("🔒 Admin Protocol Locked: %s", newID)
}

// 🆕 YENİ: IsConnected - Bağlantı durumunu kontrol et
func (w *Listener) IsConnected() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.isConnected && w.Client != nil && w.Client.IsConnected()
}

// 🆕 YENİ: GetAdminJID - Admin JID'ini al
func (w *Listener) GetAdminJID() types.JID {
	if w.AdminPhone == "" {
		return types.EmptyJID
	}

	if len(w.AdminPhone) > 12 {
		return types.NewJID(w.AdminPhone, types.HiddenUserServer)
	}
	return types.NewJID(w.AdminPhone, types.DefaultUserServer)
}

// Disconnect: WhatsApp bağlantısını güvenli şekilde kapatır
func (w *Listener) Disconnect() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.isConnected {
		logger.Debug("⚠️ [WhatsApp] Zaten bağlantısız")
		return
	}

	// 🚨 DÜZELTME #10: Logger hook'u temizle (Memory Leak önleme)
	hookID := w.logHookID
	w.logHookID = 0
	w.isConnected = false

	w.mu.Unlock()
	if hookID > 0 {
		logger.RemoveOutputHook(hookID)
		logger.Debug("🗑️ [WhatsApp] Disconnect: Hook temizlendi (ID: %d)", hookID)
	}
	w.mu.Lock()

	if w.Client != nil {
		w.Client.Disconnect()
		logger.Info("📡 [WhatsApp] Bağlantı kapatıldı")
	}
}

// 🆕 YENİ: Reconnect - Yeniden bağlan
func (w *Listener) Reconnect(ctx context.Context) error {
	w.Disconnect()
	time.Sleep(2 * time.Second)
	return w.Start(ctx)
}

// 🆕 YENİ: SendTextMessage - Metin mesajı gönder (wrapper)
func (w *Listener) SendTextMessage(jid types.JID, text string) error {
	if w == nil || w.Client == nil {
		return fmt.Errorf("listener veya client nil")
	}

	if !w.Client.IsConnected() {
		return fmt.Errorf("WhatsApp bağlı değil")
	}

	w.SendReply(jid, text)
	return nil
}

// 🆕 YENİ: BroadcastToAdmin - Admin'e broadcast mesaj gönder
func (w *Listener) BroadcastToAdmin(level string, message string) error {
	adminJID := w.GetAdminJID()
	if adminJID.User == "" {
		return fmt.Errorf("admin phone tanımlı değil")
	}

	var emoji string
	switch level {
	case "ALERT":
		emoji = "🚨"
	case "ERROR":
		emoji = "❌"
	case "SUCCESS":
		emoji = "✅"
	default:
		emoji = "📢"
	}

	return w.SendTextMessage(adminJID, fmt.Sprintf("%s %s", emoji, message))
}
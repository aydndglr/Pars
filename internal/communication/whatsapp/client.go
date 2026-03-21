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

const (
	QRTimeout            = 120 * time.Second
	DBBusyTimeout        = 10000
	ReconnectDelay       = 5 * time.Second
	MaxReconnectAttempts = 3
	HookRateLimit        = 200 * time.Millisecond
)

type Listener struct {
	Client         *whatsmeow.Client
	Agent          kernel.Agent
	AdminPhone     string
	SetupKey       string
	mu             sync.RWMutex
	isConnected    bool
	reconnectCount int
	logHookID      int
}

func isAdminPhone(adminPhone, senderPhone string) bool {
	if adminPhone == "" || senderPhone == "" {
		return false
	}
	// Normalize: + işaretlerini temizle
	cleanAdmin := strings.TrimPrefix(adminPhone, "+")
	cleanSender := strings.TrimPrefix(senderPhone, "+")
	return cleanAdmin == cleanSender
}

func isGroupMessage(chatID types.JID) bool {
	return chatID.Server == types.GroupServer
}

func isBroadcastMessage(chatID types.JID) bool {
	return chatID.Server == types.BroadcastServer
}


func New(agent kernel.Agent, adminPhone string) *Listener {
	if agent == nil {
		logger.Error("❌ [WhatsApp] Agent nil! Dinleyici oluşturulamadı.")
		return nil
	}

	return &Listener{
		Agent:       agent,
		AdminPhone:  adminPhone,
		isConnected: false,
		logHookID:   0,
	}
}


func (w *Listener) Start(ctx context.Context) error {
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

	baseDir := getBaseDir()
	dbDir := filepath.Join(baseDir, "db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		logger.Error("❌ [WhatsApp] DB dizini oluşturulamadı: %v", err)
		return fmt.Errorf("db dizini oluşturulamadı: %w", err)
	}

	waDBPath := filepath.Join(dbDir, "wa.db")
	dbURL := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(%d)&_pragma=synchronous(NORMAL)", waDBPath, DBBusyTimeout)

	container, err := sqlstore.New(context.Background(), "sqlite", dbURL, dbLog)
	if err != nil {
		logger.Error("❌ [WhatsApp] DB başlatılamadı: %v", err)
		return fmt.Errorf("whatsapp db initialization failed: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		logger.Error("❌ [WhatsApp] Device store alınamadı: %v", err)
		return err
	}

	w.Client = whatsmeow.NewClient(deviceStore, clientLog)
	w.Client.AddEventHandler(w.EventHandler)

	if err := w.connectWithRetry(ctx); err != nil {
		return err
	}

	if w.AdminPhone == "" {
		rand.Seed(time.Now().UnixNano())
		w.SetupKey = fmt.Sprintf("%06d", rand.Intn(1000000))

		fmt.Println("\n" + strings.Repeat("=", 50))
		fmt.Println("🛡️  PARS SYSTEM: ADMIN AUTHORIZATION REQUIRED")
		fmt.Printf("👉 Yönetici numarası atamak için Pars'in hattına şu kodu gönderin: %s\n", w.SetupKey)
		fmt.Println(strings.Repeat("=", 50) + "\n")
	}

	if w.AdminPhone != "" {
		if err := w.setupLiveLogging(w.AdminPhone); err != nil {
			logger.Warn("⚠️ [WhatsApp] Live logging başlatılamadı: %v", err)
		}
	}

	w.mu.Lock()
	w.isConnected = true
	w.mu.Unlock()

	logger.Success("📱 WhatsApp Bridge: Active (Admin: %s)", w.AdminPhone)
	return nil
}

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

			fmt.Fprintln(os.Stderr, "\n📱 [Auth Required] WhatsApp Bağlantısı İçin QR Kodu Okutun:")
			qrShown := false

			for evt := range qrChan {
				if evt.Event == "code" {
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

	return fmt.Errorf("bağlantı sağlanamadı (%d deneme): %w", MaxReconnectAttempts, err)
}


func (w *Listener) setupLiveLogging(phone string) error {
	if w == nil || w.Client == nil {
		return fmt.Errorf("listener veya client nil")
	}
	w.mu.Lock()
	if w.logHookID > 0 {
		oldHookID := w.logHookID
		w.logHookID = 0
		w.mu.Unlock() 
		
		logger.RemoveOutputHook(oldHookID)
		logger.Debug("♻️ [WhatsApp] Eski Hook temizlendi (ID: %d)", oldHookID)
	} else {
		w.mu.Unlock()
	}

	var lastHookTime time.Time
	var lastMessage string

	hookID := logger.AddOutputHook(func(level, message string) {
		now := time.Now()
		if now.Sub(lastHookTime) < HookRateLimit {
			return
		}

		if message == lastMessage {
			return
		}
		lastMessage = message
		lastHookTime = now

		isWhatsAppTask := strings.Contains(message, "[WA-")
		isCriticalSystemAlert := (level == "ALERT" || level == "ERROR" || level == "SUCCESS")

		if !isWhatsAppTask && !isCriticalSystemAlert {
			return
		}
	})

	w.mu.Lock()
	w.logHookID = hookID
	w.mu.Unlock()

	fmt.Fprintf(os.Stderr, "📡 Pars Live Telemetry: Active (İzole Kanal) -> %s (Hook ID: %d)\n", phone, hookID)
	return nil
}


func (w *Listener) RemoveLiveLogging() {
	w.mu.Lock()
	hookID := w.logHookID
	w.logHookID = 0
	w.mu.Unlock()

	if hookID > 0 {
		logger.RemoveOutputHook(hookID)
		logger.Debug("✅ [WhatsApp] Logger hook temizlendi (ID: %d)", hookID)
	}
}

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

func (w *Listener) Disconnect() {
	w.mu.Lock()
	if !w.isConnected {
		w.mu.Unlock()
		logger.Debug("⚠️ [WhatsApp] Zaten bağlantısız")
		return
	}

	hookID := w.logHookID
	w.logHookID = 0
	w.isConnected = false
	w.mu.Unlock()
	if hookID > 0 {
		logger.RemoveOutputHook(hookID)
		logger.Debug("🗑️ [WhatsApp] Disconnect: Hook temizlendi (ID: %d)", hookID)
	}

	if w.Client != nil {
		w.Client.Disconnect()
		logger.Info("📡 [WhatsApp] Bağlantı kapatıldı")
	}
}

func (w *Listener) Reconnect(ctx context.Context) error {
	w.Disconnect()
	time.Sleep(2 * time.Second)
	return w.Start(ctx)
}

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


func (w *Listener) IsConnected() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.isConnected && w.Client != nil && w.Client.IsConnected()
}

func (w *Listener) GetAdminJID() types.JID {
	if w.AdminPhone == "" {
		return types.EmptyJID
	}

	if len(w.AdminPhone) > 12 {
		return types.NewJID(w.AdminPhone, types.HiddenUserServer)
	}
	return types.NewJID(w.AdminPhone, types.DefaultUserServer)
}

func getBaseDir() string {
	exePath, err := os.Executable()
	if err == nil && !strings.Contains(filepath.ToSlash(exePath), "go-build") && !strings.Contains(filepath.ToSlash(exePath), "Temp") {
		return filepath.Dir(exePath)
	}
	baseDir, _ := os.Getwd()
	return baseDir
}

func (w *Listener) ValidateAdminPhone(senderPhone string) bool {
	return isAdminPhone(w.AdminPhone, senderPhone)
}

func (w *Listener) ShouldProcessMessage(chatID types.JID, senderPhone string) bool {
	if isGroupMessage(chatID) {
		logger.Debug("⚠️ [WhatsApp] Grup mesajı reddedildi: %s", chatID.String())
		return false
	}

	if isBroadcastMessage(chatID) {
		logger.Debug("⚠️ [WhatsApp] Broadcast mesajı reddedildi: %s", chatID.String())
		return false
	}

	if !isAdminPhone(w.AdminPhone, senderPhone) {
		logger.Debug("⚠️ [WhatsApp] Admin olmayan numara reddedildi: %s", senderPhone)
		return false
	}

	return true
}
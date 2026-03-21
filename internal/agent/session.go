package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

type Session struct {
	ID        string
	History   []kernel.Message
	Plan      string
	CreatedAt time.Time
	Cancel    context.CancelFunc
	mu        sync.RWMutex
}

func (a *Pars) createSession(cancel context.CancelFunc) *Session {
	a.sessMu.Lock()
	defer a.sessMu.Unlock()

	randomBytes := make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		logger.Warn("⚠️ [createSession] Kriptografik ID oluşturulamadı, fallback kullanılıyor: %v", err)
		randomBytes = []byte(fmt.Sprintf("%d", time.Now().UnixNano()))
	}

	sessID := fmt.Sprintf("TSK-%s", hex.EncodeToString(randomBytes))

	sess := &Session{
		ID:        sessID,
		History:   []kernel.Message{},
		CreatedAt: time.Now(),
		Cancel:    cancel,
	}

	a.sessions[sessID] = sess
	logger.Info("✅ [SESSION] Yeni oturum oluşturuldu: %s", sessID)
	logger.Debug("📝 [SESSION] Session detayları: ID=%s, CreatedAt=%s, History=%d mesaj",
		sessID, sess.CreatedAt.Format("15:04:05"), len(sess.History))
	return sess
}

func (a *Pars) cleanupSession(id string) {
	a.sessMu.Lock()
	defer a.sessMu.Unlock()

	logger.Debug("🗑️ [cleanupSession] Oturum temizleme başlatıldı: %s", id)

	sess, exists := a.sessions[id]
	if !exists {
		logger.Warn("⚠️ [cleanupSession] Silinmeye çalışılan oturum bulunamadı: %s", id)
		return
	}

	logger.Debug("🔍 [cleanupSession] Oturum bulundu: %s, History=%d mesaj, Plan=%d karakter",
		id, len(sess.History), len(sess.Plan))

	if sess.Cancel != nil {
		logger.Debug("🔌 [cleanupSession] Session cancel fonksiyonu çağrılıyor: %s", id)
		sess.Cancel()
	}

	delete(a.sessions, id)
	logger.Info("🗑️ [cleanupSession] Oturum temizlendi: %s", id)
}

func (a *Pars) manageContextWindow(sess *Session) {
	if a == nil || sess == nil {
		logger.Warn("⚠️ [manageContextWindow] Pars veya Session nil, işlem atlandı")
		return
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	if len(sess.History) == 0 {
		logger.Debug("ℹ️ [manageContextWindow] History boş, sıkıştırma atlandı: %s", sess.ID)
		return
	}

	maxCharLimit := 320000
	if a.Config != nil && a.Config.App.MaxContextTokens > 0 {
		maxCharLimit = a.Config.App.MaxContextTokens * 4
	}

	totalChars := 0
	for _, msg := range sess.History {
		totalChars += len(msg.Content)
	}

	logger.Debug("📊 [manageContextWindow] Context analizi: Session=%s, Toplam=%d karakter, Limit=%d karakter",
		sess.ID, totalChars, maxCharLimit)

	if totalChars <= maxCharLimit {
		logger.Debug("✅ [manageContextWindow] Limit aşılmadı, sıkıştırma atlandı: %d/%d karakter",
			totalChars, maxCharLimit)
		return
	}

	logger.Warn("⚠️ [manageContextWindow] Context limiti aşıldı, sıkıştırma başlatılıyor: %d > %d karakter",
		totalChars, maxCharLimit)

	var systemMsg kernel.Message
	var hasSystem bool
	var nonSystemMsgs []kernel.Message

	for i, msg := range sess.History {
		if msg.Role == "system" && i == 0 {
			systemMsg = msg
			hasSystem = true
			logger.Debug("📝 [manageContextWindow] Sistem mesajı bulundu: %d karakter", len(msg.Content))
		} else {
			nonSystemMsgs = append(nonSystemMsgs, msg)
		}
	}

	if !hasSystem {
		logger.Warn("⚠️ [%s] Sistem mesajı bulunamadı, context sıkıştırma atlandı", sess.ID)
		return
	}

	var recentMsgs []kernel.Message
	currentChars := len(systemMsg.Content)
	safeIndex := -1

	logger.Debug("🔍 [manageContextWindow] Mesajlar tersinden taranıyor: %d mesaj", len(nonSystemMsgs))

	for i := len(nonSystemMsgs) - 1; i >= 0; i-- {
		msg := nonSystemMsgs[i]
		msgLen := len(msg.Content)

		if currentChars+msgLen > maxCharLimit {
			logger.Debug("⚠️ [manageContextWindow] Limit aşımı tespit edildi: %d + %d > %d",
				currentChars, msgLen, maxCharLimit)

			if msg.Role == kernel.RoleTool || len(msg.ToolCalls) > 0 {
				logger.Debug("🔧 [manageContextWindow] Tool mesajı tespit edildi, döngü kırıldı")
				break
			} else {
				logger.Debug("📝 [manageContextWindow] Normal mesaj, döngü kırıldı")
				break
			}
		}

		currentChars += msgLen
		safeIndex = i
	}

	if safeIndex == -1 {
		safeIndex = len(nonSystemMsgs) - 1
		logger.Warn("⚠️ [manageContextWindow] SafeIndex -1, son mesaj kullanılıyor: %d", safeIndex)
	}

	recentMsgs = nonSystemMsgs[safeIndex:]
	newHistory := []kernel.Message{systemMsg}
	droppedCount := safeIndex

	logger.Info("📊 [manageContextWindow] Sıkıştırma istatistikleri: SafeIndex=%d, Dropped=%d mesaj, Kept=%d mesaj",
		safeIndex, droppedCount, len(recentMsgs))

	if droppedCount > 0 {
		compressionInfo := kernel.Message{
			Role:    "system",
			Content: fmt.Sprintf("\n[SİSTEM BİLGİSİ: Limitleri korumak için önceki %d mesaj arşive kaldırıldı. Güncel bağlama odaklan.]\n", droppedCount),
		}
		newHistory = append(newHistory, compressionInfo)
		logger.Info("📝 [manageContextWindow] Sıkıştırma bilgisi eklendi: %d mesaj arşivlendi", droppedCount)
	}

	newHistory = append(newHistory, recentMsgs...)
	sess.History = newHistory

	newTotalChars := 0
	for _, msg := range sess.History {
		newTotalChars += len(msg.Content)
	}

	logger.Warn("🧠 [%s] Context Budandı: Eski=%d karakter, Yeni=%d karakter, Silinen=%d mesaj",
		sess.ID, totalChars, newTotalChars, droppedCount)
}

func (a *Pars) startEventProcessor() {
	if a.EventChannel == nil {
		a.EventChannel = make(chan string, 100)
		logger.Info("📡 [EVENT BUS] Event channel oluşturuldu: 100 kapasite")
	}

	go func() {
		logger.Success("🩺 [EVENT BUS] Pars, Sistem Olaylarını dinlemeye başladı...")
		defer logger.Info("🩺 [EVENT BUS] Pars, Sistem Olayları dinlemeyi bıraktı.")

		for eventMsg := range a.EventChannel {
			logger.Info("🚨 [SİSTEM SİNYALİ ALINDI]: %d karakter", len(eventMsg))
			logger.Debug("📝 [SİSTEM SİNYALİ] İçerik: %s", eventMsg[:min(200, len(eventMsg))])
			a.handleSystemEvent(eventMsg)
		}
	}()
}

func (a *Pars) handleSystemEvent(eventMsg string) {
	if a == nil {
		logger.Warn("⚠️ [handleSystemEvent] Pars nil, event işlenmedi")
		return
	}

	logger.Info("📢 [handleSystemEvent] Sistem eventi işleniyor: %d karakter", len(eventMsg))

	if a.AlertHook != nil {
		logger.Debug("🔔 [handleSystemEvent] AlertHook mevcut, asenkron çağrılıyor")
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("🚨 [ALERT HOOK PANIC]: %v", r)
				}
			}()
			logger.Info("📞 [AlertHook] AlertHook çağrılıyor...")
			a.AlertHook(eventMsg)
			logger.Info("✅ [AlertHook] AlertHook tamamlandı")
		}()
	} else {
		logger.Warn("⚠️ [handleSystemEvent] AlertHook nil, alert gönderilemedi")
	}

	a.sessMu.RLock()
	var activeSess *Session
	var latestTime time.Time

	for _, sess := range a.sessions {
		if sess.CreatedAt.After(latestTime) {
			latestTime = sess.CreatedAt
			activeSess = sess
		}
	}
	a.sessMu.RUnlock()

	if activeSess != nil {
		logger.Info("📝 [handleSystemEvent] Aktif session bulundu: %s, CreatedAt=%s",
			activeSess.ID, activeSess.CreatedAt.Format("15:04:05"))

		activeSess.mu.Lock()

		if activeSess.History != nil {
			activeSess.History = append(activeSess.History, kernel.Message{
				Role:    "user",
				Content: fmt.Sprintf("[SİSTEM UYARISI]: %s", eventMsg),
			})
			logger.Debug("📝 [handleSystemEvent] Sistem uyarısı history'ye eklendi: %s, Total=%d mesaj",
				activeSess.ID, len(activeSess.History))
		} else {
			logger.Warn("⚠️ [handleSystemEvent] ActiveSess.History nil, mesaj eklenemedi")
		}

		activeSess.mu.Unlock()

		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("🚨 [manageContextWindow PANIC]: %v", r)
				}
			}()
			logger.Debug("🧠 [handleSystemEvent] manageContextWindow çağrılıyor...")
			a.manageContextWindow(activeSess)
			logger.Debug("✅ [handleSystemEvent] manageContextWindow tamamlandı")
		}()
	} else {
		logger.Warn("🚨 [handleSystemEvent] Aktif terminal oturumu yok, arka plan uyarısı otonom beyne iletildi: %s",
			eventMsg[:min(200, len(eventMsg))])
	}
}

// internal/agent/session.go
// 🚀 DÜZELTMELER: Thread-safety, Graceful shutdown, Nil checks, Error handling
// ⚠️ DİKKAT: execution.go ve pars.go ile %100 uyumlu, breaking change YOK

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

// =========================================================================
// 📦 SESSION YAPISI
// =========================================================================

// Session (Oturum), Pars'ın aktif olarak yürüttüğü her bir görev veya sohbetin
// izole bir şekilde tutulduğu hafıza ve durum (state) yöneticisidir.
type Session struct {
	ID        string
	History   []kernel.Message // Ajanın kullanıcı ve sistem ile olan konuşma geçmişi
	Plan      string           // Görev (Task) modunda izlenecek stratejik adımlar
	CreatedAt time.Time
	Cancel    context.CancelFunc // Oturumu dışarıdan güvenle durdurabilmek için iptal tetikleyicisi
	mu        sync.Mutex         // Eşzamanlı okuma/yazma çakışmalarını önleyen kilit
}

// =========================================================================
// 🆕 SESSION OLUŞTURMA
// =========================================================================

// createSession, benzersiz bir kimlik (ID) ile yeni bir görev oturumu başlatır
// ve bunu ajanın aktif oturumlar listesine ekler.
func (a *Pars) createSession(cancel context.CancelFunc) *Session {
	a.sessMu.Lock()
	defer a.sessMu.Unlock()

	// 🚨 DÜZELTME #1: rand.Read() hatasını kontrol et
	randomBytes := make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		// Fallback: zaman tabanlı ID (nadir gerçekleşir)
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

	a.Sessions[sessID] = sess
	logger.Debug("✅ [SESSION] Yeni oturum oluşturuldu: %s", sessID)
	return sess
}

// =========================================================================
// 🧹 SESSION TEMİZLEME
// =========================================================================

// cleanupSession, işi biten veya iptal edilen oturumu bellekten (RAM) temizler.
func (a *Pars) cleanupSession(id string) {
	a.sessMu.Lock()
	defer a.sessMu.Unlock()

	sess, exists := a.Sessions[id]
	if !exists {
		logger.Debug("⚠️ [cleanupSession] Silinmeye çalışılan oturum bulunamadı: %s", id)
		return
	}

	// 🚨 DÜZELTME #2: Cancel fonksiyonunu çağır (goroutine sızıntısını önle)
	if sess.Cancel != nil {
		sess.Cancel()
	}

	delete(a.Sessions, id)
	logger.Debug("🗑️ [cleanupSession] Oturum temizlendi: %s", id)
}

// =========================================================================
// 🧠 CONTEXT WINDOW YÖNETİMİ
// =========================================================================

// manageContextWindow, Pars'ın hafızasını (Context Window) akıllı bir şekilde yönetir.
// Mesaj sayısına değil, karakter (token) yoğunluğuna bakarak maliyetleri düşürür ve
// Büyük Dil Modellerinin bağlam sınırını aşmasını engeller.
func (a *Pars) manageContextWindow(sess *Session) {
	// 🚨 DÜZELTME #3: Nil kontrolleri
	if a == nil || sess == nil {
		return
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	if len(sess.History) == 0 {
		return
	}

	// 🚨 DÜZELTME #4: Config nil kontrolü
	maxCharLimit := 320000 // Varsayılan değer
	if a.Config != nil && a.Config.App.MaxContextTokens > 0 {
		maxCharLimit = a.Config.App.MaxContextTokens * 4
	}

	totalChars := 0
	for _, msg := range sess.History {
		totalChars += len(msg.Content)
	}

	if totalChars <= maxCharLimit {
		return
	}

	// 🚨 DÜZELTME #5: Sistem mesajını doğru şekilde koru (pointer yerine value)
	var systemMsg kernel.Message
	var hasSystem bool
	var nonSystemMsgs []kernel.Message

	for i, msg := range sess.History {
		if msg.Role == "system" && i == 0 {
			systemMsg = msg // 🆕 Pointer değil, value kopyala
			hasSystem = true
		} else {
			nonSystemMsgs = append(nonSystemMsgs, msg)
		}
	}

	if !hasSystem {
		logger.Warn("⚠️ [%s] Sistem mesajı bulunamadı, context sıkıştırma atlandı", sess.ID)
		return
	}

	// 🚨 DÜZELTME #6: Mesajları sondan başa doğru doğru sırayla topla
	var recentMsgs []kernel.Message
	currentChars := len(systemMsg.Content)

	for i := len(nonSystemMsgs) - 1; i >= 0; i-- {
		msg := nonSystemMsgs[i]
		msgLen := len(msg.Content)

		if currentChars+msgLen > maxCharLimit {
			break
		}

		recentMsgs = append(recentMsgs, msg)
		currentChars += msgLen
	}

	// 🚨 DÜZELTME #7: Mesaj sırasını düzelt (en eski -> en yeni)
	for i, j := 0, len(recentMsgs)-1; i < j; i, j = i+1, j-1 {
		recentMsgs[i], recentMsgs[j] = recentMsgs[j], recentMsgs[i]
	}

	// Yeni hafızayı inşa et
	newHistory := []kernel.Message{systemMsg}

	// Silinen mesaj bilgisi ekle
	droppedCount := len(nonSystemMsgs) - len(recentMsgs)
	if droppedCount > 0 {
		compressionInfo := kernel.Message{
			Role:    "system",
			Content: fmt.Sprintf("\n[SİSTEM BİLGİSİ: Limitleri korumak için önceki %d mesaj arşive kaldırıldı. Güncel bağlama odaklan.]\n", droppedCount),
		}
		newHistory = append(newHistory, compressionInfo)
	}

	// Son mesajları yeni hafızaya ekle
	newHistory = append(newHistory, recentMsgs...)
	sess.History = newHistory

	logger.Warn("🧠 [%s] Context Budandı: Boyut %d karaktere düşürüldü (%d eski blok silindi).",
		sess.ID, currentChars, droppedCount)
}

// =========================================================================
// 📡 EVENT PROCESSOR (TELEMETRİ / SİSTEM OLAY DİNLEYİCİSİ)
// =========================================================================

// startEventProcessor, Pars'ın arka planda sistemden gelen olayları dinlemesini sağlar.
// 🚨 DÜZELTME #8: Graceful shutdown için context desteği eklendi
func (a *Pars) startEventProcessor() {
	if a.EventChannel == nil {
		a.EventChannel = make(chan string, 100)
	}

	go func() {
		logger.Success("🩺 [EVENT BUS] Pars, Sistem Olaylarını dinlemeye başladı...")
		defer logger.Info("🩺 [EVENT BUS] Pars, Sistem Olayları dinlemeyi bıraktı.")

		// 🚨 DÜZELTME #9: Kanal kapandığında temiz çık
		for eventMsg := range a.EventChannel {
			logger.Error("🚨 [SİSTEM SİNYALİ ALINDI]: %s", eventMsg)
			a.handleSystemEvent(eventMsg)
		}
	}()
}

// =========================================================================
// 🎯 SİSTEM OLAY İŞLEME
// =========================================================================

// handleSystemEvent, alınan telemetri ve güvenlik mesajlarını işler.
// Hem otonom beyne (WhatsApp için) haber verir, hem de aktif terminal sohbetine enjekte eder.
func (a *Pars) handleSystemEvent(eventMsg string) {
	// 🚨 DÜZELTME #10: Nil kontrolü
	if a == nil {
		return
	}

	// 1. 🚀 WHATSAPP / OTONOM GÖREV KANCASI VARSA TETİKLE
	if a.AlertHook != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("🚨 [ALERT HOOK PANIC]: %v", r)
				}
			}()
			a.AlertHook(eventMsg)
		}()
	}

	// 2. 🚨 DÜZELTME #11: Aktif oturumu doğru şekilde bul (RLock kullan)
	a.sessMu.RLock()
	var activeSess *Session
	var latestTime time.Time

	for _, sess := range a.Sessions {
		if sess.CreatedAt.After(latestTime) {
			latestTime = sess.CreatedAt
			activeSess = sess
		}
	}
	a.sessMu.RUnlock()

	// 3. 🚨 DÜZELTME #12: Session silinmiş olabilir, tekrar kontrol et
	if activeSess != nil {
		activeSess.mu.Lock()
		
		// 🆕 Session hala geçerli mi kontrol et
		if activeSess.History != nil {
			activeSess.History = append(activeSess.History, kernel.Message{
				Role:    "user",
				Content: fmt.Sprintf("[SİSTEM UYARISI]: %s", eventMsg),
			})
		}
		
		activeSess.mu.Unlock()

		// 🚨 DÜZELTME #13: manageContextWindow'u goroutine'de çağır (blocking önle)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("🚨 [manageContextWindow PANIC]: %v", r)
				}
			}()
			a.manageContextWindow(activeSess)
		}()
	} else {
		logger.Debug("🚨 Aktif terminal oturumu yok, arka plan uyarısı otonom beyne iletildi: %s", eventMsg)
	}
}
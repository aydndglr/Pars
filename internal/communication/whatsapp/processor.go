// internal/communication/whatsapp/processor.go
// 🚀 DÜZELTMELER: Nil checks, Panic recovery, Error handling, Logging
// ⚠️ DİKKAT: client.go ve utils.go ile %100 uyumlu

package whatsapp

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/agent"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// EventHandler: WhatsApp olaylarını işler
func (w *Listener) EventHandler(evt interface{}) {
	// 🚨 DÜZELTME #1: Nil check
	if w == nil {
		return
	}

	switch v := evt.(type) {
	case *events.Message:
		w.HandleMessage(v)
	}
}

// HandleMessage: Gelen WhatsApp mesajlarını işler ve Pars'a iletir
func (w *Listener) HandleMessage(evt *events.Message) {
	// 🚨 DÜZELTME #2: Nil kontrolleri
	if w == nil || evt == nil || evt.Message == nil {
		return
	}

	// Kendi gönderdiğimiz mesajları yoksay
	if evt.Info.IsFromMe {
		return
	}

	var msgText string
	var images []string

	// 1. İçerik Ayıklama (Metin)
	msgText = evt.Message.GetConversation()
	if msgText == "" && evt.Message.GetExtendedTextMessage() != nil {
		msgText = evt.Message.GetExtendedTextMessage().GetText()
	}

	// Temizlik
	trimmedMsg := strings.TrimSpace(msgText)

	// ========================================================================
	// 🔐 ADMİN DOĞRULAMA (OTP) KONTROLÜ
	// ========================================================================
	if w.SetupKey != "" && trimmedMsg == w.SetupKey {
		// 🎯 EŞLEŞME BAŞARILI!
		capturedID := evt.Info.Sender.ToNonAD().User

		logger.Success("🛡️ OTP Doğrulandı! Yeni yönetici kimliği: %s", capturedID)

		w.AdminPhone = capturedID
		w.SetupKey = "" // Kodu imha et

		// Config dosyasına mühürle
		w.autoUpdateConfig(capturedID)

		// Kullanıcıya başarı mesajı gönder
		w.SendReply(evt.Info.Chat, "✅ *ERİŞİM ONAYLANDI*\nParsOS protokolleri sizin kimliğinize mühürlendi. Artık tüm sistem kontrolü sizde, Efendim.")

		// Live logging'i başlat
		w.setupLiveLogging(capturedID)
		return
	}

	// ========================================================================
	// 🛡️ ADMİN FİLTRESİ (GÜVENLİK DUVARI)
	// ========================================================================
	sender := evt.Info.Sender.User
	if w.AdminPhone != "" && !strings.Contains(sender, w.AdminPhone) {
		// Yönetici belirlenmişse ve mesaj yöneticiden gelmiyorsa Pars tepki vermez.
		return
	}

	// 🔄 Alıntılanan Mesajı Yakala
	var quotedText string
	if ext := evt.Message.GetExtendedTextMessage(); ext != nil && ext.GetContextInfo() != nil {
		if quotedMsg := ext.GetContextInfo().GetQuotedMessage(); quotedMsg != nil {
			if conv := quotedMsg.GetConversation(); conv != "" {
				quotedText = conv
			} else if qExt := quotedMsg.GetExtendedTextMessage(); qExt != nil {
				quotedText = qExt.GetText()
			}

			if quotedText != "" {
				if len(quotedText) > 500 {
					quotedText = quotedText[:500] + "..."
				}
				msgText = fmt.Sprintf("[Bağlam - Kullanıcı şu mesaja yanıt veriyor: \"%s\"]\n\nYeni Mesaj: %s", quotedText, msgText)
				logger.Info("🔄 Alıntı yakalandı ve bağlama eklendi.")
			}
		}
	}

	// 2. İçerik Ayıklama (Görsel)
	imgMsg := evt.Message.GetImageMessage()
	if imgMsg != nil {
		if msgText == "" && imgMsg.Caption != nil {
			msgText = *imgMsg.Caption
		}

		downloadCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		data, err := w.Client.Download(downloadCtx, imgMsg)
		cancel()

		if err != nil {
			logger.Error("❌ Resim indirilemedi: %v", err)
			// 🚨 DÜZELTME #3: Kullanıcıya hata bildir (opsiyonel, sessiz kalabilir)
		} else {
			// 🚨 DÜZELTME #4: Base64 encoding hatası kontrolü
			encoded := base64.StdEncoding.EncodeToString(data)
			if encoded != "" {
				images = append(images, encoded)
				logger.Info("📸 Görsel yakalandı.")
			}
		}
	}

	if msgText == "" && len(images) == 0 {
		return
	}

	// 3. UI İşlemleri (WhatsApp'ta "Yazıyor..." ibaresi)
	w.MarkAsRead(evt)
	w.SetPresence(evt.Info.Chat, types.ChatPresenceComposing)

	// 4. Ajanı Çalıştır
	go func() {
		// 🚨 DÜZELTME #5: Panic recovery ekle (goroutine crash önleme)
		defer func() {
			if r := recover(); r != nil {
				logger.Error("🚨 [WhatsApp] Message handler panic: %v", r)
				// Kullanıcıya hata mesajı gönder
				if w != nil && evt != nil {
					w.SendReply(evt.Info.Chat, "❌ *[Sistem Hatası]*\nBeklenmeyen bir hata oluştu. Lütfen tekrar deneyin.")
				}
			}
		}()

		timeoutMin := 15

		if parsAgent, ok := w.Agent.(*agent.Pars); ok {
			if parsAgent != nil && parsAgent.Config != nil {
				if parsAgent.Config.App.TimeoutMinutes > 0 {
					timeoutMin = parsAgent.Config.App.TimeoutMinutes
				}
			}
		}

		logger.Debug("⏳ WhatsApp Görev zaman aşımı süresi: %d dakika", timeoutMin)

		timeoutDuration := time.Duration(timeoutMin) * time.Minute
		ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration)
		defer cancel()

		// ====================================================================
		// 🚀 BİLİNÇ YAMASI: WHATSAPP OTURUM İZOLASYONU
		// ====================================================================
		senderID := evt.Info.Sender.ToNonAD().User
		// 🚨 DÜZELTME #6: senderID boşsa fallback
		if senderID == "" {
			senderID = "unknown"
		}
		sessionID := fmt.Sprintf("WA-%s", senderID)

		execCtx := context.WithValue(ctx, "client_task_id", sessionID)
		// ====================================================================

		// 🚨 DÜZELTME #7: Agent nil kontrolü
		if w.Agent == nil {
			logger.Error("❌ [WhatsApp] Agent nil! Mesaj işlenemedi.")
			w.SendReply(evt.Info.Chat, "❌ *[Sistem Hatası]*\nAjan başlatılamadı.")
			return
		}

		// Ajanı çalıştır
		response, err := w.Agent.Run(execCtx, msgText, images)

		// İşlem bitince "Yazıyor..." ibaresini kaldır
		w.SetPresence(evt.Info.Chat, types.ChatPresencePaused)

		// 🚨 DÜZELTME #8: Response validation
		if err != nil {
			logger.Error("❌ [WhatsApp] Agent.Run hatası: %v", err)
			w.SendReply(evt.Info.Chat, fmt.Sprintf("🚨 *[Sistem Hatası]*\nGörev tamamlanamadı:\n%v", err))
		} else if response == "" {
			logger.Warn("⚠️ [WhatsApp] Agent boş response döndürdü")
			w.SendReply(evt.Info.Chat, "⚠️ *[Bilgi]*\nİşlem tamamlandı ancak yanıt alınamadı.")
		} else {
			w.SendReply(evt.Info.Chat, response)
		}
	}()
}
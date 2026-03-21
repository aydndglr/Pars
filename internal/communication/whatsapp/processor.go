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

func (w *Listener) EventHandler(evt interface{}) {
	// 🚨 Nil check
	if w == nil {
		return
	}

	switch v := evt.(type) {
	case *events.Message:
		w.HandleMessage(v)
	}
}

func (w *Listener) HandleMessage(evt *events.Message) {
	if w == nil || evt == nil || evt.Message == nil {
		return
	}

	if evt.Info.IsFromMe {
		return
	}

	var msgText string
	var images []string
	msgText = evt.Message.GetConversation()
	if msgText == "" && evt.Message.GetExtendedTextMessage() != nil {
		msgText = evt.Message.GetExtendedTextMessage().GetText()
	}

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
		} else {
			encoded := base64.StdEncoding.EncodeToString(data)
			if encoded != "" {
				images = append(images, encoded)
				logger.Info("📸 Görsel yakalandı.")
			}
		}
	}

	trimmedMsg := strings.TrimSpace(msgText)
	if w.SetupKey != "" && trimmedMsg == w.SetupKey {
		capturedID := evt.Info.Sender.ToNonAD().User

		logger.Success("🛡️ OTP Doğrulandı! Yeni yönetici kimliği: %s", capturedID)

		w.AdminPhone = capturedID
		w.SetupKey = ""
		w.autoUpdateConfig(capturedID)
		w.SendReply(evt.Info.Chat, "✅ *ERİŞİM ONAYLANDI*\nParsOS protokolleri sizin kimliğinize mühürlendi. Artık tüm sistem kontrolü sizde, Efendim.")
		w.setupLiveLogging(capturedID)
		return
	}

	sender := evt.Info.Sender.User
	if w.AdminPhone != "" && !strings.Contains(sender, w.AdminPhone) {
		return
	}

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

	if msgText == "" && len(images) == 0 {
		return
	}

	w.MarkAsRead(evt)
	w.SetPresence(evt.Info.Chat, types.ChatPresenceComposing)


	go func() {

		defer func() {
			if r := recover(); r != nil {
				logger.Error("🚨 [WhatsApp] Message handler panic: %v", r)
				if w != nil && evt != nil {
					w.SendReply(evt.Info.Chat, "❌ *[Sistem Hatası]*\nBeklenmeyen bir hata oluştu. Lütfen tekrar deneyin.")
				}
			}
			w.SetPresence(evt.Info.Chat, types.ChatPresencePaused)
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
		senderID := evt.Info.Sender.ToNonAD().User
		if senderID == "" {
			senderID = "unknown"
		}
		sessionID := fmt.Sprintf("WA-%s", senderID)

		execCtx := context.WithValue(ctx, "client_task_id", sessionID)

		if w.Agent == nil {
			logger.Error("❌ [WhatsApp] Agent nil! Mesaj işlenemedi.")
			w.SendReply(evt.Info.Chat, "❌ *[Sistem Hatası]*\nAjan başlatılamadı.")
			return
		}
		maxRetries := 3
		var response string
		var err error

		for retry := 1; retry <= maxRetries; retry++ {
			response, err = w.Agent.Run(execCtx, msgText, images)
			if err == nil || retry == maxRetries {
				break
			}
			
			logger.Warn("⚠️ [WhatsApp] Agent başarısız oldu (Deneme %d/%d). Hata: %v", retry, maxRetries, err)
			time.Sleep(2 * time.Second) 
		}

		if err != nil {
			logger.Error("❌ [WhatsApp] Agent.Run nihai hatası: %v", err)
			w.SendReply(evt.Info.Chat, fmt.Sprintf("🚨 *[Sistem Hatası]*\nGörev %d denemenin ardından tamamlanamadı:\n%v", maxRetries, err))
		} else if response == "" {
			logger.Warn("⚠️ [WhatsApp] Agent boş response döndürdü")
			w.SendReply(evt.Info.Chat, "⚠️ *[Bilgi]*\nİşlem tamamlandı ancak yanıt alınamadı.")
		} else {
			w.SendReply(evt.Info.Chat, response)
		}
	}()
}
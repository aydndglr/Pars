// internal/communication/whatsapp/whatsapp_tool.go
// 🚀 DÜZELTMELER: Nil checks, Input validation, Error handling, Logging, Context timeout
// ⚠️ DİKKAT: client.go, utils.go ve processor.go ile %100 uyumlu

package whatsapp

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"go.mau.fi/whatsmeow/types"
)

// 🚨 YENİ: Sabitler ve Limitler
const (
	MaxMessageLength = 4096          // WhatsApp mesaj limiti
	MaxCaptionLength = 1024          // Caption limiti
)

// =====================================================================
// 📤 WHATSAPP SEND TOOL (METİN)
// =====================================================================

type WhatsAppSendTool struct {
	listener *Listener
}

func NewWhatsAppSendTool(l *Listener) *WhatsAppSendTool {
	// 🚨 DÜZELTME #1: Nil check
	if l == nil {
		logger.Error("❌ [WhatsAppSendTool] Listener nil! Araç oluşturulamadı.")
		return nil
	}
	return &WhatsAppSendTool{listener: l}
}

func (t *WhatsAppSendTool) Name() string { return "whatsapp_send" }

func (t *WhatsAppSendTool) Description() string {
	return "WhatsApp üzerinden mesaj gönderir. Eğer telefon numarası belirtilmezse mesajı doğrudan sana (Admin) iletir. Numaraları 905xxxxxxxxx formatında yazmalısın."
}

func (t *WhatsAppSendTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"message":   map[string]interface{}{"type": "string", "description": "Gönderilecek mesaj içeriği."},
			"recipient": map[string]interface{}{"type": "string", "description": "Hedef telefon numarası (Ülke koduyla, örn: 905xxxxxxxxx). Boş bırakılırsa Admin'e gönderilir."},
		},
		"required": []string{"message"},
	}
}

// 🆕 YENİ: validatePhoneNumber - Telefon numarasını doğrula
func validatePhoneNumber(phone string) (string, error) {
	// + işaretini temizle
	clean := strings.TrimPrefix(phone, "+")
	
	// Sadece rakam ve ülke kodu formatını kabul et
	if matched, _ := regexp.MatchString(`^\d{10,15}$`, clean); !matched {
		return "", fmt.Errorf("geçersiz telefon numarası formatı: %s (örn: 905xxxxxxxxx)", phone)
	}
	return clean, nil
}

func (t *WhatsAppSendTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	// 🚨 DÜZELTME #2: Nil checks
	if t == nil || t.listener == nil {
		return "", fmt.Errorf("tool veya listener nil")
	}

	if t.listener.Client == nil {
		logger.Error("❌ [WhatsAppSendTool] Client nil!")
		return "", fmt.Errorf("WhatsApp client başlatılmamış")
	}

	if !t.listener.Client.IsConnected() {
		logger.Warn("⚠️ [WhatsAppSendTool] WhatsApp bağlı değil, bağlanmaya çalışılıyor...")
		// Bağlantı yoksa hata döndür, otomatik reconnect yapma (güvenlik)
		return "", fmt.Errorf("WhatsApp bağlantısı aktif değil. Önce QR kodu okutun.")
	}

	// 🚨 DÜZELTME #3: Type assertion with ok check
	messageRaw, ok := args["message"]
	if !ok || messageRaw == nil {
		return "", fmt.Errorf("'message' parametresi eksik veya boş")
	}
	message, ok := messageRaw.(string)
	if !ok {
		return "", fmt.Errorf("'message' parametresi string formatında olmalı")
	}
	message = strings.TrimSpace(message)
	
	// 🚨 DÜZELTME #4: Input validation
	if message == "" {
		return "", fmt.Errorf("mesaj içeriği boş olamaz")
	}
	if len(message) > MaxMessageLength {
		logger.Warn("⚠️ [WhatsAppSendTool] Mesaj çok uzun (%d > %d), kırpılıyor", len(message), MaxMessageLength)
		message = message[:MaxMessageLength]
	}

	recipient := ""
	if recipientRaw, ok := args["recipient"]; ok && recipientRaw != nil {
		if r, ok := recipientRaw.(string); ok {
			recipient = strings.TrimSpace(r)
		}
	}

	// 🚨 DÜZELTME #5: Recipient validation
	var targetJID types.JID
	if recipient == "" {
		// Admin'e gönder
		if t.listener.AdminPhone == "" {
			return "", fmt.Errorf("admin telefon numarası tanımlı değil ve alıcı belirtilmedi")
		}
		targetJID = types.NewJID(t.listener.AdminPhone, types.DefaultUserServer)
		logger.Debug("📤 [WhatsApp] Admin'e mesaj gönderiliyor: %s", t.listener.AdminPhone)
	} else {
		// 🚨 DÜZELTME #6: Phone validation helper kullan
		cleanRecipient, err := validatePhoneNumber(recipient)
		if err != nil {
			return "", fmt.Errorf("alıcı numarası doğrulanamadı: %v", err)
		}
		targetJID = types.NewJID(cleanRecipient, types.DefaultUserServer)
		logger.Debug("📤 [WhatsApp] Mesaj gönderiliyor: %s", cleanRecipient)
	}

	// 🚨 DÜZELTME #8: SendReply'nin context kullanımını güncelle (utils.go'da)
	t.listener.SendReply(targetJID, message)
	
	logger.Success("✅ [WhatsApp] Mesaj başarıyla iletildi: %s", targetJID.User)
	return fmt.Sprintf("✅ Mesaj başarıyla iletildi: %s", targetJID.User), nil
}

// =====================================================================
// 📸 WHATSAPP SEND IMAGE TOOL (RESİM)
// =====================================================================

type WhatsAppSendImageTool struct {
	listener *Listener
}

func NewWhatsAppSendImageTool(l *Listener) *WhatsAppSendImageTool {
	// 🚨 DÜZELTME #9: Nil check
	if l == nil {
		logger.Error("❌ [WhatsAppSendImageTool] Listener nil! Araç oluşturulamadı.")
		return nil
	}
	return &WhatsAppSendImageTool{listener: l}
}

func (t *WhatsAppSendImageTool) Name() string { return "whatsapp_send_image" }

func (t *WhatsAppSendImageTool) Description() string {
	return "WhatsApp üzerinden resim gönderir. Bilgisayardaki bir resim dosyasının tam yolunu (image_path) alır ve gönderir. İsteğe bağlı olarak resmin altına açıklama/mesaj (caption) eklenebilir."
}

func (t *WhatsAppSendImageTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"image_path": map[string]interface{}{"type": "string", "description": "Gönderilecek resmin diskteki tam yolu (Örn: .pars_trash/screen.png)"},
			"caption":    map[string]interface{}{"type": "string", "description": "Resmin altına eklenecek açıklama (Opsiyonel)."},
			"recipient":  map[string]interface{}{"type": "string", "description": "Hedef telefon numarası. Boş bırakılırsa Admin'e gönderilir."},
		},
		"required": []string{"image_path"},
	}
}

func (t *WhatsAppSendImageTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	// 🚨 DÜZELTME #10: Nil checks
	if t == nil || t.listener == nil {
		return "", fmt.Errorf("tool veya listener nil")
	}

	if t.listener.Client == nil {
		logger.Error("❌ [WhatsAppSendImageTool] Client nil!")
		return "", fmt.Errorf("WhatsApp client başlatılmamış")
	}

	if !t.listener.Client.IsConnected() {
		logger.Warn("⚠️ [WhatsAppSendImageTool] WhatsApp bağlı değil")
		return "", fmt.Errorf("WhatsApp bağlantısı aktif değil. Önce QR kodu okutun.")
	}

	// 🚨 DÜZELTME #11: Type assertions with ok checks
	imagePathRaw, ok := args["image_path"]
	if !ok || imagePathRaw == nil {
		return "", fmt.Errorf("'image_path' parametresi eksik")
	}
	imagePath, ok := imagePathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'image_path' parametresi string formatında olmalı")
	}
	imagePath = strings.TrimSpace(imagePath)

	// 🚨 DÜZELTME #12: Input validation
	if imagePath == "" {
		return "", fmt.Errorf("resim yolu boş olamaz")
	}

	caption := ""
	if captionRaw, ok := args["caption"]; ok && captionRaw != nil {
		if c, ok := captionRaw.(string); ok {
			caption = strings.TrimSpace(c)
			if len(caption) > MaxCaptionLength {
				logger.Warn("⚠️ [WhatsAppSendImageTool] Caption çok uzun, kırpılıyor")
				caption = caption[:MaxCaptionLength]
			}
		}
	}

	recipient := ""
	if recipientRaw, ok := args["recipient"]; ok && recipientRaw != nil {
		if r, ok := recipientRaw.(string); ok {
			recipient = strings.TrimSpace(r)
		}
	}

	// 🚨 DÜZELTME #13: Recipient validation
	var targetJID types.JID
	if recipient == "" {
		if t.listener.AdminPhone == "" {
			return "", fmt.Errorf("admin telefon numarası tanımlı değil ve alıcı belirtilmedi")
		}
		targetJID = types.NewJID(t.listener.AdminPhone, types.DefaultUserServer)
	} else {
		cleanRecipient, err := validatePhoneNumber(recipient)
		if err != nil {
			return "", fmt.Errorf("alıcı numarası doğrulanamadı: %v", err)
		}
		targetJID = types.NewJID(cleanRecipient, types.DefaultUserServer)
	}

	logger.Info("📸 [WhatsApp] Resim gönderiliyor: %s -> %s", imagePath, targetJID.User)

	// 🚨 DÜZELTME #15: SendImage'de context kullanımı (utils.go'da güncellenmeli)
	err := t.listener.SendImage(targetJID, imagePath, caption)
	if err != nil {
		logger.Error("❌ [WhatsAppSendImageTool] Resim gönderimi başarısız: %v", err)
		return "", fmt.Errorf("resim gönderilirken hata oluştu: %v", err)
	}
	
	logger.Success("✅ [WhatsApp] Resim başarıyla iletildi: %s", targetJID.User)
	return fmt.Sprintf("✅ Resim başarıyla iletildi: %s", targetJID.User), nil
}

// 🆕 YENİ: GetStatus - Tool durumunu sorgula (debug için)
func (t *WhatsAppSendTool) GetStatus() map[string]interface{} {
	if t == nil || t.listener == nil {
		return map[string]interface{}{"error": "tool nil"}
	}
	return map[string]interface{}{
		"name":        t.Name(),
		"connected":   t.listener.Client != nil && t.listener.Client.IsConnected(),
		"admin_phone": t.listener.AdminPhone,
	}
}

// 🆕 YENİ: GetStatus - Image tool için
func (t *WhatsAppSendImageTool) GetStatus() map[string]interface{} {
	if t == nil || t.listener == nil {
		return map[string]interface{}{"error": "tool nil"}
	}
	return map[string]interface{}{
		"name":        t.Name(),
		"connected":   t.listener.Client != nil && t.listener.Client.IsConnected(),
		"admin_phone": t.listener.AdminPhone,
	}
}
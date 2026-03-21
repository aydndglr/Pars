package whatsapp

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

const (
	MaxMessageLength = 4096         
	MaxCaptionLength = 1024         
)


type WhatsAppSendTool struct {
	listener *Listener
}

func NewWhatsAppSendTool(l *Listener) *WhatsAppSendTool {
	if l == nil {
		logger.Error("❌ [WhatsAppSendTool] Listener nil! Araç oluşturulamadı.")
		return nil
	}
	return &WhatsAppSendTool{listener: l}
}

func (t *WhatsAppSendTool) Name() string { return "whatsapp_send" }

func (t *WhatsAppSendTool) Description() string {
	return "WhatsApp üzerinden SADECE yöneticiye (Admin) mesaj gönderir. Güvenlik gereği başka numaralara gönderim yetkisi yoktur."
}

func (t *WhatsAppSendTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"message":   map[string]interface{}{"type": "string", "description": "Gönderilecek mesaj içeriği."},
			"recipient": map[string]interface{}{"type": "string", "description": "Güvenlik gereği SADECE admin numarası kabul edilir. Boş bırakılabilir."},
		},
		"required": []string{"message"},
	}
}


func validatePhoneNumber(phone string) (string, error) {
	clean := strings.TrimPrefix(phone, "+")
	
	if matched, _ := regexp.MatchString(`^\d{10,15}$`, clean); !matched {
		return "", fmt.Errorf("geçersiz telefon numarası formatı: %s (örn: 905xxxxxxxxx)", phone)
	}
	return clean, nil
}

func (t *WhatsAppSendTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {

	if t == nil || t.listener == nil {
		return "", fmt.Errorf("tool veya listener nil")
	}

	if !t.listener.IsConnected() {
		logger.Warn("⚠️ [WhatsAppSendTool] WhatsApp bağlı değil, bağlanmaya çalışılıyor...")
		return "", fmt.Errorf("WhatsApp bağlantısı aktif değil. Önce QR kodu okutun.")
	}

	messageRaw, ok := args["message"]
	if !ok || messageRaw == nil {
		return "", fmt.Errorf("'message' parametresi eksik veya boş")
	}
	message, ok := messageRaw.(string)
	if !ok {
		return "", fmt.Errorf("'message' parametresi string formatında olmalı")
	}
	message = strings.TrimSpace(message)
	
	if message == "" {
		return "", fmt.Errorf("mesaj içeriği boş olamaz")
	}

	recipientRaw, _ := args["recipient"].(string)
	recipient := strings.TrimPrefix(strings.TrimSpace(recipientRaw), "+")
	adminPhone := strings.TrimPrefix(t.listener.AdminPhone, "+")

	if recipient != "" && recipient != adminPhone {
		logger.Error("🚨 [SECURITY] Pars yabancı bir numaraya (%s) mesaj atmaya çalıştı! ENGELLENDİ.", recipient)
		return "", fmt.Errorf("GÜVENLİK İHLALİ: Sadece yöneticiye (%s) mesaj gönderebilirsin. '%s' numarasına mesaj gönderme yetkin yok!", adminPhone, recipient)
	}

	targetJID := t.listener.GetAdminJID()

	if len(message) > MaxMessageLength {
		logger.Warn("⚠️ [WhatsAppSendTool] Mesaj çok uzun (%d > %d), kırpılıyor", len(message), MaxMessageLength)
		message = message[:MaxMessageLength]
	}

	t.listener.SendReply(targetJID, message)
	
	logger.Success("✅ [WhatsApp] Mesaj yöneticiye başarıyla iletildi: %s", adminPhone)
	return fmt.Sprintf("✅ Mesaj yöneticiye (%s) başarıyla iletildi.", adminPhone), nil
}


type WhatsAppSendImageTool struct {
	listener *Listener
}

func NewWhatsAppSendImageTool(l *Listener) *WhatsAppSendImageTool {
	if l == nil {
		logger.Error("❌ [WhatsAppSendImageTool] Listener nil! Araç oluşturulamadı.")
		return nil
	}
	return &WhatsAppSendImageTool{listener: l}
}

func (t *WhatsAppSendImageTool) Name() string { return "whatsapp_send_image" }

func (t *WhatsAppSendImageTool) Description() string {
	return "WhatsApp üzerinden SADECE yöneticiye resim gönderir. Bilgisayardaki bir resim dosyasının tam yolunu (image_path) alır ve gönderir."
}

func (t *WhatsAppSendImageTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"image_path": map[string]interface{}{"type": "string", "description": "Gönderilecek resmin diskteki tam yolu (Örn: .pars_trash/screen.png)"},
			"caption":    map[string]interface{}{"type": "string", "description": "Resmin altına eklenecek açıklama (Opsiyonel)."},
			"recipient":  map[string]interface{}{"type": "string", "description": "Güvenlik gereği SADECE admin numarası kabul edilir."},
		},
		"required": []string{"image_path"},
	}
}

func (t *WhatsAppSendImageTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if t == nil || t.listener == nil {
		return "", fmt.Errorf("tool veya listener nil")
	}

	if !t.listener.IsConnected() {
		logger.Warn("⚠️ [WhatsAppSendImageTool] WhatsApp bağlı değil")
		return "", fmt.Errorf("WhatsApp bağlantısı aktif değil. Önce QR kodu okutun.")
	}

	recipientRaw, _ := args["recipient"].(string)
	recipient := strings.TrimPrefix(strings.TrimSpace(recipientRaw), "+")
	adminPhone := strings.TrimPrefix(t.listener.AdminPhone, "+")

	if recipient != "" && recipient != adminPhone {
		logger.Error("🚨 [SECURITY] Pars yabancı bir numaraya resim atmaya çalıştı! ENGELLENDİ.")
		return "", fmt.Errorf("GÜVENLİK İHLALİ: Resim gönderimi sadece yöneticiye yapılabilir.")
	}

	imagePathRaw, ok := args["image_path"]
	if !ok || imagePathRaw == nil {
		return "", fmt.Errorf("'image_path' parametresi eksik")
	}
	imagePath, ok := imagePathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'image_path' parametresi string formatında olmalı")
	}
	imagePath = strings.TrimSpace(imagePath)

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

	targetJID := t.listener.GetAdminJID()

	logger.Info("📸 [WhatsApp] Resim yöneticiye gönderiliyor: %s", adminPhone)
	err := t.listener.SendImage(targetJID, imagePath, caption)
	if err != nil {
		logger.Error("❌ [WhatsAppSendImageTool] Resim gönderimi başarısız: %v", err)
		return "", fmt.Errorf("resim gönderilirken hata oluştu: %v", err)
	}
	
	logger.Success("✅ [WhatsApp] Resim başarıyla yöneticiye iletildi.")
	return fmt.Sprintf("✅ Resim yöneticiye (%s) başarıyla iletildi.", adminPhone), nil
}

func (t *WhatsAppSendTool) GetStatus() map[string]interface{} {
	if t == nil || t.listener == nil {
		return map[string]interface{}{"error": "tool nil"}
	}
	return map[string]interface{}{
		"name":        t.Name(),
		"connected":   t.listener.IsConnected(),
		"admin_lock":  true,
		"admin_phone": t.listener.AdminPhone,
	}
}

func (t *WhatsAppSendImageTool) GetStatus() map[string]interface{} {
	if t == nil || t.listener == nil {
		return map[string]interface{}{"error": "tool nil"}
	}
	return map[string]interface{}{
		"name":        t.Name(),
		"connected":   t.listener.IsConnected(),
		"admin_lock":  true,
		"admin_phone": t.listener.AdminPhone,
	}
}
// internal/communication/whatsapp/utils.go
// 🚀 DÜZELTMELER: Nil checks, MIME logic fix, Error handling, Memory limits
// ⚠️ DİKKAT: client.go ve processor.go ile %100 uyumlu

package whatsapp

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // PNG okumak için
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"golang.org/x/image/draw"
)

// 🚨 YENİ: Limit sabitleri
const (
	MaxImageSize      = 20 * 1024 * 1024 // 20 MB (WhatsApp limiti)
	MaxThumbnailSize  = 100 * 1024       // 100 KB thumbnail limiti
	ThumbnailWidth    = 100
	ThumbnailHeight   = 100
	ImageQuality      = 85
	ThumbnailQuality  = 70
	SendTimeout       = 60 * time.Second
)

// SendReply: Kullanıcıya cevap mesajı gönderir.
func (w *Listener) SendReply(jid types.JID, text string) {
	// 🚨 DÜZELTME #1: Nil checks
	if w == nil || w.Client == nil {
		// 🚨 KRİTİK: Burada logger.Debug KULLANMA (deadlock riski)
		return
	}
	if !w.Client.IsConnected() {
		// 🚨 KRİTİK: Burada logger.Debug KULLANMA (deadlock riski)
		return
	}
	
	// 🚨 DÜZELTME #2: Context timeout ekle
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	_, err := w.Client.SendMessage(ctx, jid, &waProto.Message{
		Conversation: proto.String(text),
	})
	if err != nil {
		// 🚨 KRİTİK DEĞİŞİKLİK: Burada logger.Error kullanmıyoruz!
		// Aksi halde hata mesajı tekrar WhatsApp'a gitmeye çalışıp sonsuz döngü yaratır.
		// Sadece debug modda ve çok sınırlı logla
		fmt.Fprintf(os.Stderr, "⚠️ [WA-SEND] Mesaj gönderim hatası (sessiz): %v\n", err)
	}
}

// MarkAsRead: Mesajı okundu olarak işaretler.
func (w *Listener) MarkAsRead(evt *events.Message) {
	// 🚨 DÜZELTME #3: Nil checks
	if w == nil || w.Client == nil || evt == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := w.Client.MarkRead(ctx, []types.MessageID{evt.Info.ID}, time.Now(), evt.Info.Chat, evt.Info.Sender)
	if err != nil {
		logger.Debug("⚠️ [WhatsApp] Okundu işareti başarısız: %v", err)
	}
}

// SetPresence: "Yazıyor..." durumunu günceller.
func (w *Listener) SetPresence(jid types.JID, presence types.ChatPresence) {
	// 🚨 DÜZELTME #4: Nil checks
	if w == nil || w.Client == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := w.Client.SendChatPresence(ctx, jid, presence, types.ChatPresenceMediaText)
	if err != nil {
		logger.Debug("⚠️ [WhatsApp] Presence güncellenemedi: %v", err)
	}
}

// =====================================================================
// 📸 GELİŞTİRİLMİŞ WHATSAPP RESİM MOTORU (PARS V5 SPECIAL)
// =====================================================================

// SendImage: Resmi işler, optimize eder ve WhatsApp'a fırlatır.
func (w *Listener) SendImage(jid types.JID, imagePath string, caption string) error {
	// 🚨 DÜZELTME #5: Nil checks
	if w == nil || w.Client == nil {
		return fmt.Errorf("WhatsApp client nil")
	}

	if !w.Client.IsConnected() {
		return fmt.Errorf("WhatsApp bağlı değil")
	}

	// 🚨 DÜZELTME #6: Input validation
	if imagePath == "" {
		return fmt.Errorf("imagePath boş")
	}

	if jid.User == "" {
		return fmt.Errorf("geçersiz JID")
	}

	ctx, cancel := context.WithTimeout(context.Background(), SendTimeout)
	defer cancel()

	// 1. Dosyayı Oku + Boyut Kontrolü
	imgData, err := os.ReadFile(imagePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("dosya bulunamadı: %s", imagePath)
		}
		return fmt.Errorf("dosya okunamadı: %v", err)
	}

	// 🚨 DÜZELTME #7: Dosya boyutu limiti (Memory bloat önleme)
	if len(imgData) == 0 {
		return fmt.Errorf("dosya boş (0 byte)")
	}
	if len(imgData) > MaxImageSize {
		return fmt.Errorf("dosya çok büyük (%d byte > %d byte)", len(imgData), MaxImageSize)
	}

	mimeType := http.DetectContentType(imgData)

	// 2. Decode ve Boyut Analizi
	img, format, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return fmt.Errorf("resim çözümlenemedi (%s): %v", format, err)
	}

	width := uint32(img.Bounds().Dx())
	height := uint32(img.Bounds().Dy())

	// 🚨 DÜZELTME #8: Resim boyutu mantıklı mı kontrol et
	if width == 0 || height == 0 {
		return fmt.Errorf("geçersiz resim boyutu: %dx%d", width, height)
	}

	// 3. PNG/WebP -> JPEG Dönüşümü (Uyumluluk için)
	// 🚨 DÜZELTME #9: MIME type logic fix (önceki: || mimeType != "image/jpeg" her zaman true'ydu)
	if mimeType != "image/jpeg" {
		buf := new(bytes.Buffer)
		if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: ImageQuality}); err == nil {
			imgData = buf.Bytes()
			mimeType = "image/jpeg"
			logger.Debug("🔄 [WhatsApp] Resim JPEG'e dönüştürüldü: %s -> %d bytes", imagePath, len(imgData))
		} else {
			logger.Warn("⚠️ [WhatsApp] JPEG dönüşümü başarısız, orijinal format kullanılıyor: %v", err)
		}
	}

	// 4. 🔥 KRİTİK: Profesyonel Thumbnail Üretimi
	thumbBytes := generateSafeThumbnail(img)
	if thumbBytes == nil {
		logger.Warn("⚠️ [WhatsApp] Thumbnail oluşturulamadı, boş thumbnail gönderiliyor")
		thumbBytes = []byte{}
	} else if len(thumbBytes) > MaxThumbnailSize {
		logger.Warn("⚠️ [WhatsApp] Thumbnail çok büyük (%d byte), kırpılıyor", len(thumbBytes))
		thumbBytes = thumbBytes[:MaxThumbnailSize]
	}

	// 5. Sunucuya Yükleme (MediaImage modunda)
	logger.Info("⏳ [WA-UPLOAD] %s yükleniyor (%d bytes, %dx%d)...", imagePath, len(imgData), width, height)
	resp, err := w.Client.Upload(ctx, imgData, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("WhatsApp upload hatası: %v", err)
	}

	// 6. Mesaj Yapısını Oluştur (Tüm anahtarlar eksiksiz olmalı)
	msg := &waProto.Message{
		ImageMessage: &waProto.ImageMessage{
			Caption:       proto.String(caption),
			Mimetype:      proto.String(mimeType),
			URL:           proto.String(resp.URL),
			DirectPath:    proto.String(resp.DirectPath),
			MediaKey:      resp.MediaKey,
			FileEncSHA256: resp.FileEncSHA256,
			FileSHA256:    resp.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(imgData))),
			Width:         proto.Uint32(width),
			Height:        proto.Uint32(height),
			JPEGThumbnail: thumbBytes,
		},
	}

	// 7. Gönder
	_, err = w.Client.SendMessage(ctx, jid, msg)
	if err != nil {
		logger.Error("❌ [WA-SEND] Resim gönderimi başarısız: %v", err)
		return err
	}

	logger.Info("✅ [WA-SUCCESS] Resim hedefe ulaştı: %s (%dx%d, %d bytes)", jid.User, width, height, len(imgData))
	return nil
}

// generateSafeThumbnail: Resmi WhatsApp'ın kabul edeceği minik bir önizlemeye dönüştürür.
func generateSafeThumbnail(src image.Image) []byte {
	// 🚨 DÜZELTME #10: Nil image kontrolü
	if src == nil {
		return nil
	}

	// Hedef thumbnail boyutu
	thumbRect := image.Rect(0, 0, ThumbnailWidth, ThumbnailHeight)
	dst := image.NewRGBA(thumbRect)

	// Kaliteli ölçekleme (BiLinear interpolation)
	draw.BiLinear.Scale(dst, thumbRect, src, src.Bounds(), draw.Over, nil)

	buf := new(bytes.Buffer)
	// Thumbnail için düşük kalite yeterli (küçük boyut)
	if err := jpeg.Encode(buf, dst, &jpeg.Options{Quality: ThumbnailQuality}); err != nil {
		logger.Debug("⚠️ [Thumbnail] JPEG encode hatası: %v", err)
		return nil
	}

	return buf.Bytes()
}

// 🆕 YENİ: ValidateImage - Resim dosyasını önceden doğrula
func ValidateImage(imagePath string) error {
	if imagePath == "" {
		return fmt.Errorf("imagePath boş")
	}

	info, err := os.Stat(imagePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("dosya bulunamadı: %s", imagePath)
		}
		return fmt.Errorf("dosya erişim hatası: %v", err)
	}

	if info.IsDir() {
		return fmt.Errorf("klasör değil, dosya yolu gerekli: %s", imagePath)
	}

	if info.Size() == 0 {
		return fmt.Errorf("dosya boş: %s", imagePath)
	}

	if info.Size() > MaxImageSize {
		return fmt.Errorf("dosya çok büyük (%d byte > %d byte): %s", info.Size(), MaxImageSize, imagePath)
	}

	// MIME type kontrolü (temel)
	file, err := os.Open(imagePath)
	if err != nil {
		return err
	}
	defer file.Close()

	buf := make([]byte, 512)
	_, err = file.Read(buf)
	if err != nil {
		return err
	}

	mimeType := http.DetectContentType(buf)
	if !strings.HasPrefix(mimeType, "image/") {
		return fmt.Errorf("geçersiz dosya tipi: %s (beklenen: image/*)", mimeType)
	}

	return nil
}

// 🆕 YENİ: CompressImage - Resmi optimize et (boyut küçültme)
func CompressImage(src image.Image, quality int) ([]byte, string, error) {
	if src == nil {
		return nil, "", fmt.Errorf("src image nil")
	}

	if quality < 1 || quality > 100 {
		quality = ImageQuality
	}

	buf := new(bytes.Buffer)
	if err := jpeg.Encode(buf, src, &jpeg.Options{Quality: quality}); err != nil {
		return nil, "", fmt.Errorf("JPEG encode hatası: %v", err)
	}

	return buf.Bytes(), "image/jpeg", nil
}
package whatsapp

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" 
	"io"         
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

const (
	MaxImageSize      = 20 * 1024 * 1024 
	MaxThumbnailSize  = 100 * 1024       
	ThumbnailWidth    = 100
	ThumbnailHeight   = 100
	ImageQuality      = 85
	ThumbnailQuality  = 70
	SendTimeout       = 60 * time.Second
)

func (w *Listener) SendReply(jid types.JID, text string) {

	if w == nil || w.Client == nil {
		return
	}
	if !w.Client.IsConnected() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	_, err := w.Client.SendMessage(ctx, jid, &waProto.Message{
		Conversation: proto.String(text),
	})
	if err != nil {

		fmt.Fprintf(os.Stderr, "⚠️ [WA-SEND] Mesaj gönderim hatası (sessiz): %v\n", err)
	}
}


func (w *Listener) MarkAsRead(evt *events.Message) {

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

func (w *Listener) SetPresence(jid types.JID, presence types.ChatPresence) {
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

func (w *Listener) SendImage(jid types.JID, imagePath string, caption string) error {

	if w == nil || w.Client == nil {
		return fmt.Errorf("WhatsApp client nil")
	}

	if !w.Client.IsConnected() {
		return fmt.Errorf("WhatsApp bağlı değil")
	}

	if imagePath == "" {
		return fmt.Errorf("imagePath boş")
	}

	if jid.User == "" {
		return fmt.Errorf("geçersiz JID")
	}

	ctx, cancel := context.WithTimeout(context.Background(), SendTimeout)
	defer cancel()

	imgData, err := os.ReadFile(imagePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("dosya bulunamadı: %s", imagePath)
		}
		return fmt.Errorf("dosya okunamadı: %v", err)
	}

	if len(imgData) == 0 {
		return fmt.Errorf("dosya boş (0 byte)")
	}
	if len(imgData) > MaxImageSize {
		return fmt.Errorf("dosya çok büyük (%d byte > %d byte)", len(imgData), MaxImageSize)
	}

	mimeType := http.DetectContentType(imgData)

	img, format, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return fmt.Errorf("resim çözümlenemedi (%s): %v", format, err)
	}

	width := uint32(img.Bounds().Dx())
	height := uint32(img.Bounds().Dy())

	if width == 0 || height == 0 {
		return fmt.Errorf("geçersiz resim boyutu: %dx%d", width, height)
	}


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

	thumbBytes := generateSafeThumbnail(img)
	if len(thumbBytes) > MaxThumbnailSize {
		logger.Warn("⚠️ [WhatsApp] Thumbnail boyutu limiti aştı (%d byte). Bozuk veri göndermemek için iptal ediliyor.", len(thumbBytes))
		thumbBytes = nil 
	} else if len(thumbBytes) == 0 {
		logger.Warn("⚠️ [WhatsApp] Thumbnail oluşturulamadı, boş thumbnail gönderiliyor.")
		thumbBytes = []byte{}
	}

	logger.Info("⏳ [WA-UPLOAD] %s yükleniyor (%d bytes, %dx%d)...", imagePath, len(imgData), width, height)
	resp, err := w.Client.Upload(ctx, imgData, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("WhatsApp upload hatası: %v", err)
	}

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

	_, err = w.Client.SendMessage(ctx, jid, msg)
	if err != nil {
		logger.Error("❌ [WA-SEND] Resim gönderimi başarısız: %v", err)
		return err
	}

	logger.Info("✅ [WA-SUCCESS] Resim hedefe ulaştı: %s (%dx%d, %d bytes)", jid.User, width, height, len(imgData))
	return nil
}

func generateSafeThumbnail(src image.Image) []byte {
	if src == nil {
		return nil
	}

	thumbRect := image.Rect(0, 0, ThumbnailWidth, ThumbnailHeight)
	dst := image.NewRGBA(thumbRect)

	draw.BiLinear.Scale(dst, thumbRect, src, src.Bounds(), draw.Over, nil)

	buf := new(bytes.Buffer)
	if err := jpeg.Encode(buf, dst, &jpeg.Options{Quality: ThumbnailQuality}); err != nil {
		logger.Debug("⚠️ [Thumbnail] JPEG encode hatası: %v", err)
		return nil
	}

	return buf.Bytes()
}

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

	file, err := os.Open(imagePath)
	if err != nil {
		return err
	}
	defer file.Close()

	buf := make([]byte, 512)
	n, err := file.Read(buf)
	
	if err != nil && err != io.EOF {
		return err
	}

	mimeType := http.DetectContentType(buf[:n])
	if !strings.HasPrefix(mimeType, "image/") {
		return fmt.Errorf("geçersiz dosya tipi: %s (beklenen: image/*)", mimeType)
	}

	return nil
}

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
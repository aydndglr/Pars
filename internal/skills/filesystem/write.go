// internal/skills/filesystem/write.go
// 🚀 DÜZELTME V7: Security Audit Log Standardizasyonu + Backup Race Condition Fix
// ⚠️ DİKKAT: SIEM uyumlu audit log formatı (delete.go ile %100 tutarlı)
// ⚠️ DİKKAT: auditLog fonksiyonu utils.go'da tanımlıdır, burada SADECE çağrılır

package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

// 🚨 YENİ: Sabitler ve Limitler
const (
	MaxWriteContentLength = 10 * 1024 * 1024 // 10 MB (büyük dosya write koruması)
	WriteTimeout          = 30 * time.Second // Dosya yazma timeout
	MaxBackupSize         = 50 * 1024 * 1024 // 50 MB (backup için max boyut)
)

// WriteTool: Dosya yazma aracı (Security Level aware)
type WriteTool struct {
	SecurityLevel string // god_mode, standard, restricted
}

// NewWriter: Yeni write tool oluştur
func NewWriter(securityLevel string) *WriteTool {
	if securityLevel == "" {
		securityLevel = "standard"
	}
	return &WriteTool{SecurityLevel: securityLevel}
}

func (t *WriteTool) Name() string { return "fs_write" }

func (t *WriteTool) Description() string {
	return "Dosyaya veri yazar. 'mode' parametresi ile üzerine yazabilir (overwrite), sonuna ekleyebilir (append) veya belirli bir satıra ekleme yapabilirsin (insert)."
}

func (t *WriteTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path":    map[string]interface{}{"type": "string", "description": "İşlem yapılacak dosya yolu."},
			"content": map[string]interface{}{"type": "string", "description": "Yazılacak içerik."},
			"mode":    map[string]interface{}{"type": "string", "description": "Kayıt modu: 'overwrite' (üzerine yaz - varsayılan), 'append' (sonuna ekle), 'insert' (belirli satıra ekle).", "enum": []string{"overwrite", "append", "insert"}},
			"line":    map[string]interface{}{"type": "integer", "description": "Sadece 'insert' modunda geçerlidir. İçeriğin ekleneceği satır numarası."},
		},
		"required": []string{"path", "content"},
	}
}

// 🆕 YENİ: validateWriteInput - Tüm input'ları doğrula
func validateWriteInput(path, content, mode string, line int) error {
	if path == "" {
		return fmt.Errorf("path boş olamaz")
	}
	if strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("geçersiz karakter tespit edildi")
	}
	if content == "" {
		return fmt.Errorf("content boş olamaz")
	}
	if len(content) > MaxWriteContentLength {
		return fmt.Errorf("content çok büyük (%d byte > %d byte)", len(content), MaxWriteContentLength)
	}
	if mode != "overwrite" && mode != "append" && mode != "insert" {
		return fmt.Errorf("geçersiz mode: %s (overwrite/append/insert)", mode)
	}
	if mode == "insert" && line < 1 {
		return fmt.Errorf("insert modu için line >= 1 olmalı")
	}
	return nil
}

func (t *WriteTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	// 🚨 DÜZELTME #1: Nil check
	if t == nil {
		return "", fmt.Errorf("WriteTool nil")
	}

	// 🚨 DÜZELTME #2: Type assertion with ok checks
	pathRaw, ok := args["path"]
	if !ok || pathRaw == nil {
		return "", fmt.Errorf("HATA: 'path' parametresi eksik. Nereye yazacağımı belirtmelisin")
	}
	pathStr, ok := pathRaw.(string)
	if !ok {
		return "", fmt.Errorf("HATA: 'path' parametresi metin (string) formatında olmalı")
	}

	contentRaw, ok := args["content"]
	if !ok || contentRaw == nil {
		return "", fmt.Errorf("HATA: 'content' parametresi eksik. Dosyaya ne yazacağımı belirtmelisin")
	}
	contentStr, ok := contentRaw.(string)
	if !ok {
		return "", fmt.Errorf("HATA: 'content' parametresi metin (string) formatında olmalı")
	}

	mode := "overwrite"
	if mRaw, ok := args["mode"]; ok && mRaw != nil {
		if m, ok := mRaw.(string); ok && m != "" {
			mode = m
		}
	}

	line := 1
	if lRaw, ok := args["line"]; ok && lRaw != nil {
		if l, ok := lRaw.(float64); ok {
			line = int(l)
		}
	}

	path := ResolvePath(pathStr)
	content := contentStr

	// 🚨 DÜZELTME #3: Input validation
	if err := validateWriteInput(path, content, mode, line); err != nil {
		return "", fmt.Errorf("validation hatası: %w", err)
	}

	logger.Action("📝 [WriteTool] Dosya yazma işlemi: %s (mode: %s, security: %s)", path, mode, t.SecurityLevel)

	// =====================================================================
	// 🛡️ AKILLI GÜVENLİK FİLTRESİ (SMART GUARDRAILS)
	// =====================================================================
	rootDir, err := os.Getwd()
	if err != nil {
		logger.Warn("⚠️ [WriteTool] Kök dizin alınamadı: %v", err)
		rootDir = "."
	}

	relPath, err := filepath.Rel(rootDir, path)
	if err == nil && !strings.HasPrefix(relPath, "..") {
		slashRel := filepath.ToSlash(relPath)

		// 🚨 ÖZ KORUMA (SELF-PRESERVATION): Kendini ezmesini/bozmasını engelle
		criticalCorePaths := []string{
			".git",
			"go.mod",
			"go.sum",
			"internal",
			"cmd",
			"config",
			"pars_tum_kodlar_v5.txt",
		}

		for _, protected := range criticalCorePaths {
			if slashRel == protected || strings.HasPrefix(slashRel, protected+"/") {
				logger.Error("❌ [WriteTool] Öz koruma protokolü aktif: %s", path)
				return "", fmt.Errorf("🛑 ÖZ KORUMA PROTOKOLÜ: '%s' yolu Pars'ın kendi hayati organıdır. Hangi modda olursam olayım kendi sistem dosyalarımı ezemem veya değiştiremem!", path)
			}
		}

		// 🛡️ STANDART MOD: Kendi proje klasörü içindeki diğer dosyalara yazmasını engelle.
		if t.SecurityLevel != "god_mode" {
			allowedStandardDirs := []string{"tools", "logs", "user_skills", "imported", ".pars_trash", "db"}
			isAllowed := false

			for _, allowed := range allowedStandardDirs {
				if strings.HasPrefix(slashRel, allowed+"/") || slashRel == allowed {
					isAllowed = true
					break
				}
			}

			if !isAllowed && slashRel != "." {
				logger.Warn("⚠️ [WriteTool] Güvenlik ihlali engellendi: %s", path)
				return "", fmt.Errorf("🛑 GÜVENLİK İHLALİ (Standard Mod): '%s' yoluna yazma izniniz yok. Sadece tools/ veya logs/ gibi güvenli alanlara dosya yazabilirsiniz.", path)
			}
		}
	} else {
		// HEDEF: Dış Dünya (Pars'ın klasörü dışındaki her yer)
		// ⚡ GOD MODE AÇIKSA DIŞ DÜNYADA SINIR YOK!
		if t.SecurityLevel != "god_mode" {
			logger.Warn("⚠️ [WriteTool] Dış dünya yazma engellendi: %s", path)
			return "", fmt.Errorf("🛑 GÜVENLİK İHLALİ (Standard Mod): Pars'ın çalışma alanı dışındaki dosyalara (%s) veri yazamazsınız. Bu işlem için 'god_mode' gereklidir.", path)
		} else {
			logger.Warn("⚡ [WriteTool] GOD MODE: Tehlikeli yazma işlemine izin verildi -> %s", path)
		}
	}

	// 🚨 DÜZELTME #4: Context timeout ekle
	writeCtx, cancel := context.WithTimeout(ctx, WriteTimeout)
	defer cancel()

	// Klasör hiyerarşisini garantiye al
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		logger.Error("❌ [WriteTool] Klasör oluşturulamadı: %v", err)
		return "", fmt.Errorf("klasör oluşturulamadı: %w", err)
	}

	// Dosya hiç yoksa insert veya append yapamayız, mecburen overwrite moduna dönüyoruz
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if mode != "overwrite" {
			logger.Debug("🔄 [WriteTool] Dosya yok, mode '%s' -> 'overwrite' olarak değiştirildi", mode)
		}
		mode = "overwrite"
	}

	// 🚨 DÜZELTME #5: Write operation'ı goroutine'de yap (timeout ile)
	var result string
	var writeErr error
	done := make(chan struct{})

	go func() {
		defer func() {
			if r := recover(); r != nil {
				writeErr = fmt.Errorf("panic: %v", r)
				logger.Error("💥 [WriteTool] Write panic: %v", r)
			}
			close(done)
		}()

		switch mode {
		case "append":
			result, writeErr = t.appendFile(path, content)
		case "insert":
			result, writeErr = t.insertFile(path, content, line)
		default: // "overwrite"
			result, writeErr = t.overwriteFile(path, content)
		}
	}()

	// 🚨 DÜZELTME #6: Timeout bekle
	select {
	case <-done:
		// İşlem tamamlandı
	case <-writeCtx.Done():
		logger.Error("❌ [WriteTool] Write timeout: %s (%d sn)", path, int(WriteTimeout.Seconds()))
		return "", fmt.Errorf("dosya yazma işlemi zaman aşımına uğradı (%d sn)", int(WriteTimeout.Seconds()))
	}

	if writeErr != nil {
		logger.Error("❌ [WriteTool] Write hatası: %v", writeErr)
		return "", writeErr
	}

	// 🚨 DÜZELTME #7: God mode audit log (utils.go'daki shared fonksiyon)
	// 🆕 DEĞİŞİKLİK: operation_type parametresi daha açıklayıcı
	if t.SecurityLevel == "god_mode" {
		auditLog(fmt.Sprintf("%s_WRITE", strings.ToUpper(mode)), path, "SUCCESS", int64(len(content)), fmt.Sprintf("mode:%s", mode))
	}

	logger.Success("✅ [WriteTool] Dosya yazma tamamlandı: %s (%d byte)", path, len(content))
	return result, nil
}

// 🆕 YENİ: appendFile - Dosya sonuna ekle
func (t *WriteTool) appendFile(path, content string) (string, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("dosya açılamadı: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString("\n" + content); err != nil {
		return "", fmt.Errorf("yazma hatası: %w", err)
	}

	logger.Info("💾 [WriteTool] Dosya sonuna eklendi: %s (%d byte)", path, len(content))
	return fmt.Sprintf("✅ İşlem Başarılı: %s dosyasına içerik eklendi (append).", path), nil
}

// 🆕 YENİ: insertFile - Belirli satıra ekle
func (t *WriteTool) insertFile(path, content string, line int) (string, error) {
	// 🚨 DÜZELTME #8: Büyük dosya kontrolü
	if info, err := os.Stat(path); err == nil && info.Size() > MaxBackupSize {
		logger.Warn("⚠️ [WriteTool] Büyük dosya (%.2f MB), insert işlemi yavaş olabilir", float64(info.Size())/(1024*1024))
	}

	fileBytes, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("dosya okunamadı: %w", err)
	}

	lines := strings.Split(string(fileBytes), "\n")

	// 🚨 DÜZELTME #9: Satır numarası guardrails
	if line < 1 {
		line = 1
	}
	if line > len(lines)+1 {
		line = len(lines) + 1
		logger.Debug("🔄 [WriteTool] Line numarası düzeltildi: %d -> %d", line, line)
	}

	// Yeni içeriği araya yerleştirme (Go Slice Magic)
	var newLines []string
	newLines = append(newLines, lines[:line-1]...) // Eklenecek yere kadar olan mevcut kısım
	newLines = append(newLines, content)           // Pars'ın enjekte ettiği kod/metin
	newLines = append(newLines, lines[line-1:]...) // Dosyanın geri kalanı

	finalContent := strings.Join(newLines, "\n")

	if err := os.WriteFile(path, []byte(finalContent), 0644); err != nil {
		return "", fmt.Errorf("yazma hatası: %w", err)
	}

	logger.Info("💾 [WriteTool] Dosyaya satır eklendi (Satır: %d): %s", line, path)
	return fmt.Sprintf("✅ İşlem Başarılı: %s dosyasının %d. satırına içerik yerleştirildi (insert).", path, line), nil
}

// 🆕 YENİ: overwriteFile - Dosyayı tamamen üzerine yaz
func (t *WriteTool) overwriteFile(path, content string) (string, error) {
	// 🚨 DÜZELTME #10: Backup oluştur (god_mode dışında) - Race condition fix
	if t.SecurityLevel != "god_mode" {
		if _, err := os.Stat(path); err == nil {
			// Dosya var, backup oluştur
			backupPath := path + ".bak"
			
			// 🆕 YENİ: Backup dosyası zaten varsa timestamp ekle
			if _, err := os.Stat(backupPath); err == nil {
				backupPath = fmt.Sprintf("%s.bak.%d", path, time.Now().Unix())
			}
			
			if err := os.Rename(path, backupPath); err != nil {
				// Rename başarısızsa copy dene
				srcData, readErr := os.ReadFile(path)
				if readErr == nil {
					writeErr := os.WriteFile(backupPath, srcData, 0644)
					if writeErr == nil {
						logger.Debug("💾 [WriteTool] Backup oluşturuldu (copy): %s", backupPath)
					} else {
						logger.Warn("⚠️ [WriteTool] Backup oluşturulamadı: %v", writeErr)
					}
				} else {
					logger.Warn("⚠️ [WriteTool] Backup oluşturulamadı: %v", err)
				}
			} else {
				logger.Debug("💾 [WriteTool] Backup oluşturuldu: %s", backupPath)
			}
		}
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("yazma hatası: %w", err)
	}

	logger.Info("💾 [WriteTool] Dosya üzerine yazıldı: %s (%d byte)", path, len(content))
	return fmt.Sprintf("✅ İşlem Başarılı: %s dosyası baştan yaratıldı (overwrite).", path), nil
}

// 🆕 YENİ: GetSecurityLevel - Güvenlik seviyesini döndür
func (t *WriteTool) GetSecurityLevel() string {
	if t == nil {
		return "unknown"
	}
	return t.SecurityLevel
}

// 🆕 YENİ: SetSecurityLevel - Güvenlik seviyesini değiştir
func (t *WriteTool) SetSecurityLevel(level string) {
	if t == nil {
		return
	}
	if level == "" {
		level = "standard"
	}
	t.SecurityLevel = level
	logger.Debug("🔧 [WriteTool] Güvenlik seviyesi değiştirildi: %s", level)
}
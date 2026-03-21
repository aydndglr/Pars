// internal/skills/filesystem/delete.go
// 🚀 DÜZELTME V2: auditLog Fonksiyonu utils.go'ya Taşındı (Temizlendi)
// ⚠️ DİKKAT: SIEM uyumlu audit log formatı (write.go ile %100 tutarlı)
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
	MaxPathLength = 4096 // Maksimum yol uzunluğu
)

// DeleteTool: Dosya ve klasör silme aracı (Security Level aware)
type DeleteTool struct {
	SecurityLevel string // god_mode, standard, restricted
}

// NewDeleter: Yeni silme aracı oluştur
func NewDeleter(securityLevel string) *DeleteTool {
	if securityLevel == "" {
		securityLevel = "standard" // Varsayılan
	}
	return &DeleteTool{SecurityLevel: securityLevel}
}

func (t *DeleteTool) Name() string { return "fs_delete" }

func (t *DeleteTool) Description() string {
	return "Dosya veya klasörü siler. 'permanent:false' (varsayılan) ile çöp kutusuna taşır, 'permanent:true' ile kalıcı olarak yok eder."
}

func (t *DeleteTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path":      map[string]interface{}{"type": "string", "description": "Silinecek dosya veya klasör yolu."},
			"permanent": map[string]interface{}{"type": "boolean", "description": "True ise geri dönüşümsüz siler. False ise '.pars_trash' klasörüne taşır (Varsayılan: false)."},
		},
		"required": []string{"path"},
	}
}

// 🆕 YENİ: validatePath - Yol geçerliliğini doğrula
func validatePath(path string) error {
	if path == "" {
		return fmt.Errorf("yol boş olamaz")
	}
	if len(path) > MaxPathLength {
		return fmt.Errorf("yol çok uzun (%d > %d karakter)", len(path), MaxPathLength)
	}
	if strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("geçersiz karakter tespit edildi")
	}
	return nil
}

// 🆕 YENİ: isCriticalSystemPath - Kritik sistem yollarını kontrol et (god_mode'da bile uyar)
func isCriticalSystemPath(path string) bool {
	// Windows kritik yolları
	winCritical := []string{
		`c:\windows`,
		`c:\program files`,
		`c:\program files (x86)`,
		`c:\boot`,
	}

	// Linux kritik yolları
	linCritical := []string{
		`/bin`,
		`/sbin`,
		`/usr/bin`,
		`/usr/sbin`,
		`/etc`,
		`/boot`,
		`/proc`,
		`/sys`,
	}

	pathLower := strings.ToLower(filepath.Clean(path))

	for _, critical := range winCritical {
		if strings.HasPrefix(pathLower, critical) {
			return true
		}
	}

	for _, critical := range linCritical {
		if strings.HasPrefix(pathLower, critical) {
			return true
		}
	}

	return false
}

func (t *DeleteTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	// 🚨 DÜZELTME #1: Nil checks
	if t == nil {
		return "", fmt.Errorf("DeleteTool nil")
	}

	if args == nil {
		return "", fmt.Errorf("args nil")
	}

	// 🚨 DÜZELTME #2: Type assertion with ok check
	pathRaw, ok := args["path"]
	if !ok || pathRaw == nil {
		return "", fmt.Errorf("'path' parametresi eksik")
	}

	pathStr, ok := pathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'path' parametresi string formatında olmalı")
	}

	permanent, _ := args["permanent"].(bool)

	// 🚨 DÜZELTME #3: Path validation
	if err := validatePath(pathStr); err != nil {
		return "", fmt.Errorf("path validation hatası: %w", err)
	}

	path := ResolvePath(pathStr)

	logger.Action("🗑️ [DeleteTool] Silme işlemi başlatılıyor: %s (permanent: %v, security: %s)", 
		path, permanent, t.SecurityLevel)

	// =====================================================================
	// 🛡️ GÜVENLİK SEVİYESİ KONTROLLERİ
	// =====================================================================

	// 🚨 DÜZELTME #4: Root/kök dizin koruması (HER MODDA AKTİF - Kullanıcıyı koru)
	rootDir, err := os.Getwd()
	if err != nil {
		logger.Warn("⚠️ [DeleteTool] Kök dizin alınamadı: %v", err)
		rootDir = "."
	}

	cleanPath := filepath.Clean(path)
	if cleanPath == "/" || cleanPath == "\\" || cleanPath == "C:\\" || cleanPath == "c:\\" || cleanPath == "." {
		logger.Error("❌ [DeleteTool] Kök dizin silme girişimi engellendi: %s", path)
		return "", fmt.Errorf("🛑 KRİTİK İHLAL: Kök dizinler (/ veya C:\\) veya proje ana dizini (.) tamamen silinemez!")
	}

	// 🚨 DÜZELTME #5: Sistem yolları için uyarı (god_mode'da bile logla)
	if isCriticalSystemPath(path) {
		if t.SecurityLevel == "god_mode" {
			// ⚡ GOD MODE: İzin ver ama AUDIT LOG'u at
			logger.Warn("⚠️ [GOD MODE AUDIT] Kullanıcı kritik sistem yolunu siliyor: %s", path)
			logger.Warn("⚠️ [GOD MODE AUDIT] Sorumluluk tamamen kullanıcıya aittir!")
		} else {
			// 🛑 STANDARD/RESTRICTED: Engelle
			logger.Error("❌ [DeleteTool] Kritik sistem yolu silme girişimi engellendi: %s", path)
			return "", fmt.Errorf("🛑 GÜVENLİK İHLALİ: Kritik sistem dosyaları (%s) silinemez. god_mode gereklidir.", path)
		}
	}

	// 🛡️ STANDART MOD KISITLAMALARI (god_mode'da ATLANIR)
	if t.SecurityLevel != "god_mode" {
		relPath, err := filepath.Rel(rootDir, path)
		if err == nil && !strings.HasPrefix(relPath, "..") {
			// HEDEF: Pars'ın Kendi Proje Klasörünün İçi
			slashRel := filepath.ToSlash(relPath)

			// 🚨 ÖZ KORUMA (SELF-PRESERVATION): Hangi modda olursa olsun, ajan kendini imha edemez!
			criticalCorePaths := []string{
				".git",
				"go.mod",
				"go.sum",
				"internal",
				"cmd",
				"config",
			}

			for _, protected := range criticalCorePaths {
				if slashRel == protected || strings.HasPrefix(slashRel, protected+"/") {
					logger.Error("❌ [DeleteTool] Öz koruma protokolü aktif: %s", path)
					return "", fmt.Errorf("🛑 ÖZ KORUMA PROTOKOLÜ: '%s' yolu Pars'ın kendi hayati organıdır. Hangi modda olursam olayım kendi beynimi/kalbimi silemem!", path)
				}
			}

			// 🛡️ STANDART MOD: Güvenli alanlar dışında silme yasak
			allowedStandardDirs := []string{"tools", ".pars_trash", "logs", "user_skills", "imported", "db"}
			isAllowed := false

			for _, allowed := range allowedStandardDirs {
				if strings.HasPrefix(slashRel, allowed+"/") || slashRel == allowed {
					isAllowed = true
					break
				}
			}

			if !isAllowed && slashRel != "." {
				logger.Error("❌ [DeleteTool] Güvenlik ihlali engellendi: %s", path)
				return "", fmt.Errorf("🛑 GÜVENLİK İHLALİ (Standard Mod): '%s' yoluna müdahale izniniz yok. Sadece tools/, .pars_trash/, logs/ gibi güvenli alanlarda işlem yapabilirsiniz. (god_mode gereklidir)", path)
			}
		} else {
			// HEDEF: Dış Dünya (Pars'ın klasörü dışı)
			logger.Error("❌ [DeleteTool] Dış dünya silme girişimi engellendi: %s", path)
			return "", fmt.Errorf("🛑 GÜVENLİK İHLALİ (Standard Mod): Pars'ın çalışma alanı dışındaki dosyalara (%s) müdahale edemezsiniz. Bu işlem için 'god_mode' gereklidir.", path)
		}
	} else {
		// ⚡ GOD MODE: TAM ÖZGÜRLÜK
		logger.Warn("⚡ [GOD MODE] Silme işlemi devam ediyor (kısıtlama yok): %s", path)
	}

	// =====================================================================
	// DOSYA VARLIK KONTROLÜ
	// =====================================================================
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		logger.Debug("⚠️ [DeleteTool] Dosya bulunamadı: %s", path)
		return "", fmt.Errorf("HATA: Silinmek istenen yol bulunamadı: %s", path)
	}
	if err != nil {
		logger.Error("❌ [DeleteTool] Dosya erişim hatası: %v", err)
		return "", fmt.Errorf("dosya erişim hatası: %v", err)
	}

	// Dosya boyutunu al (audit log için)
	fileSize := int64(0)
	if !info.IsDir() {
		fileSize = info.Size()
	}

	// =====================================================================
	// SİLME İŞLEMİ (SOFT vs HARD DELETE)
	// =====================================================================
	if !permanent {
		// ♻️ ÇÖP KUTUSU SİSTEMİ (SOFT DELETE)
		trashDir := ".pars_trash"
		if err := os.MkdirAll(trashDir, 0755); err != nil {
			logger.Error("❌ [DeleteTool] Çöp kutusu dizini oluşturulamadı: %v", err)
			return "", fmt.Errorf("çöp kutusu dizini oluşturulamadı: %v", err)
		}

		newName := fmt.Sprintf("%d_%s", time.Now().Unix(), filepath.Base(path))
		trashPath := filepath.Join(trashDir, newName)

		if err := os.Rename(path, trashPath); err != nil {
			logger.Error("❌ [DeleteTool] Çöp kutusuna taşıma başarısız: %v", err)
			return "", fmt.Errorf("çöp kutusuna taşıma başarısız: %v", err)
		}

		logger.Warn("♻️ [DeleteTool] Çöp Kutusu'na Taşındı: %s -> %s", path, trashPath)
		
		// 🚨 DÜZELTME #9: God mode audit log for soft delete (utils.go'daki shared fonksiyon)
		if t.SecurityLevel == "god_mode" {
			auditLog("SOFT_DELETE", trashPath, "SUCCESS", fileSize, "moved_to_trash")
		}
		
		return fmt.Sprintf("✅ '%s' başarıyla çöp kutusuna (.pars_trash) taşındı. Pişman olursan oradan alabilirsin.", path), nil
	}

	// 🔥 KALICI SİLME (HARD DELETE)
	// 🚨 DÜZELTME #6: Context timeout ile silme (uzun sürerse iptal et)
	deleteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// 🚨 DÜZELTME #7: Silme işlemini goroutine'de yap (context ile iptal edilebilir)
	done := make(chan error, 1)
	go func() {
		done <- os.RemoveAll(path)
	}()

	select {
	case err := <-done:
		if err != nil {
			logger.Error("❌ [DeleteTool] Kalıcı silme başarısız: %v", err)
			return "", fmt.Errorf("kalıcı silme başarısız: %v", err)
		}
	case <-deleteCtx.Done():
		logger.Error("❌ [DeleteTool] Silme işlemi zaman aşımına uğradı: %s", path)
		return "", fmt.Errorf("silme işlemi zaman aşımına uğradı (30 sn)")
	}

	typeStr := "Dosya"
	if info.IsDir() {
		typeStr = "Klasör ve içeriği"
	}

	logger.Warn("🗑️ [DeleteTool] KALICI OLARAK SİLİNDİ: %s (%s)", path, typeStr)

	// 🚨 DÜZELTME #8: God mode audit log - utils.go'daki shared fonksiyon
	if t.SecurityLevel == "god_mode" {
		auditLog("HARD_DELETE", path, "SUCCESS", fileSize, typeStr)
	}

	return fmt.Sprintf("🗑️ %s başarıyla ve KALICI olarak silindi: %s", typeStr, path), nil
}

// 🆕 YENİ: GetSecurityLevel - Güvenlik seviyesini döndür
func (t *DeleteTool) GetSecurityLevel() string {
	if t == nil {
		return "unknown"
	}
	return t.SecurityLevel
}

// 🆕 YENİ: SetSecurityLevel - Güvenlik seviyesini değiştir
func (t *DeleteTool) SetSecurityLevel(level string) {
	if t == nil {
		return
	}
	if level == "" {
		level = "standard"
	}
	t.SecurityLevel = level
	logger.Debug("🔧 [DeleteTool] Güvenlik seviyesi değiştirildi: %s", level)
}
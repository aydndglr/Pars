// internal/db_manager/manager.go
// 🚀 DÜZELTMELER: Absolute path fix, Logging, Error handling, Validation
// ⚠️ DİKKAT: Tüm micro-DB'ler (pars_memory.db, wa.db, vb.) artık doğru yolda oluşturulacak

package db_manager

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"context"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	_ "modernc.org/sqlite" // CGO gerektirmeyen saf Go SQLite sürücüsü
)

// 🚨 YENİ: Timeout sabitleri
const (
	DBPingTimeout     = 5 * time.Second
	ConnMaxLifetime   = 1 * time.Hour
)

var (
	// dbPool, açılan veritabanı bağlantılarını MUTLAK yoluna göre önbellekte tutar.
	// 🚨 DÜZELTME: Relative path sorunu çözüldü - tüm yollar absolute'e çevrilir
	dbPool = make(map[string]*sql.DB)
	
	// mu, eşzamanlı okuma/yazma (Concurrency) işlemlerinde bağlantı havuzunu koruyan kilittir.
	mu sync.RWMutex
)

// 🆕 YENİ: normalizeDBPath - Veritabanı yolunu mutlak (absolute) hale getirir
func normalizeDBPath(dbPath string) (string, error) {
	// Boş path kontrolü
	if dbPath == "" {
		return "", fmt.Errorf("veritabanı yolu boş olamaz")
	}

	// Zaten absolute path ise temizle ve dön
	if filepath.IsAbs(dbPath) {
		return filepath.Clean(dbPath), nil
	}

	// Relative path ise:
	// 1. Önce clean yap (../, ./, vs. temizle)
	cleanPath := filepath.Clean(dbPath)
	
	// 2. Absolute path'e çevir
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		// Fallback: Eğer Abs başarısız olursa, mevcut working directory ile birleştir
		wd, wdErr := os.Getwd()
		if wdErr != nil {
			return "", fmt.Errorf("veritabanı yolu çözümlenemedi: %v (wd error: %v)", err, wdErr)
		}
		absPath = filepath.Join(wd, cleanPath)
	}

	// 3. Parent dizin var mı kontrol et (yoksa oluştur)
	parentDir := filepath.Dir(absPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		logger.Warn("⚠️ [DBManager] Veritabanı dizini oluşturulamadı: %v", err)
		// Hata olsa bile devam et, belki dizin zaten vardır
	}

	logger.Debug("🔍 [DBManager] Relative path '%s' -> Absolute: '%s'", dbPath, absPath)
	return absPath, nil
}

// GetDB: Belirtilen dosya yolundaki SQLite veritabanı için güvenli, sıralı ve yapılandırılmış bir bağlantı döndürür.
// 🚨 DÜZELTME: dbPath otomatik olarak absolute path'e çevrilir, böylece farklı CWD'lerde aynı dosya kullanılır
func GetDB(dbPath string) (*sql.DB, error) {
	// 🚨 DÜZELTME #1: Path'i normalize et (relative -> absolute)
	absPath, err := normalizeDBPath(dbPath)
	if err != nil {
		logger.Error("❌ [DBManager] Path normalization hatası: %v", err)
		return nil, fmt.Errorf("path normalization hatası: %w", err)
	}

	// 1. Önce sadece "Okuma Kilidi" (RLock) alarak havuzu kontrol et.
	mu.RLock()
	if db, exists := dbPool[absPath]; exists {
		mu.RUnlock()
		logger.Debug("🔄 [DBManager] Havuzdan bağlantı alındı: %s", filepath.Base(absPath))
		return db, nil
	}
	mu.RUnlock()

	// 2. Bağlantı yoksa "Yazma Kilidi" (Lock) alarak yeni bağlantı oluştur.
	mu.Lock()
	defer mu.Unlock()

	// 3. Double-check (Çifte Kontrol)
	if db, exists := dbPool[absPath]; exists {
		mu.Unlock()
		return db, nil
	}

	logger.Action("🗄️ [DBManager] Yeni veritabanı bağlantısı oluşturuluyor: %s", filepath.Base(absPath))

	// =========================================================================
	// 🚀 İLERİ SEVİYE PRAGMA ENJEKSİYONU (KİLİTLENME SAVAR)
	// =========================================================================
	// modernc.org/sqlite için PRAGMA ayarlarını URL üzerinden vermek en güvenli yöntemdir.
	dbURL := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)", absPath)

	// 4. Yeni bir SQLite bağlantısı oluştur
	db, err := sql.Open("sqlite", dbURL)
	if err != nil {
		logger.Error("❌ [DBManager] Bağlantı açılamadı: %v", err)
		return nil, fmt.Errorf("veritabanı bağlantısı açılamadı (%s): %w", filepath.Base(absPath), err)
	}

	// 5. Bağlantının gerçekten geçerli ve erişilebilir olduğunu doğrula (Ping)
	pingCtx, cancel := context.WithTimeout(context.Background(), DBPingTimeout)
	defer cancel()
	
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		logger.Error("❌ [DBManager] Ping başarısız: %v", err)
		return nil, fmt.Errorf("veritabanına ulaşılamıyor (Ping başarısız) (%s): %w", filepath.Base(absPath), err)
	}

	// =========================================================================
	// 🚀 KUSURSUZ SIRALI EKLEME (SAFE-QUEUE) MİMARİSİ
	// =========================================================================
	db.SetMaxOpenConns(1)                  // Kesin Sıralı Ekleme (Aynı anda sadece 1 işlem)
	db.SetMaxIdleConns(1)                  // Boşta bekleyen bağlantı limiti
	db.SetConnMaxLifetime(ConnMaxLifetime) // 1 saatte bir bağlantıyı tazele (Memory Leak önlemi)

	// 6. Hazırlanan güvenli bağlantıyı havuza kaydet
	dbPool[absPath] = db
	
	logger.Success("🗄️ Mikro-DB Bağlantısı Kuruldu (Safe-Queue & WAL Aktif): %s", filepath.Base(absPath))
	logger.Debug("📍 [DBManager] Fiziksel yol: %s", absPath)

	return db, nil
}

// CloseAll: Sistem kapatılırken (Graceful Shutdown) havuzdaki tüm veritabanı bağlantılarını güvenli bir şekilde kapatır.
func CloseAll() {
	mu.Lock()
	defer mu.Unlock()

	logger.Info("🛑 [DBManager] Tüm veritabanı bağlantıları kapatılıyor...")
	
	closedCount := 0
	for absPath, db := range dbPool {
		dbName := filepath.Base(absPath)
		
		// 🚨 DÜZELTME #2: WAL checkpoint before close
		if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE);"); err != nil {
			logger.Warn("⚠️ [DBManager] WAL checkpoint başarısız (%s): %v", dbName, err)
		}

		if err := db.Close(); err != nil {
			logger.Error("❌ [DBManager] Veritabanı kapatılırken hata (%s): %v", dbName, err)
		} else {
			logger.Debug("🔒 [DBManager] Bağlantı kapatıldı: %s", dbName)
			closedCount++
		}
		
		// Havuzdan temizle
		delete(dbPool, absPath)
	}
	
	logger.Success("✅ [DBManager] %d veritabanı bağlantısı güvenli şekilde kapatıldı", closedCount)
}

// 🆕 YENİ: GetPoolStats - Havuz istatistiklerini döndür (debug için)
func GetPoolStats() map[string]string {
	mu.RLock()
	defer mu.RUnlock()
	
	stats := make(map[string]string, len(dbPool))
	for absPath, db := range dbPool {
		stats[filepath.Base(absPath)] = fmt.Sprintf("open=%d, in_use=%d", 
			db.Stats().OpenConnections, 
			db.Stats().InUse)
	}
	return stats
}

// 🆕 YENİ: RemoveFromPool - Belirli bir veritabanını havuzdan çıkar (test/reconnect için)
func RemoveFromPool(dbPath string) bool {
	absPath, err := normalizeDBPath(dbPath)
	if err != nil {
		logger.Warn("⚠️ [DBManager] RemoveFromPool path error: %v", err)
		return false
	}
	
	mu.Lock()
	defer mu.Unlock()
	
	if db, exists := dbPool[absPath]; exists {
		db.Close()
		delete(dbPool, absPath)
		logger.Debug("🗑️ [DBManager] Havuzdan çıkarıldı: %s", filepath.Base(absPath))
		return true
	}
	return false
}
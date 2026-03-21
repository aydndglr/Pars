package db_manager

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	_ "modernc.org/sqlite" 
)

const (
	DBPingTimeout   = 5 * time.Second
	ConnMaxLifetime = 1 * time.Hour
)

var (
	globalBaseDir string
	baseDirOnce   sync.Once
	
	dbPool = make(map[string]*sql.DB)
	mu     sync.RWMutex
)

func GetBaseDir() string {
	baseDirOnce.Do(func() {
		exePath, err := os.Executable()
		if err == nil {
			slashPath := filepath.ToSlash(exePath)
			if !strings.Contains(slashPath, "go-build") && !strings.Contains(slashPath, "Temp") {
				globalBaseDir = filepath.Dir(exePath)
				logger.Debug("📍 [DBManager] BaseDir (binary): %s", globalBaseDir)
				return
			}
		}
		if wd, err := os.Getwd(); err == nil {
			globalBaseDir = wd
		} else {
			globalBaseDir = "."
		}
		logger.Debug("📍 [DBManager] BaseDir (fallback): %s", globalBaseDir)
	})
	return globalBaseDir
}

func SetBaseDir(path string) {
	globalBaseDir = path
	logger.Debug("🔧 [DBManager] BaseDir manuel ayarlandı: %s", path)
}

func normalizeDBPath(dbPath string) (string, error) {
	if dbPath == "" {
		return "", fmt.Errorf("veritabanı yolu boş olamaz")
	}

	if filepath.IsAbs(dbPath) {
		return filepath.Clean(dbPath), nil
	}

	baseDir := GetBaseDir()
	dbName := filepath.Base(dbPath)
	absPath := filepath.Join(baseDir, "db", dbName)
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		logger.Warn("⚠️ [DBManager] Veritabanı dizini oluşturulamadı: %v", err)
	}

	logger.Debug("🔍 [DBManager] Path çözümlendi: '%s' -> '%s'", dbPath, absPath)
	return absPath, nil
}

func GetDB(dbPath string) (*sql.DB, error) {
	absPath, err := normalizeDBPath(dbPath)
	if err != nil {
		logger.Error("❌ [DBManager] Path normalization hatası: %v", err)
		return nil, fmt.Errorf("path normalization hatası: %w", err)
	}


	mu.RLock()
	if db, exists := dbPool[absPath]; exists {
		mu.RUnlock()
		logger.Debug("🔄 [DBManager] Havuzdan bağlantı alındı: %s", filepath.Base(absPath))
		return db, nil
	}
	mu.RUnlock()
	mu.Lock()
	defer mu.Unlock()
	if db, exists := dbPool[absPath]; exists {
		return db, nil
	}

	logger.Action("🗄️ [DBManager] Yeni bağlantı oluşturuluyor: %s", filepath.Base(absPath))
	dbURL := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)", absPath)

	db, err := sql.Open("sqlite", dbURL)
	if err != nil {
		logger.Error("❌ [DBManager] Bağlantı açılamadı: %v", err)
		return nil, fmt.Errorf("veritabanı bağlantısı açılamadı (%s): %w", filepath.Base(absPath), err)
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), DBPingTimeout)
	pingErr := db.PingContext(pingCtx)
	cancel() 
	
	if pingErr != nil {
		_ = db.Close() 
		logger.Error("❌ [DBManager] Ping başarısız: %v", pingErr)
		return nil, fmt.Errorf("veritabanına ulaşılamıyor (%s): %w", filepath.Base(absPath), pingErr)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(ConnMaxLifetime)

	dbPool[absPath] = db
	dbName := filepath.Base(absPath)

	logger.Success("🗄️ Mikro-DB aktif: %s (WAL + Safe-Queue)", dbName)
	logger.Debug("📍 [DBManager] Fiziksel yol: %s", absPath)

	return db, nil
}

func CloseAll() {
	mu.Lock()
	defer mu.Unlock()

	logger.Info("🛑 [DBManager] Tüm bağlantılar kapatılıyor...")
	
	closedCount := 0
	for absPath, db := range dbPool {
		dbName := filepath.Base(absPath)
		
		if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE);"); err != nil {
			logger.Warn("⚠️ [DBManager] WAL checkpoint başarısız (%s): %v", dbName, err)
		}

		if err := db.Close(); err != nil {
			logger.Error("❌ [DBManager] Kapatma hatası (%s): %v", dbName, err)
		} else {
			logger.Debug("🔒 [DBManager] Bağlantı kapatıldı: %s", dbName)
			closedCount++
		}
		delete(dbPool, absPath)
	}
	
	logger.Success("✅ [DBManager] %d bağlantı güvenli şekilde kapatıldı", closedCount)
}

func GetPoolStats() map[string]string {
	mu.RLock()
	defer mu.RUnlock()
	
	stats := make(map[string]string, len(dbPool))
	for absPath, db := range dbPool {
		s := db.Stats()
		stats[filepath.Base(absPath)] = fmt.Sprintf("open=%d, in_use=%d, idle=%d", 
			s.OpenConnections, s.InUse, s.Idle)
	}
	return stats
}

func RemoveFromPool(dbPath string) bool {
	absPath, err := normalizeDBPath(dbPath)
	if err != nil {
		logger.Warn("⚠️ [DBManager] RemoveFromPool path error: %v", err)
		return false
	}
	
	mu.Lock()
	defer mu.Unlock()
	
	if db, exists := dbPool[absPath]; exists {
		_ = db.Close()
		delete(dbPool, absPath)
		logger.Debug("🗑️ [DBManager] Havuzdan çıkarıldı: %s", filepath.Base(absPath))
		return true
	}
	return false
}

func GetDBPath(dbPath string) (string, error) {
	return normalizeDBPath(dbPath)
}

func GetActiveConnections() int {
	mu.RLock()
	defer mu.RUnlock()
	return len(dbPool)
}

func IsConnected(dbPath string) bool {
	absPath, err := normalizeDBPath(dbPath)
	if err != nil {
		return false
	}
	
	mu.RLock()
	defer mu.RUnlock()
	_, exists := dbPool[absPath]
	return exists
}
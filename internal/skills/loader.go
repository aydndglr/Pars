// internal/skills/loader.go
// 🚀 DÜZELTME V3: DB Path Standardizasyonu - runner.go ile Tutarlı Hale Getirildi
// ⚠️ DİKKAT: db_manager bağlantı havuzunu kullanır, defer db.Close() YOK

package skills

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/aydndglr/pars-agent-v3/internal/db_manager"
)

// 🚨 YENİ: Timeout sabitleri
const (
	LoaderQueryTimeout = 30 * time.Second
	LoaderLoadTimeout  = 5 * time.Minute
)

// Loader: SQLite Veritabanından araçları okuyup Manager'a (Canlı Hafızaya) yükler.
type Loader struct {
	Manager    *Manager
	ToolsDir   string
	PythonPath string // venv/bin/python
	UvPath     string // İzole uv motorunun mutlak yolu
	BaseDir    string // 🆕 YENİ: Proje ana dizini (runner.go ile tutarlılık için)
}

// NewLoader: Güvenli yollarla yeni bir yükleyici oluşturur. Mutlak uvPath parametresi eklendi.
func NewLoader(mgr *Manager, toolsDir, pythonPath, uvPath string) *Loader {
	// 🚨 DÜZELTME #1: Nil check
	if mgr == nil {
		logger.Error("❌ [Loader] Manager nil! Loader oluşturulamadı.")
		return nil
	}

	// 🚀 ZIRHLI YOLLAR: Loader başlatılırken yolları kesinlikle mutlak (absolute) hale getiriyoruz.
	absToolsDir, err := filepath.Abs(toolsDir)
	if err != nil {
		logger.Warn("⚠️ [Loader] ToolsDir mutlak yola çevrilemedi: %v", err)
		absToolsDir = toolsDir
	}

	absPythonPath, err := filepath.Abs(pythonPath)
	if err != nil {
		logger.Warn("⚠️ [Loader] PythonPath mutlak yola çevrilemedi: %v", err)
		absPythonPath = pythonPath
	}

	absUvPath, err := filepath.Abs(uvPath)
	if err != nil {
		logger.Warn("⚠️ [Loader] UvPath mutlak yola çevrilemedi: %v", err)
		absUvPath = uvPath
	}

	// 🆕 YENİ: BaseDir'i tools klasörünün parent'ı olarak hesapla (runner.go ile aynı mantık)
	baseDir := filepath.Dir(absToolsDir)

	return &Loader{
		Manager:    mgr,
		ToolsDir:   absToolsDir,
		PythonPath: absPythonPath,
		UvPath:     absUvPath,
		BaseDir:    baseDir, // 🆕 YENİ: BaseDir kaydediliyor
	}
}

// 🆕 YENİ: getDBPath - DB yolunu tutarlı şekilde hesapla (runner.go ile aynı mantık)
func (l *Loader) getDBPath() (string, error) {
	if l == nil {
		return "", fmt.Errorf("loader nil")
	}

	// 🚀 YENİ: BaseDir'den hesapla (runner.go:127 ile aynı mantık)
	dbDir := filepath.Join(l.BaseDir, "db")

	// 🚨 DÜZELTME: DB dizini var mı kontrol et
	if _, err := os.Stat(dbDir); os.IsNotExist(err) {
		logger.Debug("📂 [Loader] DB dizini bulunamadı, oluşturuluyor: %s", dbDir)
		if err := os.MkdirAll(dbDir, 0755); err != nil {
			logger.Error("❌ [Loader] DB dizini oluşturulamadı: %v", err)
			return "", fmt.Errorf("db dizini oluşturulamadı: %w", err)
		}
	}

	dbPath := filepath.Join(dbDir, "pars_tools.db")

	// 🚨 DÜZELTME: Absolute path garantisi
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		logger.Warn("⚠️ [Loader] DB path absolute'e çevrilemedi: %v", err)
		return dbPath, nil
	}

	logger.Debug("🔍 [Loader] DB Path çözümlendi: %s", absPath)
	return absPath, nil
}

// LoadAll: Veritabanını tarar, diskteki karşılıklarını doğrular ve araçları zihne yükler.
func (l *Loader) LoadAll() error {
	// 🚨 DÜZELTME #2: Nil checks
	if l == nil {
		return fmt.Errorf("loader nil")
	}

	if l.Manager == nil {
		return fmt.Errorf("manager nil")
	}

	// 🚨 DÜZELTME #3: Path validation
	if l.ToolsDir == "" {
		return fmt.Errorf("toolsDir boş")
	}

	// 🆕 YENİ: Tutarlı DB path hesaplaması
	dbPath, err := l.getDBPath()
	if err != nil {
		logger.Error("❌ [Loader] DB path hesaplanamadı: %v", err)
		return err
	}

	// 🚀 YENİ: Merkezi ve Kilit Savar db_manager üzerinden güvenli bağlantı alıyoruz!
	db, err := db_manager.GetDB(dbPath)
	if err != nil {
		logger.Error("❌ [Loader] Veritabanına bağlanılamadı, araçlar zihne yüklenemiyor: %v", err)
		return err
	}
	// DİKKAT: defer db.Close() YOK! Havuzdaki bağlantı kapatılmaz, db_manager halleder.

	// 🚨 DÜZELTME #5: Timeout'lu context oluştur
	ctx, cancel := context.WithTimeout(context.Background(), LoaderLoadTimeout)
	defer cancel()

	// 1. Veritabanından pars'in yazdığı ve Kullanıcının yüklediği araçları çek
	query := `SELECT name, description, parameters, script_path, is_async, instructions 
              FROM tools 
              WHERE source_type IN ('pars', 'user')`

	// 🚨 DÜZELTME #6: Query context ile çalıştır
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		// 🚨 DÜZELTME #7: Tablo yoksa hata değil, debug log
		if err == sql.ErrNoRows || err.Error() == "no such table: tools" {
			logger.Debug("📂 [Loader] Araç veritabanı boş veya 'tools' tablosu henüz oluşturulmadı.")
			return nil
		}
		logger.Error("❌ [Loader] Sorgu hatası: %v", err)
		return fmt.Errorf("sorgu hatası: %w", err)
	}
	defer rows.Close()

	newCount := 0
	updateCount := 0
	skipCount := 0

	logger.Info("🧠 [Loader] Veritabanından araçlar yükleniyor... (DB: %s)", filepath.Base(dbPath))

	// 2. Satır satır araçları oku ve zihne kazı
	for rows.Next() {
		// 🚨 DÜZELTME #8: Context cancellation kontrolü
		select {
		case <-ctx.Done():
			logger.Warn("⚠️ [Loader] Yükleme zaman aşımına uğradı")
			return ctx.Err()
		default:
		}

		var name, desc, paramsStr, scriptPath, instructions string
		var isAsync bool

		if err := rows.Scan(&name, &desc, &paramsStr, &scriptPath, &isAsync, &instructions); err != nil {
			logger.Warn("⚠️ [Loader] Veritabanından araç okunurken atlama yapıldı: %v", err)
			skipCount++
			continue
		}

		// 🚨 DÜZELTME #9: Name validation
		if name == "" {
			logger.Warn("⚠️ [Loader] İsimsiz araç tespit edildi, atlanıyor")
			skipCount++
			continue
		}

		// 🛡️ AKILLI FİZİKSEL KONTROL: DB'de var ama Python dosyası silinmiş mi?
		if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
			logger.Warn("⚠️ [Loader] Araç hayaleti tespit edildi: '%s' veritabanında var ama fiziksel dosyası kayıp! Yükleme atlandı.", name)
			skipCount++
			continue
		}

		// 🚨 DÜZELTME #10: Script path mutlak yol mu kontrol et
		if !filepath.IsAbs(scriptPath) {
			logger.Debug("🔧 [Loader] Göreceli yol mutlak yola çevriliyor: %s", scriptPath)
			scriptPath = filepath.Join(l.ToolsDir, scriptPath)
		}

		// 3. JSON String olarak tutulan parametreleri Go Map formatına geri çevir
		var params map[string]interface{}
		if paramsStr != "" && paramsStr != "{}" {
			if err := json.Unmarshal([]byte(paramsStr), &params); err != nil {
				logger.Warn("⚠️ [Loader] Araç parametreleri bozuk (%s), argümansız yüklenecek: %v", name, err)
				params = make(map[string]interface{})
			}
		} else {
			params = make(map[string]interface{})
		}

		// 4. Aracı oluştur (⚡ UvPath parametresi eklendi)
		tool := NewPythonTool(name, desc, scriptPath, l.PythonPath, l.UvPath, params, isAsync, instructions)

		// 🚨 DÜZELTME #11: Tool nil kontrolü
		if tool == nil {
			logger.Warn("⚠️ [Loader] Tool oluşturulamadı: %s", name)
			skipCount++
			continue
		}

		// 5. Manager'a kaydet ve durumu izle
		isUpdate := l.Manager.Register(tool)
		if isUpdate {
			updateCount++
			logger.Debug("🔄 [Loader] Tool güncellendi: %s", name)
		} else {
			newCount++
			logger.Debug("✅ [Loader] Tool kaydedildi: %s", name)
		}
	}

	// 🚨 DÜZELTME #12: Rows error kontrolü
	if err := rows.Err(); err != nil {
		logger.Error("❌ [Loader] Rows iteration hatası: %v", err)
		return fmt.Errorf("rows hatası: %w", err)
	}

	// 6. Gelişmiş Raporlama
	total := newCount + updateCount
	if total > 0 {
		logger.Success("✅ [Loader] Veritabanından %d araç beyni okundu ve aktif edildi. (%d Yeni, %d Güncellendi, %d Atlanmış)", 
			total, newCount, updateCount, skipCount)
	} else {
		logger.Debug("📂 [Loader] Yüklenecek dinamik Python aracı bulunamadı. (Atlanmış: %d)", skipCount)
	}

	return nil
}

// 🆕 YENİ: GetToolCount - Veritabanındaki toplam araç sayısını döndür
func (l *Loader) GetToolCount() (int, error) {
	if l == nil {
		return 0, fmt.Errorf("loader nil")
	}

	// 🆕 YENİ: Tutarlı DB path hesaplaması
	dbPath, err := l.getDBPath()
	if err != nil {
		return 0, err
	}

	db, err := db_manager.GetDB(dbPath)
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), LoaderQueryTimeout)
	defer cancel()

	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tools WHERE source_type IN ('pars', 'user')`).Scan(&count)
	if err != nil {
		if err == sql.ErrNoRows || err.Error() == "no such table: tools" {
			return 0, nil
		}
		return 0, err
	}

	return count, nil
}

// 🆕 YENİ: PurgeGhostTools - DB'de var ama dosyası olmayan araçları temizle
func (l *Loader) PurgeGhostTools() (int, error) {
	if l == nil {
		return 0, fmt.Errorf("loader nil")
	}

	// 🆕 YENİ: Tutarlı DB path hesaplaması
	dbPath, err := l.getDBPath()
	if err != nil {
		return 0, err
	}

	db, err := db_manager.GetDB(dbPath)
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), LoaderQueryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, `SELECT name, script_path FROM tools WHERE source_type IN ('pars', 'user')`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	deletedCount := 0
	var ghostTools []string

	for rows.Next() {
		var name, scriptPath string
		if err := rows.Scan(&name, &scriptPath); err != nil {
			continue
		}

		if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
			ghostTools = append(ghostTools, name)
			
			_, err := db.ExecContext(ctx, `DELETE FROM tools WHERE name = ?`, name)
			if err == nil {
				deletedCount++
				logger.Debug("🗑️ [Loader] Hayalet tool temizlendi: %s", name)
			}
		}
	}

	if deletedCount > 0 {
		logger.Success("✅ [Loader] %d hayalet tool veritabanından temizlendi: %v", deletedCount, ghostTools)
	}

	return deletedCount, nil
}
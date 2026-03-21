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

const (
	LoaderQueryTimeout = 30 * time.Second
	LoaderLoadTimeout  = 5 * time.Minute
)

type Loader struct {
	Manager    *Manager
	ToolsDir   string
	PythonPath string
	UvPath     string 
	BaseDir    string
}

func NewLoader(mgr *Manager, toolsDir, pythonPath, uvPath string) *Loader {

	if mgr == nil {
		logger.Error("❌ [Loader] Manager nil! Loader oluşturulamadı.")
		return nil
	}


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


	baseDir := filepath.Dir(absToolsDir)

	return &Loader{
		Manager:    mgr,
		ToolsDir:   absToolsDir,
		PythonPath: absPythonPath,
		UvPath:     absUvPath,
		BaseDir:    baseDir,
	}
}


func (l *Loader) getDBPath() (string, error) {
	if l == nil {
		return "", fmt.Errorf("loader nil")
	}


	dbDir := filepath.Join(l.BaseDir, "db")


	if _, err := os.Stat(dbDir); os.IsNotExist(err) {
		logger.Debug("📂 [Loader] DB dizini bulunamadı, oluşturuluyor: %s", dbDir)
		if err := os.MkdirAll(dbDir, 0755); err != nil {
			logger.Error("❌ [Loader] DB dizini oluşturulamadı: %v", err)
			return "", fmt.Errorf("db dizini oluşturulamadı: %w", err)
		}
	}

	dbPath := filepath.Join(dbDir, "pars_tools.db")


	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		logger.Warn("⚠️ [Loader] DB path absolute'e çevrilemedi: %v", err)
		return dbPath, nil
	}

	logger.Debug("🔍 [Loader] DB Path çözümlendi: %s", absPath)
	return absPath, nil
}


func (l *Loader) LoadAll() error {

	if l == nil {
		return fmt.Errorf("loader nil")
	}

	if l.Manager == nil {
		return fmt.Errorf("manager nil")
	}


	if l.ToolsDir == "" {
		return fmt.Errorf("toolsDir boş")
	}


	dbPath, err := l.getDBPath()
	if err != nil {
		logger.Error("❌ [Loader] DB path hesaplanamadı: %v", err)
		return err
	}


	db, err := db_manager.GetDB(dbPath)
	if err != nil {
		logger.Error("❌ [Loader] Veritabanına bağlanılamadı, araçlar zihne yüklenemiyor: %v", err)
		return err
	}


	ctx, cancel := context.WithTimeout(context.Background(), LoaderLoadTimeout)
	defer cancel()


	query := `SELECT name, description, parameters, script_path, is_async, instructions 
              FROM tools 
              WHERE source_type IN ('pars', 'user')`


	rows, err := db.QueryContext(ctx, query)
	if err != nil {

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

	for rows.Next() {

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

		if name == "" {
			logger.Warn("⚠️ [Loader] İsimsiz araç tespit edildi, atlanıyor")
			skipCount++
			continue
		}


		if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
			logger.Warn("⚠️ [Loader] Araç hayaleti tespit edildi: '%s' veritabanında var ama fiziksel dosyası kayıp! Yükleme atlandı.", name)
			skipCount++
			continue
		}

	
		if !filepath.IsAbs(scriptPath) {
			logger.Debug("🔧 [Loader] Göreceli yol mutlak yola çevriliyor: %s", scriptPath)
			scriptPath = filepath.Join(l.ToolsDir, scriptPath)
		}


		var params map[string]interface{}
		if paramsStr != "" && paramsStr != "{}" {
			if err := json.Unmarshal([]byte(paramsStr), &params); err != nil {
				logger.Warn("⚠️ [Loader] Araç parametreleri bozuk (%s), argümansız yüklenecek: %v", name, err)
				params = make(map[string]interface{})
			}
		} else {
			params = make(map[string]interface{})
		}


		tool := NewPythonTool(name, desc, scriptPath, l.PythonPath, l.UvPath, params, isAsync, instructions)


		if tool == nil {
			logger.Warn("⚠️ [Loader] Tool oluşturulamadı: %s", name)
			skipCount++
			continue
		}


		isUpdate := l.Manager.Register(tool)
		if isUpdate {
			updateCount++
			logger.Debug("🔄 [Loader] Tool güncellendi: %s", name)
		} else {
			newCount++
			logger.Debug("✅ [Loader] Tool kaydedildi: %s", name)
		}
	}


	if err := rows.Err(); err != nil {
		logger.Error("❌ [Loader] Rows iteration hatası: %v", err)
		return fmt.Errorf("rows hatası: %w", err)
	}


	total := newCount + updateCount
	if total > 0 {
		logger.Success("✅ [Loader] Veritabanından %d araç beyni okundu ve aktif edildi. (%d Yeni, %d Güncellendi, %d Atlanmış)", 
			total, newCount, updateCount, skipCount)
	} else {
		logger.Debug("📂 [Loader] Yüklenecek dinamik Python aracı bulunamadı. (Atlanmış: %d)", skipCount)
	}

	return nil
}


func (l *Loader) GetToolCount() (int, error) {
	if l == nil {
		return 0, fmt.Errorf("loader nil")
	}


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


func (l *Loader) PurgeGhostTools() (int, error) {
	if l == nil {
		return 0, fmt.Errorf("loader nil")
	}


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
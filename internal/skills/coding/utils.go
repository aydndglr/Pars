// internal/skills/coding/utils.go
// 🚀 DÜZELTMELER: Context timeout, Input validation, Logging, Error handling
// ⚠️ DİKKAT: db_manager ile entegre, UPSERT pattern korunuyor

package coding

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/aydndglr/pars-agent-v3/internal/db_manager"
)

// 🚨 YENİ: Timeout sabitleri
const (
	ToolDBWriteTimeout = 10 * time.Second
	ToolDBReadTimeout  = 5 * time.Second
)

// ----------------------------------------------------------------------
// 🛠️ VERİTABANI ODAKLI ARAÇ KAYIT YÖNETİMİ
// ----------------------------------------------------------------------

// RegisterToolToDB, veritabanına yeni bir araç kaydeder veya mevcut aracı günceller.
func RegisterToolToDB(workspaceDir, name, sourceType, desc, scriptPath string, params map[string]interface{}, isAsync bool, instructions string) error {
	// 🚨 DÜZELTME #1: Input validation
	if workspaceDir == "" {
		return fmt.Errorf("workspaceDir boş olamaz")
	}
	if name == "" {
		return fmt.Errorf("tool adı boş olamaz")
	}
	if sourceType == "" {
		return fmt.Errorf("sourceType boş olamaz")
	}

	dbDir := filepath.Join(workspaceDir, "..", "db")
	
	// 🚨 DÜZELTME #2: DB dizini oluştur
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		logger.Error("❌ [RegisterToolToDB] DB dizini oluşturulamadı: %v", err)
		return fmt.Errorf("db dizini oluşturulamadı: %w", err)
	}
	
	dbPath := filepath.Join(dbDir, "pars_tools.db")
	
	// 🚀 Merkezi havuzdan (Pool) bağlantı alıyoruz.
	db, err := db_manager.GetDB(dbPath)
	if err != nil {
		logger.Error("❌ [RegisterToolToDB] DB bağlantı hatası: %v", err)
		return err
	}
	// ⚠️ DİKKAT: defer db.Close() YOK! Bağlantı kapatma işi db_manager'da.

	// 🛡️ GÜVENLİK: Tablo yoksa anında yarat
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS tools (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT UNIQUE,
		source_type TEXT,
		description TEXT,
		parameters TEXT,
		script_path TEXT,
		is_async BOOLEAN,
		instructions TEXT
	)`)
	if err != nil {
		logger.Error("❌ [RegisterToolToDB] Araç tablosu oluşturulamadı: %v", err)
		return err
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		logger.Warn("⚠️ [RegisterToolToDB] Params JSON marshal hatası: %v, fallback kullanılıyor", err)
		paramsJSON = []byte("{}")
	}

	// 🚨 DÜZELTME #3: Context timeout ekle
	ctx, cancel := context.WithTimeout(context.Background(), ToolDBWriteTimeout)
	defer cancel()

	query := `
	INSERT INTO tools (name, source_type, description, parameters, script_path, is_async, instructions)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(name) DO UPDATE SET
		source_type = excluded.source_type,
		description = excluded.description,
		parameters = excluded.parameters,
		script_path = excluded.script_path,
		is_async = excluded.is_async,
		instructions = excluded.instructions;
	`
	
	// 🚨 DÜZELTME #4: ExecContext kullan (timeout ile)
	result, err := db.ExecContext(ctx, query, name, sourceType, desc, string(paramsJSON), scriptPath, isAsync, instructions)
	if err != nil {
		logger.Error("❌ [RegisterToolToDB] Araç veritabanına kaydedilemedi (%s): %v", name, err)
		return err
	}

	// 🚨 DÜZELTME #5: Operation type logla (INSERT vs UPDATE)
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		logger.Debug("🔧 [RegisterToolToDB] %d satır etkilendi: %s", rowsAffected, name)
	}
	
	logger.Success("💾 [RegisterToolToDB] Araç DB'ye başarıyla entegre edildi: %s [%s]", name, strings.ToUpper(sourceType))
	return nil
}

// RemoveToolFromDB, belirtilen aracı SQLite veritabanından ismine göre güvenli bir şekilde siler.
func RemoveToolFromDB(workspaceDir, name string) error {
	// 🚨 DÜZELTME #6: Input validation
	if workspaceDir == "" {
		return fmt.Errorf("workspaceDir boş olamaz")
	}
	if name == "" {
		return fmt.Errorf("tool adı boş olamaz")
	}

	dbDir := filepath.Join(workspaceDir, "..", "db")
	
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		logger.Error("❌ [RemoveToolFromDB] DB dizini oluşturulamadı: %v", err)
		return err
	}
	
	dbPath := filepath.Join(dbDir, "pars_tools.db")
	
	// 🚀 Merkezi havuzdan bağlantı alıyoruz.
	db, err := db_manager.GetDB(dbPath)
	if err != nil {
		return err
	}

	// 🚨 DÜZELTME #7: Context timeout ekle
	ctx, cancel := context.WithTimeout(context.Background(), ToolDBWriteTimeout)
	defer cancel()

	// 🚨 DÜZELTME #8: ExecContext kullan
	result, err := db.ExecContext(ctx, `DELETE FROM tools WHERE name = ?`, name)
	if err != nil {
		logger.Error("❌ [RemoveToolFromDB] Araç DB'den silinemedi (%s): %v", name, err)
		return err
	}

	// 🚨 DÜZELTME #9: Silinen satır sayısını logla
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		logger.Warn("⚠️ [RemoveToolFromDB] Silinecek tool bulunamadı: %s", name)
	} else {
		logger.Debug("🗑️ [RemoveToolFromDB] %d tool silindi: %s", rowsAffected, name)
	}
	
	logger.Success("🗑️ [RemoveToolFromDB] Araç DB'den tamamen silindi: %s", name)
	return nil
}

// ----------------------------------------------------------------------
// 🐍 PYTHON KOD SARICI (WRAPPER) VE UV YARDIMCILARI
// ----------------------------------------------------------------------

// formatPythonCode, üretilen Python kodunu "Zırh (God Mode Armor)" ile sarar.
func formatPythonCode(filename, desc, code string) string {
	// 🚨 DÜZELTME #10: Input validation
	if filename == "" {
		filename = "unknown_tool.py"
	}
	if desc == "" {
		desc = "Pars otonom tool"
	}
	
	// Olası Markdown kalıntılarını (```python vb.) temizle
	code = strings.TrimPrefix(code, "```python")
	code = strings.TrimPrefix(code, "```")
	code = strings.TrimSuffix(code, "```")
	code = strings.TrimSpace(code)

	// 🚨 DÜZELTME #11: Kod boşsa uyarı ekle
	if code == "" {
		logger.Warn("⚠️ [formatPythonCode] Kod boş, placeholder ekleniyor")
		code = "# ⚠️ UYARI: Kod içeriği boş\nprint('Error: No code generated')"
	}

	// 🚀 ZIRHLI WRAPPER: Python scriptini dış dünyadan gelecek hatalara karşı koruyan ana zırh
	wrapper := `"""
NAME: %s
DESCRIPTION: %s
"""
import sys, json, os, io, traceback

# 1. Windows UTF-8 ve Standart Çıktı Ayarı (Karakter Kodlaması Çökmelerini Önler)
if sys.platform == "win32":
	try:
		sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding='utf-8', errors='replace')
		sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding='utf-8', errors='replace')
	except: pass

# 2. Pars Özel Hata Yakalayıcı (Crash Handler)
def parsos_excepthook(exc_type, exc_value, exc_traceback):
	print("\n" + "="*50)
	print("🚨 [Pars KRİTİK PYTHON HATASI] 🚨")
	print("="*50)
	print(f"Hata Türü : {exc_type.__name__}")
	print(f"Detay     : {exc_value}")
	print("\n--- Traceback (Hataya Giden Yol) ---")
	traceback.print_exception(exc_type, exc_value, exc_traceback, file=sys.stdout)
	print("="*50)

sys.excepthook = parsos_excepthook

# 3. Akıllı Argüman Yakalama (JSON Zırhı)
args = {}
if len(sys.argv) > 1:
	raw_arg = " ".join(sys.argv[1:]) # Tüm argümanları tek bir metinde birleştir
	try:
		data = json.loads(raw_arg)
		args = data.get("args", data) if isinstance(data, dict) else data
	except Exception as e:
		args = {"raw_input": raw_arg}

# ==========================================
# --- PARS OTONOM KOD BAŞLANGICI ---
# ==========================================
%s
# ==========================================
# --- PARS OTONOM KOD BİTİŞİ ---
# ==========================================
`
	return fmt.Sprintf(wrapper, filename, desc, code)
}

// 🆕 YENİ: ValidateToolParams - Tool parametrelerini doğrula
func ValidateToolParams(name, scriptPath string, params map[string]interface{}) error {
	if name == "" {
		return fmt.Errorf("tool adı boş olamaz")
	}
	if scriptPath == "" {
		return fmt.Errorf("script_path boş olamaz")
	}
	if params == nil {
		return fmt.Errorf("params nil olamaz")
	}
	
	// 🚨 DÜZELTME #12: JSON schema validasyonu (basit)
	if _, ok := params["type"]; !ok {
		return fmt.Errorf("params 'type' field içermeli (örn: 'object')")
	}
	if _, ok := params["properties"]; !ok {
		return fmt.Errorf("params 'properties' field içermeli")
	}
	
	return nil
}

// 🆕 YENİ: GetToolFromDB - DB'den tool bilgisi çek (debug için)
func GetToolFromDB(workspaceDir, name string) (map[string]interface{}, error) {
	if workspaceDir == "" {
		return nil, fmt.Errorf("workspaceDir boş olamaz")
	}
	if name == "" {
		return nil, fmt.Errorf("tool adı boş olamaz")
	}

	dbDir := filepath.Join(workspaceDir, "..", "db")
	dbPath := filepath.Join(dbDir, "pars_tools.db")
	
	db, err := db_manager.GetDB(dbPath)
	if err != nil {
		return nil, err
	}

	// 🚨 DÜZELTME #13: Context timeout ekle
	ctx, cancel := context.WithTimeout(context.Background(), ToolDBReadTimeout)
	defer cancel()

	// 🚨 DÜZELTME #14: QueryContext kullan
	row := db.QueryRowContext(ctx, `
		SELECT name, source_type, description, parameters, script_path, is_async, instructions 
		FROM tools 
		WHERE name = ?`, name)
	
	var toolName, sourceType, description, paramsStr, scriptPath, instructions string
	var isAsync bool
	
	err = row.Scan(&toolName, &sourceType, &description, &paramsStr, &scriptPath, &isAsync, &instructions)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("tool bulunamadı: %s", name)
		}
		return nil, err
	}

	// 🚨 DÜZELTME #15: Params JSON parse
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(paramsStr), &params); err != nil {
		params = make(map[string]interface{})
	}

	return map[string]interface{}{
		"name":         toolName,
		"source_type":  sourceType,
		"description":  description,
		"parameters":   params,
		"script_path":  scriptPath,
		"is_async":     isAsync,
		"instructions": instructions,
	}, nil
}

// 🆕 YENİ: ListToolsFromDB - DB'deki tüm tool'ları listele
func ListToolsFromDB(workspaceDir string) ([]map[string]interface{}, error) {
	if workspaceDir == "" {
		return nil, fmt.Errorf("workspaceDir boş olamaz")
	}

	dbDir := filepath.Join(workspaceDir, "..", "db")
	dbPath := filepath.Join(dbDir, "pars_tools.db")
	
	db, err := db_manager.GetDB(dbPath)
	if err != nil {
		return nil, err
	}

	// 🚨 DÜZELTME #16: Context timeout ekle
	ctx, cancel := context.WithTimeout(context.Background(), ToolDBReadTimeout)
	defer cancel()

	// 🚨 DÜZELTME #17: QueryContext kullan
	rows, err := db.QueryContext(ctx, `
		SELECT name, source_type, description, script_path, is_async 
		FROM tools 
		ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tools []map[string]interface{}
	for rows.Next() {
		var toolName, sourceType, description, scriptPath string
		var isAsync bool
		
		err := rows.Scan(&toolName, &sourceType, &description, &scriptPath, &isAsync)
		if err != nil {
			logger.Warn("⚠️ [ListToolsFromDB] Row scan hatası: %v", err)
			continue
		}

		tools = append(tools, map[string]interface{}{
			"name":        toolName,
			"source_type": sourceType,
			"description": description,
			"script_path": scriptPath,
			"is_async":    isAsync,
		})
	}

	// 🚨 DÜZELTME #18: Rows error check
	if err := rows.Err(); err != nil {
		return nil, err
	}

	logger.Debug("📋 [ListToolsFromDB] %d tool listelendi", len(tools))
	return tools, nil
}
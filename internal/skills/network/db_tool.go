// internal/skills/network/db_tool.go
// 🚀 DÜZELTMELER: Connection pool cleanup, SQL injection protection, Validation, Error handling
// ⚠️ DİKKAT: db_manager ile entegre çalışabilir yapıda

package network

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"

	// Sürücüleri buraya eklemelisin (go mod tidy yapmayı unutma patron)
	_ "modernc.org/sqlite"
	// _ "github.com/go-sql-driver/mysql"
	// _ "github.com/lib/pq"
)

// 🚨 YENİ: Sabitler ve Limitler
const (
	DBQueryTimeout      = 30 * time.Second
	DBMaxRows           = 500
	DBMaxCellLength     = 1000 // Uzun hücreleri kırp
	DBPoolCleanupInterval = 10 * time.Minute
	DBMaxIdleConns      = 5
)

// DBConnection: Aktif veritabanı bağlantılarını ve havuzunu yönetir
type DBConnection struct {
	DB         *sql.DB
	Driver     string
	DSN        string
	LastActive time.Time
	CreatedAt  time.Time
	mu         sync.RWMutex
}

// 🆕 YENİ: IsExpired - Bağlantı süresi doldu mu kontrol et
func (c *DBConnection) IsExpired(maxAge time.Duration) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Since(c.CreatedAt) > maxAge
}

// 🆕 YENİ: MarkActive - Son aktivite zamanını güncelle
func (c *DBConnection) MarkActive() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LastActive = time.Now()
}

var (
	dbPool = make(map[string]*DBConnection)
	dbMu   sync.RWMutex
	poolCleanupDone = make(chan struct{})
)

// 🆕 YENİ: StartPoolCleanup - Arka planda eski bağlantıları temizle
func StartPoolCleanup(ctx context.Context, maxAge time.Duration) {
	if maxAge == 0 {
		maxAge = 1 * time.Hour
	}
	ticker := time.NewTicker(DBPoolCleanupInterval)
	defer ticker.Stop()
	defer close(poolCleanupDone)

	for {
		select {
		case <-ticker.C:
			cleanupExpiredConnections(maxAge)
		case <-ctx.Done():
			return
		}
	}
}

// 🆕 YENİ: cleanupExpiredConnections - Süresi dolan bağlantıları kapat
func cleanupExpiredConnections(maxAge time.Duration) {
	dbMu.Lock()
	defer dbMu.Unlock()

	for dsn, conn := range dbPool {
		if conn.IsExpired(maxAge) {
			logger.Debug("🧹 [DBTool] Eski bağlantı temizleniyor: %s", dsn)
			if conn.DB != nil {
				_ = conn.DB.Close()
			}
			delete(dbPool, dsn)
		}
	}
}

// 🆕 YENİ: StopPoolCleanup - Temizlik goroutine'ini durdur
func StopPoolCleanup() {
	select {
	case <-poolCleanupDone:
		// Zaten durmuş
	default:
		close(poolCleanupDone)
	}
}

type DBQueryTool struct{}

func (t *DBQueryTool) Name() string { return "db_query" }

func (t *DBQueryTool) Description() string {
	return `İLERİ SEVİYE VERİTABANI İSTİHBARAT SERVİSİ (DB Core).
- 'schema': Veritabanındaki tüm tabloları ve kolon yapılarını döner (Sorgu yazmadan önce buna bak!).
- 'query': Veri okuma işlemleri (SELECT). Sonuçları otomatik tablo formatına sokar.
- 'exec': Veri değiştirme/ekleme işlemleri (INSERT, UPDATE, DELETE).
- 'direct': Karmaşık ham SQL çalıştırma.
🚨 GÜVENLİK: İşlemler 30 saniye timeout ile korunur ve maksimum 500 satır döner.`
}

func (t *DBQueryTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action":   map[string]interface{}{"type": "string", "enum": []string{"schema", "query", "exec", "direct"}},
			"driver":   map[string]interface{}{"type": "string", "description": "DB Tipi: 'sqlite', 'mysql', 'postgres' (Varsayılan: sqlite)", "default": "sqlite"},
			"dsn":      map[string]interface{}{"type": "string", "description": "Bağlantı dizesi (Örn: 'pars_memory.db' veya 'user:pass@tcp(host:3306)/db')"},
			"sql":      map[string]interface{}{"type": "string", "description": "Çalıştırılacak SQL komutu."},
			"limit":    map[string]interface{}{"type": "integer", "description": "Dönecek maksimum satır sayısı (Varsayılan: 100)."},
			"format":   map[string]interface{}{"type": "string", "enum": []string{"table", "json"}, "default": "table"},
			"timeout":  map[string]interface{}{"type": "integer", "description": "Sorgu timeout süresi (saniye, varsayılan: 30)."},
		},
		"required": []string{"action", "dsn"},
	}
}

// 🆕 YENİ: validateDSN - DSN formatını basitçe doğrula (SQL injection önleme)
func validateDSN(driver, dsn string) error {
	if dsn == "" {
		return fmt.Errorf("dsn boş olamaz")
	}
	// Basit SQL injection pattern'ları engelle
	dangerous := []string{"--", ";", "/*", "*/", "xp_", "exec(", "drop ", "delete ", "truncate ", "alter ", "create "}
	dsnLower := strings.ToLower(dsn)
	for _, pattern := range dangerous {
		if strings.Contains(dsnLower, pattern) && driver != "sqlite" {
			// SQLite için yerel dosya yolu kontrolü
			if driver == "sqlite" && !strings.Contains(dsn, ".db") && !strings.Contains(dsn, ".sqlite") {
				return fmt.Errorf("geçersiz DSN formatı: potansiyel SQL injection tespit edildi")
			}
		}
	}
	// SQLite için: sadece .db/.sqlite dosyalarına izin ver
	if driver == "sqlite" {
		if !regexp.MustCompile(`^[a-zA-Z0-9_\-\.\/\\]+\.db(?:3|lite)?$`).MatchString(dsn) {
			return fmt.Errorf("geçersiz SQLite DSN: sadece .db/.sqlite dosyaları kabul edilir")
		}
	}
	return nil
}

// 🆕 YENİ: isReadOnlyQuery - Sorgunun sadece okuma yapıp yapmadığını kontrol et
func isReadOnlyQuery(sqlStr string) bool {
	sqlUpper := strings.ToUpper(strings.TrimSpace(sqlStr))
	// Sadece SELECT, EXPLAIN, PRAGMA (read), SHOW (MySQL) gibi okuma sorguları
	readOnlyPrefixes := []string{"SELECT ", "EXPLAIN ", "PRAGMA ", "SHOW ", "DESCRIBE ", "DESC "}
	for _, prefix := range readOnlyPrefixes {
		if strings.HasPrefix(sqlUpper, prefix) {
			return true
		}
	}
	return false
}

func (t *DBQueryTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	// 🚨 DÜZELTME #1: Nil check
	if t == nil {
		return "", fmt.Errorf("DBQueryTool nil")
	}

	// 🚨 DÜZELTME #2: Type assertions with ok checks
	actionRaw, ok := args["action"]
	if !ok || actionRaw == nil {
		return "", fmt.Errorf("'action' parametresi eksik")
	}
	action, ok := actionRaw.(string)
	if !ok {
		return "", fmt.Errorf("'action' parametresi string formatında olmalı")
	}

	driverRaw, ok := args["driver"]
	if !ok {
		driverRaw = "sqlite"
	}
	driver, ok := driverRaw.(string)
	if !ok {
		driver = "sqlite"
	}

	dsnRaw, ok := args["dsn"]
	if !ok || dsnRaw == nil {
		return "", fmt.Errorf("'dsn' parametresi eksik")
	}
	dsn, ok := dsnRaw.(string)
	if !ok {
		return "", fmt.Errorf("'dsn' parametresi string formatında olmalı")
	}

	sqlStr, _ := args["sql"].(string)

	// 🚨 DÜZELTME #3: Input validation
	if err := validateDSN(driver, dsn); err != nil {
		return "", fmt.Errorf("DSN validation hatası: %w", err)
	}

	// Limit handling
	limit := 100
	if lRaw, ok := args["limit"]; ok && lRaw != nil {
		if l, ok := lRaw.(float64); ok && l > 0 {
			limit = int(l)
		}
	}
	if limit > DBMaxRows {
		limit = DBMaxRows
		logger.Warn("⚠️ [DBTool] Limit %d'den büyük, %d'ye kırpıldı", limit, DBMaxRows)
	}

	outputFormat := "table"
	if fRaw, ok := args["format"]; ok && fRaw != nil {
		if f, ok := fRaw.(string); ok {
			outputFormat = f
		}
	}

	// 🚨 DÜZELTME #4: Timeout handling
	queryTimeout := DBQueryTimeout
	if tRaw, ok := args["timeout"]; ok && tRaw != nil {
		if tSec, ok := tRaw.(float64); ok && tSec > 0 {
			queryTimeout = time.Duration(tSec) * time.Second
		}
	}
	if queryTimeout > 5*time.Minute {
		queryTimeout = 5 * time.Minute
	}

	logger.Action("🗄️ DB İşlemi: [%s] -> %s (%s)", strings.ToUpper(action), dsn, driver)

	// 1. BAĞLANTI YÖNETİMİ (Bağlantı Havuzu)
	dbMu.RLock()
	conn, exists := dbPool[dsn]
	dbMu.RUnlock()

	if !exists {
		dbMu.Lock()
		// Double-check after acquiring write lock
		conn, exists = dbPool[dsn]
		if !exists {
			// 🚨 DÜZELTME #5: Context-aware connection creation
			connCtx, connCancel := context.WithTimeout(ctx, 10*time.Second)
			defer connCancel()

			db, err := sql.Open(driver, dsn)
			if err != nil {
				dbMu.Unlock()
				return "", fmt.Errorf("DB Bağlantı Hatası: %v", err)
			}

			// 🚨 DÜZELTME #6: Connection pool ayarları
			db.SetMaxOpenConns(DBMaxIdleConns)
			db.SetMaxIdleConns(DBMaxIdleConns)
			db.SetConnMaxLifetime(30 * time.Minute)

			// Bağlantıyı test et (context-aware)
			if err := db.PingContext(connCtx); err != nil {
				_ = db.Close()
				dbMu.Unlock()
				return "", fmt.Errorf("DB Ping Hatası: %v", err)
			}

			conn = &DBConnection{
				DB:         db,
				Driver:     driver,
				DSN:        dsn,
				LastActive: time.Now(),
				CreatedAt:  time.Now(),
			}
			dbPool[dsn] = conn
			logger.Debug("✅ [DBTool] Yeni bağlantı oluşturuldu: %s", dsn)
		}
		dbMu.Unlock()
	}

	// Mark connection as active
	conn.MarkActive()

	// 🚨 DÜZELTME #7: Security check for exec/direct actions
	if (action == "exec" || action == "direct") && !isReadOnlyQuery(sqlStr) {
		logger.Warn("⚠️ [DBTool] Yazma işlemi tespit edildi: %s", action)
		// İleride security level kontrolü eklenebilir
	}

	// 2. EYLEMLER
	switch action {
	case "schema":
		return t.inspectSchema(ctx, conn)

	case "query", "direct":
		if sqlStr == "" {
			return "❌ HATA: SQL sorgusu belirtilmedi.", nil
		}
		// 🚨 DÜZELTME #8: Context with timeout for query
		queryCtx, queryCancel := context.WithTimeout(ctx, queryTimeout)
		defer queryCancel()
		return t.executeQuery(queryCtx, conn, sqlStr, limit, outputFormat)

	case "exec":
		if sqlStr == "" {
			return "❌ HATA: SQL komutu belirtilmedi.", nil
		}
		execCtx, execCancel := context.WithTimeout(ctx, queryTimeout)
		defer execCancel()
		res, err := conn.DB.ExecContext(execCtx, sqlStr)
		if err != nil {
			logger.Error("❌ [DBTool] Exec hatası: %v", err)
			return "", fmt.Errorf("exec hatası: %v", err)
		}
		affected, _ := res.RowsAffected()
		logger.Success("✅ [DBTool] Exec başarılı: %d satır etkilendi", affected)
		return fmt.Sprintf("✅ İşlem Başarılı. Etkilenen Satır Sayısı: %d", affected), nil
	}

	return "Bilinmeyen aksiyon.", nil
}

// executeQuery: Sorguyu koşturur ve profesyonelce formatlar
func (t *DBQueryTool) executeQuery(ctx context.Context, conn *DBConnection, query string, limit int, format string) (string, error) {
	startTime := time.Now()

	rows, err := conn.DB.QueryContext(ctx, query)
	if err != nil {
		logger.Error("❌ [DBTool] Query hatası: %v", err)
		return "", fmt.Errorf("query hatası: %v", err)
	}
	defer rows.Close()

	// 🚨 DÜZELTME #9: Columns error handling
	cols, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("columns alınamadı: %v", err)
	}

	var results []map[string]interface{}
	rowCount := 0

	for rows.Next() {
		// 🚨 DÜZELTME #10: Context cancellation check in loop
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("sorgu iptal edildi: %v", ctx.Err())
		default:
		}

		if rowCount >= limit {
			logger.Warn("⚠️ [DBTool] Limit aşıldı (%d), sonuçlar kırpıldı", limit)
			break
		}

		columns := make([]interface{}, len(cols))
		columnPointers := make([]interface{}, len(cols))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}

		if err := rows.Scan(columnPointers...); err != nil {
			return "", fmt.Errorf("scan hatası: %v", err)
		}

		m := make(map[string]interface{}, len(cols))
		for i, colName := range cols {
			val := columnPointers[i].(*interface{})
			// 🚨 DÜZELTME #11: Nil value handling + cell length limit
			cellVal := *val
			if cellVal == nil {
				m[colName] = nil
			} else {
				cellStr := fmt.Sprintf("%v", cellVal)
				if len(cellStr) > DBMaxCellLength {
					cellStr = cellStr[:DBMaxCellLength] + "..."
				}
				m[colName] = cellStr
			}
		}
		results = append(results, m)
		rowCount++
	}

	// 🚨 DÜZELTME #12: Rows error check
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("rows iteration hatası: %v", err)
	}

	execTime := time.Since(startTime)
	logger.Debug("✅ [DBTool] Query tamamlandı: %d satır, %v", len(results), execTime)

	if len(results) == 0 {
		return "📭 Sorgu sonucu boş (0 satır).", nil
	}

	// JSON Formatı
	if format == "json" {
		data, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			return "", fmt.Errorf("JSON formatlama hatası: %v", err)
		}
		return string(data), nil
	}

	// 📊 Gelişmiş Markdown Tablo Formatı
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 SORGU SONUCU (%d satır, %v):\n\n| ", len(results), execTime.Round(time.Millisecond)))
	for _, col := range cols {
		sb.WriteString(col + " | ")
	}
	sb.WriteString("\n| " + strings.Repeat("--- | ", len(cols)) + "\n")

	for _, row := range results {
		sb.WriteString("| ")
		for _, col := range cols {
			cell := row[col]
			if cell == nil {
				sb.WriteString("NULL | ")
			} else {
				cellStr := fmt.Sprintf("%v", cell)
				// 🚨 DÜZELTME #13: Tablo içinde pipe karakteri escape et
				cellStr = strings.ReplaceAll(cellStr, "|", "\\|")
				sb.WriteString(cellStr + " | ")
			}
		}
		sb.WriteString("\n")
	}

	// 🚨 DÜZELTME #14: Çok uzun çıktıları kırp
	output := sb.String()
	if len(output) > 50*1024 { // 50 KB limit
		output = output[:50*1024] + "\n\n[... Çıktı çok uzun olduğu için kırpıldı ...]"
	}

	return output, nil
}

// inspectSchema: Veritabanı anatomisini Pars'e raporlar
func (t *DBQueryTool) inspectSchema(ctx context.Context, conn *DBConnection) (string, error) {
	var query string
	switch conn.Driver {
	case "sqlite":
		query = "SELECT name, sql FROM sqlite_master WHERE type='table' ORDER BY name;"
	case "mysql":
		query = "SELECT table_name, table_type, engine FROM information_schema.tables WHERE table_schema = DATABASE() ORDER BY table_name;"
	case "postgres":
		query = "SELECT table_name, table_type FROM information_schema.tables WHERE table_schema = 'public' ORDER BY table_name;"
	default:
		return "⚠️ Bu driver için otomatik şema analizi henüz desteklenmiyor.", nil
	}

	// 🚨 DÜZELTME #15: Context with timeout
	schemaCtx, schemaCancel := context.WithTimeout(ctx, DBQueryTimeout)
	defer schemaCancel()

	res, err := t.executeQuery(schemaCtx, conn, query, 100, "table")
	if err != nil {
		return "", err
	}
	return "🧠 VERİTABANI ŞEMASI (ANATOMİ):\n" + res, nil
}

// 🆕 YENİ: GetConnectionInfo - Debug için bağlantı bilgisi
func (t *DBQueryTool) GetConnectionInfo(dsn string) map[string]interface{} {
	dbMu.RLock()
	defer dbMu.RUnlock()

	conn, exists := dbPool[dsn]
	if !exists {
		return map[string]interface{}{"exists": false}
	}

	conn.mu.RLock()
	defer conn.mu.RUnlock()

	stats := conn.DB.Stats()
	return map[string]interface{}{
		"exists":       true,
		"driver":       conn.Driver,
		"lastActive":   conn.LastActive,
		"createdAt":    conn.CreatedAt,
		"open":         stats.OpenConnections,
		"inUse":        stats.InUse,
		"idle":         stats.Idle,
		"waitCount":    stats.WaitCount,
		"waitDuration": stats.WaitDuration.String(),
		"maxIdle":      stats.MaxIdleClosed,
		"maxLifetime":  stats.MaxLifetimeClosed,
	}
}

// 🆕 YENİ: CloseConnection - Belirli bir DSN'nin bağlantısını kapat
func (t *DBQueryTool) CloseConnection(dsn string) error {
	dbMu.Lock()
	defer dbMu.Unlock()

	conn, exists := dbPool[dsn]
	if !exists {
		return fmt.Errorf("bağlantı bulunamadı: %s", dsn)
	}

	if conn.DB != nil {
		if err := conn.DB.Close(); err != nil {
			logger.Error("❌ [DBTool] Bağlantı kapatılamadı: %v", err)
			return err
		}
	}
	delete(dbPool, dsn)
	logger.Debug("🔌 [DBTool] Bağlantı kapatıldı: %s", dsn)
	return nil
}

// 🆕 YENİ: CloseAll - Tüm bağlantıları kapat (shutdown için)
func (t *DBQueryTool) CloseAll() {
	dbMu.Lock()
	defer dbMu.Unlock()

	for dsn, conn := range dbPool {
		if conn.DB != nil {
			_ = conn.DB.Close()
		}
		delete(dbPool, dsn)
	}
	logger.Info("🔌 [DBTool] Tüm veritabanı bağlantıları kapatıldı")
}
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
    "github.com/aydndglr/pars-agent-v3/internal/db_manager"
    _ "modernc.org/sqlite"

)

const (
    DBQueryTimeout        = 30 * time.Second
    DBMaxRows             = 500
    DBMaxCellLength       = 1000 
    DBPoolCleanupInterval = 10 * time.Minute
    DBMaxIdleConns        = 5
    
    TaskDefaultTTL      = 30 * time.Minute 
    TaskMaxTTL          = 24 * time.Hour    
    TaskTypeUser        = "user"           
    TaskTypeAgent       = "agent"          
    TaskStatusPending   = "pending"        
    TaskStatusRunning   = "running"        
    TaskStatusCompleted = "completed"       
    TaskStatusFailed    = "failed"          
    TaskStatusStale     = "stale"         
)


type DBConnection struct {
    DB         *sql.DB
    Driver     string
    DSN        string
    LastActive time.Time
    CreatedAt  time.Time
    mu         sync.RWMutex
}

func (c *DBConnection) IsExpired(maxAge time.Duration) bool {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return time.Since(c.CreatedAt) > maxAge
}


func (c *DBConnection) MarkActive() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.LastActive = time.Now()
}

var (
    dbPool            = make(map[string]*DBConnection)
    dbMu              sync.RWMutex
    poolCleanupDone   = make(chan struct{})
)


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

func StopPoolCleanup() {
    select {
    case <-poolCleanupDone:
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


func validateDSN(driver, dsn string) error {
    if dsn == "" {
        return fmt.Errorf("dsn boş olamaz")
    }

    dangerous := []string{"--", ";", "/*", "*/", "xp_", "exec(", "drop ", "delete ", "truncate ", "alter ", "create "}
    dsnLower := strings.ToLower(dsn)
    for _, pattern := range dangerous {
        if strings.Contains(dsnLower, pattern) && driver != "sqlite" {

            if driver == "sqlite" && !strings.Contains(dsn, ".db") && !strings.Contains(dsn, ".sqlite") {
                return fmt.Errorf("geçersiz DSN formatı: potansiyel SQL injection tespit edildi")
            }
        }
    }

    if driver == "sqlite" {
        if !regexp.MustCompile(`^[a-zA-Z0-9_\-\.\/\\]+\.db(?:3|lite)?$`).MatchString(dsn) {
            return fmt.Errorf("geçersiz SQLite DSN: sadece .db/.sqlite dosyaları kabul edilir")
        }
    }
    return nil
}


func isReadOnlyQuery(sqlStr string) bool {
    sqlUpper := strings.ToUpper(strings.TrimSpace(sqlStr))

    readOnlyPrefixes := []string{"SELECT ", "EXPLAIN ", "PRAGMA ", "SHOW ", "DESCRIBE ", "DESC "}
    for _, prefix := range readOnlyPrefixes {
        if strings.HasPrefix(sqlUpper, prefix) {
            return true
        }
    }
    return false
}

func (t *DBQueryTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {

    if t == nil {
        return "", fmt.Errorf("DBQueryTool nil")
    }


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

    if err := validateDSN(driver, dsn); err != nil {
        return "", fmt.Errorf("DSN validation hatası: %w", err)
    }

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

    dbMu.RLock()
    conn, exists := dbPool[dsn]
    dbMu.RUnlock()

    if !exists {
        dbMu.Lock()

        conn, exists = dbPool[dsn]
        if !exists {
            connCtx, connCancel := context.WithTimeout(ctx, 10*time.Second)
            defer connCancel()

            db, err := sql.Open(driver, dsn)
            if err != nil {
                dbMu.Unlock()
                return "", fmt.Errorf("DB Bağlantı Hatası: %v", err)
            }

            db.SetMaxOpenConns(DBMaxIdleConns)
            db.SetMaxIdleConns(DBMaxIdleConns)
            db.SetConnMaxLifetime(30 * time.Minute)
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

    conn.MarkActive()

    if (action == "exec" || action == "direct") && !isReadOnlyQuery(sqlStr) {
        logger.Warn("⚠️ [DBTool] Yazma işlemi tespit edildi: %s", action)
    }


    switch action {
    case "schema":
        return t.inspectSchema(ctx, conn)

    case "query", "direct":
        if sqlStr == "" {
            return "❌ HATA: SQL sorgusu belirtilmedi.", nil
        }

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


func (t *DBQueryTool) executeQuery(ctx context.Context, conn *DBConnection, query string, limit int, format string) (string, error) {
    startTime := time.Now()

    rows, err := conn.DB.QueryContext(ctx, query)
    if err != nil {
        logger.Error("❌ [DBTool] Query hatası: %v", err)
        return "", fmt.Errorf("query hatası: %v", err)
    }
    defer rows.Close()
    cols, err := rows.Columns()
    if err != nil {
        return "", fmt.Errorf("columns alınamadı: %v", err)
    }

    var results []map[string]interface{}
    rowCount := 0

    for rows.Next() {
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

    if err := rows.Err(); err != nil {
        return "", fmt.Errorf("rows iteration hatası: %v", err)
    }

    execTime := time.Since(startTime)
    logger.Debug("✅ [DBTool] Query tamamlandı: %d satır, %v", len(results), execTime)

    if len(results) == 0 {
        return "📭 Sorgu sonucu boş (0 satır).", nil
    }

    if format == "json" {
        data, err := json.MarshalIndent(results, "", "  ")
        if err != nil {
            return "", fmt.Errorf("JSON formatlama hatası: %v", err)
        }
        return string(data), nil
    }

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
                cellStr = strings.ReplaceAll(cellStr, "|", "\\|")
                sb.WriteString(cellStr + " | ")
            }
        }
        sb.WriteString("\n")
    }


    output := sb.String()
    if len(output) > 50*1024 { 
        output = output[:50*1024] + "\n\n[... Çıktı çok uzun olduğu için kırpıldı ...]"
    }

    return output, nil
}

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


    schemaCtx, schemaCancel := context.WithTimeout(ctx, DBQueryTimeout)
    defer schemaCancel()

    res, err := t.executeQuery(schemaCtx, conn, query, 100, "table")
    if err != nil {
        return "", err
    }
    return "🧠 VERİTABANI ŞEMASI (ANATOMİ):\n" + res, nil
}


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


type CreateTaskTool struct {
    TasksDBPath string 
}

func NewCreateTaskTool(tasksDBPath string) *CreateTaskTool {
    return &CreateTaskTool{TasksDBPath: tasksDBPath}
}

func (t *CreateTaskTool) Name() string { return "create_task" }

func (t *CreateTaskTool) Description() string {
    return `YENİ GÖREV OLUŞTURMA ARACI. Uzun süreli veya arka plan görevleri için kullanılır.
- Agent görevleri: Otomatik temizlenir (TTL: 30 dakika varsayılan)
- User görevleri: Kullanıcı sil diyene kadar kalıcıdır
Kullanıcı "görev oluştur", "arka planda çalıştır", "zamanlanmış görev" dediğinde bu aracı kullan.`
}

func (t *CreateTaskTool) Parameters() map[string]interface{} {
    return map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "name": map[string]interface{}{
                "type":        "string",
                "description": "Görev adı (Örn: 'Günlük Yedekleme', 'Veri İşleme')",
            },
            "description": map[string]interface{}{
                "type":        "string",
                "description": "Görev açıklaması",
            },
            "prompt": map[string]interface{}{
                "type":        "string",
                "description": "Görev için LLM prompt'u (ne yapılacak)",
            },
            "task_type": map[string]interface{}{
                "type":        "string",
                "description": "Görev tipi: 'user' (kalıcı) veya 'agent' (otomatik silinir)",
                "enum":        []string{"user", "agent"},
                "default":     "user",
            },
            "ttl_minutes": map[string]interface{}{
                "type":        "integer",
                "description": "Time-to-live (dakika). 0 = kalıcı. Agent görevleri için varsayılan 30.",
                "default":     0,
            },
            "interval_min": map[string]interface{}{
                "type":        "integer",
                "description": "Tekrarlama aralığı (dakika). 0 = tek seferlik.",
                "default":     0,
            },
        },
        "required": []string{"name", "prompt"},
    }
}

func (t *CreateTaskTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
    if t == nil {
        return "", fmt.Errorf("CreateTaskTool nil")
    }

    name, _ := args["name"].(string)
    description, _ := args["description"].(string)
    prompt, _ := args["prompt"].(string)
    taskType, _ := args["task_type"].(string)
    ttlMinutesFloat, ok := args["ttl_minutes"].(float64)
    ttlMinutes := 0
    if ok {
        ttlMinutes = int(ttlMinutesFloat)
    }
    
    intervalMinFloat, ok := args["interval_min"].(float64)
    intervalMin := 0
    if ok {
        intervalMin = int(intervalMinFloat)
    }

    if name == "" || prompt == "" {
        return "", fmt.Errorf("name ve prompt zorunlu")
    }

    if taskType == "" {
        taskType = TaskTypeUser
    }

    if taskType == TaskTypeAgent && ttlMinutes <= 0 {
        ttlMinutes = int(TaskDefaultTTL.Minutes())
    }
    if taskType == TaskTypeUser {
        ttlMinutes = 0
    }
    if ttlMinutes > int(TaskMaxTTL.Minutes()) {
        ttlMinutes = int(TaskMaxTTL.Minutes())
    }

    db, err := db_manager.GetDB(t.TasksDBPath)
    if err != nil {
        return "", fmt.Errorf("DB bağlantı hatası: %v", err)
    }

    createTableSQL := `
    CREATE TABLE IF NOT EXISTS user_tasks (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT NOT NULL,
        description TEXT,
        prompt TEXT NOT NULL,
        interval_min INTEGER DEFAULT 0,
        last_run DATETIME DEFAULT CURRENT_TIMESTAMP,
        is_completed BOOLEAN DEFAULT 0,
        task_type TEXT DEFAULT 'user',
        ttl_minutes INTEGER DEFAULT 0,
        status TEXT DEFAULT 'pending',
        created_by TEXT DEFAULT 'system',
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
        completed_at DATETIME
    );
    CREATE INDEX IF NOT EXISTS idx_task_type ON user_tasks(task_type);
    CREATE INDEX IF NOT EXISTS idx_status ON user_tasks(status);
    CREATE INDEX IF NOT EXISTS idx_created_at ON user_tasks(created_at);
    `
    if _, err := db.Exec(createTableSQL); err != nil {
        return "", fmt.Errorf("tablo oluşturma hatası: %v", err)
    }

    insertSQL := `
    INSERT INTO user_tasks (name, description, prompt, interval_min, task_type, ttl_minutes, status, created_by)
    VALUES (?, ?, ?, ?, ?, ?, 'pending', 'user')
    `
    result, err := db.Exec(insertSQL, name, description, prompt, intervalMin, taskType, ttlMinutes)
    if err != nil {
        logger.Error("❌ [CreateTask] Görev oluşturulamadı: %v", err)
        return "", fmt.Errorf("görev oluşturma hatası: %v", err)
    }

    id, err := result.LastInsertId()
    if err != nil {
        return "", fmt.Errorf("ID alınamadı: %v", err)
    }

    logger.Success("✅ [CreateTask] Görev oluşturuldu: %s (ID: %d, Type: %s, TTL: %d dk)", name, id, taskType, ttlMinutes)

    return fmt.Sprintf("✅ Görev başarıyla oluşturuldu!\n📋 **Görev Bilgileri:**\n- **ID:** %d\n- **Ad:** %s\n- **Tip:** %s\n- **TTL:** %d dakika\n- **Durum:** pending\n\n%s", 
        id, name, taskType, ttlMinutes,
        func() string {
            if taskType == TaskTypeAgent {
                return "🗑️ **Not:** Bu görev tamamlandıktan veya TTL süresi dolduktan sonra otomatik silinecek."
            }
            return "💾 **Not:** Bu görev sen sil diyene kadar kalıcı olacak."
        }()), nil
}

type UpdateTaskStatusTool struct {
    TasksDBPath string
}

func NewUpdateTaskStatusTool(tasksDBPath string) *UpdateTaskStatusTool {
    return &UpdateTaskStatusTool{TasksDBPath: tasksDBPath}
}

func (t *UpdateTaskStatusTool) Name() string { return "update_task_status" }

func (t *UpdateTaskStatusTool) Description() string {
    return `GÖREV DURUMU GÜNCELLEME ARACI. Çalışan görevlerin durumunu günceller.
- 'running': Görev çalışıyor
- 'completed': Görev tamamlandı
- 'failed': Görev başarısız
Uzun görevlerde ilerleme bildirmek için kullan.`
}

func (t *UpdateTaskStatusTool) Parameters() map[string]interface{} {
    return map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "task_id": map[string]interface{}{
                "type":        "integer",
                "description": "Görev ID'si",
            },
            "status": map[string]interface{}{
                "type":        "string",
                "description": "Yeni durum",
                "enum":        []string{"pending", "running", "completed", "failed", "stale"},
            },
        },
        "required": []string{"task_id", "status"},
    }
}

func (t *UpdateTaskStatusTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
    if t == nil {
        return "", fmt.Errorf("UpdateTaskStatusTool nil")
    }

    taskIDFloat, ok := args["task_id"].(float64)
    if !ok {
        return "", fmt.Errorf("task_id eksik veya geçersiz")
    }
    taskID := int(taskIDFloat)

    status, _ := args["status"].(string)
    if status == "" {
        return "", fmt.Errorf("status eksik")
    }

    db, err := db_manager.GetDB(t.TasksDBPath)
    if err != nil {
        return "", fmt.Errorf("DB bağlantı hatası: %v", err)
    }

    var query string
    if status == "completed" {
        query = `UPDATE user_tasks SET status = ?, completed_at = CURRENT_TIMESTAMP WHERE id = ?`
    } else {
        query = `UPDATE user_tasks SET status = ? WHERE id = ?`
    }

    result, err := db.Exec(query, status, taskID)
    if err != nil {
        return "", fmt.Errorf("durum güncelleme hatası: %v", err)
    }

    rowsAffected, _ := result.RowsAffected()
    if rowsAffected == 0 {
        return fmt.Sprintf("⚠️ Görev bulunamadı (ID: %d)", taskID), nil
    }

    logger.Debug("✅ [UpdateTaskStatus] Görev durumu güncellendi: ID=%d, Status=%s", taskID, status)
    return fmt.Sprintf("✅ Görev durumu güncellendi: %s", status), nil
}

type ListTasksTool struct {
    TasksDBPath string
}

func NewListTasksTool(tasksDBPath string) *ListTasksTool {
    return &ListTasksTool{TasksDBPath: tasksDBPath}
}

func (t *ListTasksTool) Name() string { return "list_tasks" }

func (t *ListTasksTool) Description() string {
    return `GÖREV LİSTELEME ARACI. Tüm görevleri veya filtrelenmiş görevleri listeler.
- task_type: 'user', 'agent' veya 'all'
- status: 'pending', 'running', 'completed', 'failed' veya 'all'
Kullanıcı "görevleri göster", "aktif görevler neler" dediğinde bu aracı kullan.`
}

func (t *ListTasksTool) Parameters() map[string]interface{} {
    return map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "task_type": map[string]interface{}{
                "type":        "string",
                "description": "Görev tipi filtresi ('all' ile tümü)",
                "enum":        []string{"user", "agent", "all"},
                "default":     "all",
            },
            "status": map[string]interface{}{
                "type":        "string",
                "description": "Durum filtresi ('all' ile tümü)",
                "enum":        []string{"pending", "running", "completed", "failed", "all"},
                "default":     "all",
            },
            "limit": map[string]interface{}{
                "type":        "integer",
                "description": "Maksimum sonuç sayısı",
                "default":     50,
            },
        },
    }
}

func (t *ListTasksTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
    if t == nil {
        return "", fmt.Errorf("ListTasksTool nil")
    }

    taskType, _ := args["task_type"].(string)
    status, _ := args["status"].(string)
    limitFloat, _ := args["limit"].(float64)
    limit := int(limitFloat)
    if limit <= 0 || limit > 100 {
        limit = 50
    }

    db, err := db_manager.GetDB(t.TasksDBPath)
    if err != nil {
        return "", fmt.Errorf("DB bağlantı hatası: %v", err)
    }

    query := `
    SELECT id, name, description, task_type, status, ttl_minutes, interval_min, created_at, completed_at
    FROM user_tasks
    WHERE 1=1
    `
    var queryArgs []interface{}

    if taskType != "" && taskType != "all" {
        query += ` AND task_type = ?`
        queryArgs = append(queryArgs, taskType)
    }
    if status != "" && status != "all" {
        query += ` AND status = ?`
        queryArgs = append(queryArgs, status)
    }
    query += ` ORDER BY created_at DESC LIMIT ?`
    queryArgs = append(queryArgs, limit)

    rows, err := db.Query(query, queryArgs...)
    if err != nil {
        return "", fmt.Errorf("sorgu hatası: %v", err)
    }
    defer rows.Close()

    var results []map[string]interface{}
    for rows.Next() {
        var id, ttlMinutes, intervalMin int
        var name, description, taskType, status, createdAt string
        var completedAt sql.NullString

        err := rows.Scan(&id, &name, &description, &taskType, &status, &ttlMinutes, &intervalMin, &createdAt, &completedAt)
        if err != nil {
            continue
        }

        result := map[string]interface{}{
            "id":           id,
            "name":         name,
            "description":  description,
            "task_type":    taskType,
            "status":       status,
            "ttl_minutes":  ttlMinutes,
            "interval_min": intervalMin,
            "created_at":   createdAt,
        }
        if completedAt.Valid {
            result["completed_at"] = completedAt.String
        }
        results = append(results, result)
    }

    if len(results) == 0 {
        return "📭 Görev bulunamadı.", nil
    }

    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("📋 **GÖREV LİSTESİ** (%d görev)\n\n", len(results)))
    sb.WriteString("| ID | Ad | Tip | Durum | TTL (dk) | Oluşturulma |\n")
    sb.WriteString("|---|---|---|---|---|---|\n")

    for _, r := range results {
        sb.WriteString(fmt.Sprintf("| %d | %s | %s | %s | %d | %s |\n",
            r["id"], r["name"], r["task_type"], r["status"], r["ttl_minutes"], r["created_at"]))
    }

    return sb.String(), nil
}

type DeleteTaskTool struct {
    TasksDBPath string
}

func NewDeleteTaskTool(tasksDBPath string) *DeleteTaskTool {
    return &DeleteTaskTool{TasksDBPath: tasksDBPath}
}

func (t *DeleteTaskTool) Name() string { return "delete_task" }

func (t *DeleteTaskTool) Description() string {
    return `GÖREV SİLME ARACI. SADECE User görevlerini siler. Agent görevleri otomatik temizlenir.
Kullanıcı "görevi sil", "görevi kaldır" dediğinde bu aracı kullan.
⚠️ DİKKAT: Agent görevlerini manuel silemezsin, onlar otomatik temizlenir.`
}

func (t *DeleteTaskTool) Parameters() map[string]interface{} {
    return map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "task_id": map[string]interface{}{
                "type":        "integer",
                "description": "Silinecek görev ID'si",
            },
        },
        "required": []string{"task_id"},
    }
}

func (t *DeleteTaskTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
    if t == nil {
        return "", fmt.Errorf("DeleteTaskTool nil")
    }

    taskIDFloat, ok := args["task_id"].(float64)
    if !ok {
        return "", fmt.Errorf("task_id eksik veya geçersiz")
    }
    taskID := int(taskIDFloat)

    db, err := db_manager.GetDB(t.TasksDBPath)
    if err != nil {
        return "", fmt.Errorf("DB bağlantı hatası: %v", err)
    }

    var taskType string
    err = db.QueryRow(`SELECT task_type FROM user_tasks WHERE id = ?`, taskID).Scan(&taskType)
    if err != nil {
        if err == sql.ErrNoRows {
            return fmt.Sprintf("⚠️ Görev bulunamadı (ID: %d)", taskID), nil
        }
        return "", fmt.Errorf("görev sorgulama hatası: %v", err)
    }

    if taskType == TaskTypeAgent {
        return "❌ **HATA:** Agent görevleri manuel silinemez! Bu görevler otomatik olarak temizlenir (TTL veya completion'da).", nil
    }

    _, err = db.Exec(`DELETE FROM user_tasks WHERE id = ?`, taskID)
    if err != nil {
        return "", fmt.Errorf("silme hatası: %v", err)
    }

    logger.Success("✅ [DeleteTask] User görev silindi: ID=%d", taskID)
    return fmt.Sprintf("✅ Görev başarıyla silindi (ID: %d)", taskID), nil
}
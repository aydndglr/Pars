// internal/memory/sqlite_store.go
// 🚀 DÜZELTMELER: Nil checks, Error handling, Validation, Logging, Context timeout
// ⚠️ DİKKAT: db_manager bağlantı havuzunu kullanır, defer db.Close() YOK

package memory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/aydndglr/pars-agent-v3/internal/db_manager"
	_ "modernc.org/sqlite"
)

// 🚨 YENİ: Timeout sabitleri
const (
	DBQueryTimeout   = 30 * time.Second
	DBWriteTimeout   = 10 * time.Second
	MaxSearchLimit   = 100
	MaxContentLength = 1 * 1024 * 1024 // 1 MB
)

// 🚨 DÜZELTME: Struct tanımlamalarını EN ÜSTE taşı
// CodeChunk: RAG sisteminden dönecek olan kod parçasının yapısı
type CodeChunk struct {
	ProjectName string
	FilePath    string
	Content     string
	StartLine   int
	EndLine     int
}

// 🚨 DÜZELTME: RAGProjectStat - Fonksiyonlardan ÖNCE tanımlanmalı
type RAGProjectStat struct {
	ProjectName string
	FileCount   int
	ChunkCount  int
}

// 🚨 DÜZELTME: ChatMessage - Fonksiyonlardan ÖNCE tanımlanmalı
type ChatMessage struct {
	SessionID string
	Role      string
	Content   string
	CreatedAt string
}

// 🚨 DÜZELTME: SessionInfo - Fonksiyonlardan ÖNCE tanımlanmalı
type SessionInfo struct {
	SessionID    string
	MessageCount int
	LastActive   string
}

// SQLiteStore: SQLite veritabanı için hafıza ve RAG deposu
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore: Veritabanını merkezi db_manager üzerinden alır, Hafıza ve RAG (FTS5) tablolarını hazırlar
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	// 🚨 DÜZELTME #1: Input validation
	if dbPath == "" {
		return nil, fmt.Errorf("dbPath boş olamaz")
	}

	// 🚀 Merkezi bağlantı havuzundan bağlantı al
	db, err := db_manager.GetDB(dbPath)
	if err != nil {
		logger.Error("❌ [SQLiteStore] DB bağlantısı alınamadı: %v", err)
		return nil, fmt.Errorf("hafıza veritabanı bağlantısı alınamadı: %v", err)
	}

	// 🚨 DÜZELTME #2: DB nil kontrolü
	if db == nil {
		return nil, fmt.Errorf("db bağlantısı nil")
	}

	// 1. ORİJİNAL HAFIZA TABLOSU
	queryMem := `
	CREATE TABLE IF NOT EXISTS memories (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		content TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(queryMem); err != nil {
		logger.Error("❌ [SQLiteStore] Hafıza tablosu oluşturulamadı: %v", err)
		return nil, fmt.Errorf("hafıza tablosu oluşturulamadı: %v", err)
	}

	// 2. MULTI-PROJECT RAG SİSTEMİ (FTS5)
	queryFTS := `
	CREATE VIRTUAL TABLE IF NOT EXISTS code_index USING fts5(
		project_name,
		file_path,
		content,
		start_line UNINDEXED,
		end_line UNINDEXED
	);`
	if _, err := db.Exec(queryFTS); err != nil {
		logger.Error("❌ [SQLiteStore] RAG (FTS5) tablosu oluşturulamadı: %v", err)
		return nil, fmt.Errorf("RAG (FTS5) tablosu oluşturulamadı: %v", err)
	}

	// 3. KALICI SOHBET BİLİNCİ (CHAT LOGS)
	queryChatLogs := `
	CREATE TABLE IF NOT EXISTS chat_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT,
		role TEXT,
		content TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(queryChatLogs); err != nil {
		logger.Error("❌ [SQLiteStore] Chat logs tablosu oluşturulamadı: %v", err)
		return nil, fmt.Errorf("chat_logs tablosu oluşturulamadı: %v", err)
	}

	logger.Success("✅ [SQLiteStore] Hafıza merkezi aktif: %s", dbPath)
	return &SQLiteStore{db: db}, nil
}

// =====================================================================
// 🧠 1. BÖLÜM: UZUN SÜRELİ HAFIZA (GÖREV GEÇMİŞİ)
// =====================================================================

// Add: Pars'in görev sonu raporlarını doğrudan kaydeder
func (s *SQLiteStore) Add(ctx context.Context, content string, metadata map[string]interface{}) error {
	// 🚨 DÜZELTME #3: Nil check
	if s == nil || s.db == nil {
		return fmt.Errorf("sqliteStore nil")
	}

	// 🚨 DÜZELTME #4: Input validation
	if strings.TrimSpace(content) == "" {
		logger.Debug("⚠️ [SQLiteStore] Boş content kaydedilmedi")
		return nil
	}

	// 🚨 DÜZELTME #5: Content length check
	if len(content) > MaxContentLength {
		content = content[:MaxContentLength]
		logger.Warn("⚠️ [SQLiteStore] Content kırpıldı (%d karakter)", MaxContentLength)
	}

	// 🚨 DÜZELTME #6: Timeout'lu context
	ctx, cancel := context.WithTimeout(ctx, DBWriteTimeout)
	defer cancel()

	query := `INSERT INTO memories (content) VALUES (?)`
	_, err := s.db.ExecContext(ctx, query, content)
	if err != nil {
		logger.Error("❌ [SQLiteStore] Memory write hatası: %v", err)
		return fmt.Errorf("memory write hatası: %v", err)
	}

	logger.Debug("✅ [SQLiteStore] Memory kaydı eklendi: %d karakter", len(content))
	return nil
}

// Search: Pars yeni bir göreve başlarken geçmişi tarar
func (s *SQLiteStore) Search(ctx context.Context, query string, limit int) ([]string, error) {
	// 🚨 DÜZELTME #7: Nil check
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("sqliteStore nil")
	}

	// 🚨 DÜZELTME #8: Input validation
	if strings.TrimSpace(query) == "" {
		return []string{}, nil
	}

	// 🚨 DÜZELTME #9: Limit validation
	if limit <= 0 || limit > MaxSearchLimit {
		limit = 10 // Varsayılan limit
	}

	// 🚨 DÜZELTME #10: Timeout'lu context
	ctx, cancel := context.WithTimeout(ctx, DBQueryTimeout)
	defer cancel()

	words := strings.Fields(query)
	sqlQuery := `SELECT content FROM memories WHERE 1=1`
	var args []interface{}

	for _, word := range words {
		if len(word) > 3 {
			sqlQuery += ` AND content LIKE ?`
			args = append(args, "%"+word+"%")
		}
	}

	sqlQuery += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		logger.Debug("⚠️ [SQLiteStore] Search hatası, fallback deneniyor: %v", err)
		// Fallback: Basit sorgu
		rows, err = s.db.QueryContext(ctx, `SELECT content FROM memories ORDER BY created_at DESC LIMIT ?`, limit)
		if err != nil {
			logger.Error("❌ [SQLiteStore] Fallback search hatası: %v", err)
			return nil, err
		}
	}
	defer rows.Close()

	var results []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err == nil {
			results = append(results, content)
		}
	}

	// 🚨 DÜZELTME #11: Rows error check
	if err := rows.Err(); err != nil {
		logger.Error("❌ [SQLiteStore] Rows iteration hatası: %v", err)
		return nil, err
	}

	logger.Debug("✅ [SQLiteStore] Search tamamlandı: %d sonuç", len(results))
	return results, nil
}

// =====================================================================
// 🚀 2. BÖLÜM: RAG (CODEBASE INDEXING) MOTORU
// =====================================================================

// ClearProjectIndex: Belirli bir projenin RAG verilerini siler
func (s *SQLiteStore) ClearProjectIndex(ctx context.Context, projectName string) error {
	// 🚨 DÜZELTME #12: Nil check
	if s == nil || s.db == nil {
		return fmt.Errorf("sqliteStore nil")
	}

	// 🚨 DÜZELTME #13: Timeout'lu context
	ctx, cancel := context.WithTimeout(ctx, DBWriteTimeout)
	defer cancel()

	var err error
	if projectName == "" {
		_, err = s.db.ExecContext(ctx, `DELETE FROM code_index`)
		logger.Info("🧹 [SQLiteStore] Tüm RAG verileri temizlendi")
	} else {
		_, err = s.db.ExecContext(ctx, `DELETE FROM code_index WHERE project_name = ?`, projectName)
		logger.Info("🧹 [SQLiteStore] Proje RAG verileri temizlendi: %s", projectName)
	}

	if err != nil {
		logger.Error("❌ [SQLiteStore] ClearProjectIndex hatası: %v", err)
		return fmt.Errorf("clear project index hatası: %v", err)
	}

	return nil
}

// AddCodeChunk: Kod parçasını proje adıyla FTS5 tablosuna gömer
func (s *SQLiteStore) AddCodeChunk(ctx context.Context, projectName, filePath, content string, startLine, endLine int) error {
	// 🚨 DÜZELTME #14: Nil check
	if s == nil || s.db == nil {
		return fmt.Errorf("sqliteStore nil")
	}

	// 🚨 DÜZELTME #15: Input validation
	if strings.TrimSpace(projectName) == "" {
		return fmt.Errorf("projectName boş olamaz")
	}

	if strings.TrimSpace(filePath) == "" {
		return fmt.Errorf("filePath boş olamaz")
	}

	if strings.TrimSpace(content) == "" {
		logger.Debug("⚠️ [SQLiteStore] Boş content indekslenmedi")
		return nil
	}

	// 🚨 DÜZELTME #16: Line number validation
	if startLine < 1 || endLine < startLine {
		logger.Warn("⚠️ [SQLiteStore] Geçersiz satır numaraları: %d-%d", startLine, endLine)
		startLine = 1
		endLine = 1
	}

	// 🚨 DÜZELTME #17: Timeout'lu context
	ctx, cancel := context.WithTimeout(ctx, DBWriteTimeout)
	defer cancel()

	query := `INSERT INTO code_index (project_name, file_path, content, start_line, end_line) VALUES (?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, projectName, filePath, content, startLine, endLine)
	if err != nil {
		logger.Debug("⚠️ [SQLiteStore] AddCodeChunk hatası: %v", err)
		return fmt.Errorf("add code chunk hatası: %v", err)
	}

	logger.Debug("✅ [SQLiteStore] Code chunk eklendi: %s (%d-%d)", filePath, startLine, endLine)
	return nil
}

// SearchCode: FTS5 MATCH operatörü ile projede ışık hızında filtreli kod bulur
func (s *SQLiteStore) SearchCode(ctx context.Context, projectName, query string, limit int) ([]CodeChunk, error) {
	// 🚨 DÜZELTME #18: Nil check
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("sqliteStore nil")
	}

	// 🚨 DÜZELTME #19: Input validation
	if strings.TrimSpace(query) == "" {
		return []CodeChunk{}, nil
	}

	// 🚨 DÜZELTME #20: Limit validation
	if limit <= 0 || limit > MaxSearchLimit {
		limit = 10
	}

	// 🚨 DÜZELTME #21: Timeout'lu context
	ctx, cancel := context.WithTimeout(ctx, DBQueryTimeout)
	defer cancel()

	words := strings.Fields(query)
	var matchTerms []string
	for _, w := range words {
		w = strings.ReplaceAll(w, "\"", "")
		w = strings.ReplaceAll(w, "'", "")
		w = strings.ReplaceAll(w, "-", " ")
		w = strings.ReplaceAll(w, "*", " ")
		w = strings.TrimSpace(w)
		if len(w) > 2 {
			matchTerms = append(matchTerms, w)
		}
	}

	if len(matchTerms) == 0 {
		logger.Debug("⚠️ [SQLiteStore] Geçerli arama terimi bulunamadı")
		return []CodeChunk{}, nil
	}

	matchQuery := strings.Join(matchTerms, " OR ")

	if projectName != "" {
		safeProject := strings.ReplaceAll(projectName, "\"", "")
		matchQuery = fmt.Sprintf("project_name: \"%s\" AND (%s)", safeProject, matchQuery)
	}

	sqlQuery := `
		SELECT project_name, file_path, content, start_line, end_line
		FROM code_index
		WHERE code_index MATCH ?
		ORDER BY rank
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, sqlQuery, matchQuery, limit)
	if err != nil {
		logger.Debug("⚠️ [SQLiteStore] FTS5 hatası, fallback deneniyor: %v", err)
		return s.searchCodeFallback(ctx, projectName, matchTerms, limit)
	}
	defer rows.Close()

	var results []CodeChunk
	for rows.Next() {
		var chunk CodeChunk
		if err := rows.Scan(&chunk.ProjectName, &chunk.FilePath, &chunk.Content, &chunk.StartLine, &chunk.EndLine); err == nil {
			results = append(results, chunk)
		}
	}

	if err := rows.Err(); err != nil {
		logger.Error("❌ [SQLiteStore] Rows iteration hatası: %v", err)
		return nil, err
	}

	logger.Debug("✅ [SQLiteStore] SearchCode tamamlandı: %d sonuç", len(results))
	return results, nil
}

// searchCodeFallback: MATCH operatörü patlarsa LIKE ile proje bazlı arama yapar
func (s *SQLiteStore) searchCodeFallback(ctx context.Context, projectName string, terms []string, limit int) ([]CodeChunk, error) {
	sqlQuery := `SELECT project_name, file_path, content, start_line, end_line FROM code_index WHERE 1=1`
	var args []interface{}

	if projectName != "" {
		sqlQuery += ` AND project_name = ?`
		args = append(args, projectName)
	}

	for _, term := range terms {
		sqlQuery += ` AND content LIKE ?`
		args = append(args, "%"+term+"%")
	}
	sqlQuery += ` LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []CodeChunk
	for rows.Next() {
		var chunk CodeChunk
		if err := rows.Scan(&chunk.ProjectName, &chunk.FilePath, &chunk.Content, &chunk.StartLine, &chunk.EndLine); err == nil {
			results = append(results, chunk)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// =====================================================================
// 📊 3. BÖLÜM: RAG ZİHİN ANALİZİ (İSTATİSTİKLER)
// =====================================================================

// GetRAGProjectsStats: FTS5 tablosundaki projeleri gruplayarak dosya ve parça sayılarını hesaplar
func (s *SQLiteStore) GetRAGProjectsStats(ctx context.Context) ([]RAGProjectStat, error) {
	// 🚨 DÜZELTME #22: Nil check
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("sqliteStore nil")
	}

	// 🚨 DÜZELTME #23: Timeout'lu context
	ctx, cancel := context.WithTimeout(ctx, DBQueryTimeout)
	defer cancel()

	query := `
		SELECT project_name, COUNT(DISTINCT file_path) as file_count, COUNT(*) as chunk_count
		FROM code_index
		GROUP BY project_name
		ORDER BY project_name ASC
	`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		logger.Error("❌ [SQLiteStore] GetRAGProjectsStats hatası: %v", err)
		return nil, err
	}
	defer rows.Close()

	var stats []RAGProjectStat
	for rows.Next() {
		var stat RAGProjectStat
		if err := rows.Scan(&stat.ProjectName, &stat.FileCount, &stat.ChunkCount); err == nil {
			stats = append(stats, stat)
		}
	}

	if err := rows.Err(); err != nil {
		logger.Error("❌ [SQLiteStore] Rows iteration hatası: %v", err)
		return nil, err
	}

	logger.Debug("✅ [SQLiteStore] RAG stats alındı: %d proje", len(stats))
	return stats, nil
}

// =====================================================================
// 💬 4. BÖLÜM: KALICI SOHBET BİLİNCİ (CHAT LOGS)
// =====================================================================

// AddChatMessage: Sohbet mesajlarını veritabanına anlık olarak işler
func (s *SQLiteStore) AddChatMessage(ctx context.Context, sessionID, role, content string) error {
	// 🚨 DÜZELTME #24: Nil check
	if s == nil || s.db == nil {
		return fmt.Errorf("sqliteStore nil")
	}

	// 🚨 DÜZELTME #25: Input validation
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("sessionID boş olamaz")
	}

	if strings.TrimSpace(role) == "" {
		return fmt.Errorf("role boş olamaz")
	}

	if strings.TrimSpace(content) == "" {
		logger.Debug("⚠️ [SQLiteStore] Boş chat message kaydedilmedi")
		return nil
	}

	// 🚨 DÜZELTME #26: Content length check
	if len(content) > MaxContentLength {
		content = content[:MaxContentLength]
		logger.Warn("⚠️ [SQLiteStore] Chat message kırpıldı (%d karakter)", MaxContentLength)
	}

	// 🚨 DÜZELTME #27: Timeout'lu context
	ctx, cancel := context.WithTimeout(ctx, DBWriteTimeout)
	defer cancel()

	query := `INSERT INTO chat_logs (session_id, role, content) VALUES (?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, sessionID, role, content)
	if err != nil {
		logger.Debug("⚠️ [SQLiteStore] AddChatMessage hatası: %v", err)
		return fmt.Errorf("add chat message hatası: %v", err)
	}

	return nil
}

// GetSessionChat: Belirli bir oturumun geçmişini getirir
func (s *SQLiteStore) GetSessionChat(ctx context.Context, sessionID string) ([]ChatMessage, error) {
	// 🚨 DÜZELTME #28: Nil check
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("sqliteStore nil")
	}

	// 🚨 DÜZELTME #29: Input validation
	if strings.TrimSpace(sessionID) == "" {
		return []ChatMessage{}, nil
	}

	// 🚨 DÜZELTME #30: Timeout'lu context
	ctx, cancel := context.WithTimeout(ctx, DBQueryTimeout)
	defer cancel()

	query := `SELECT session_id, role, content, created_at FROM chat_logs WHERE session_id = ? ORDER BY id ASC`
	rows, err := s.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		logger.Error("❌ [SQLiteStore] GetSessionChat hatası: %v", err)
		return nil, err
	}
	defer rows.Close()

	var msgs []ChatMessage
	for rows.Next() {
		var msg ChatMessage
		if err := rows.Scan(&msg.SessionID, &msg.Role, &msg.Content, &msg.CreatedAt); err == nil {
			msgs = append(msgs, msg)
		}
	}

	if err := rows.Err(); err != nil {
		logger.Error("❌ [SQLiteStore] Rows iteration hatası: %v", err)
		return nil, err
	}

	logger.Debug("✅ [SQLiteStore] Session chat alındı: %s (%d mesaj)", sessionID, len(msgs))
	return msgs, nil
}

// GetRecentSessions: Geçmiş sohbetleri listeler
func (s *SQLiteStore) GetRecentSessions(ctx context.Context, limit int) ([]SessionInfo, error) {
	// 🚨 DÜZELTME #31: Nil check
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("sqliteStore nil")
	}

	// 🚨 DÜZELTME #32: Limit validation
	if limit <= 0 || limit > MaxSearchLimit {
		limit = 20
	}

	// 🚨 DÜZELTME #33: Timeout'lu context
	ctx, cancel := context.WithTimeout(ctx, DBQueryTimeout)
	defer cancel()

	query := `
		SELECT session_id, COUNT(id) as msg_count, MAX(created_at) as last_active
		FROM chat_logs
		GROUP BY session_id
		ORDER BY last_active DESC
		LIMIT ?
	`
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		logger.Error("❌ [SQLiteStore] GetRecentSessions hatası: %v", err)
		return nil, err
	}
	defer rows.Close()

	var sessions []SessionInfo
	for rows.Next() {
		var info SessionInfo
		if err := rows.Scan(&info.SessionID, &info.MessageCount, &info.LastActive); err == nil {
			sessions = append(sessions, info)
		}
	}

	if err := rows.Err(); err != nil {
		logger.Error("❌ [SQLiteStore] Rows iteration hatası: %v", err)
		return nil, err
	}

	logger.Debug("✅ [SQLiteStore] Recent sessions alındı: %d oturum", len(sessions))
	return sessions, nil
}

// GetSessionsByDate: Belirli bir gündeki sohbetleri listeler
func (s *SQLiteStore) GetSessionsByDate(ctx context.Context, dateStr string) ([]SessionInfo, error) {
	// 🚨 DÜZELTME #34: Nil check
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("sqliteStore nil")
	}

	// 🚨 DÜZELTME #35: Input validation
	if strings.TrimSpace(dateStr) == "" {
		return []SessionInfo{}, nil
	}

	// 🚨 DÜZELTME #36: Timeout'lu context
	ctx, cancel := context.WithTimeout(ctx, DBQueryTimeout)
	defer cancel()

	query := `
		SELECT 
			c.session_id, 
			COUNT(c.id) as msg_count, 
			MAX(c.created_at) as last_active,
			COALESCE((SELECT content FROM chat_logs WHERE session_id = c.session_id AND role = 'user' ORDER BY id ASC LIMIT 1), 'Bilinmeyen Sohbet') as title
		FROM chat_logs c
		WHERE DATE(c.created_at) = ?
		GROUP BY c.session_id
		ORDER BY last_active DESC
	`
	rows, err := s.db.QueryContext(ctx, query, dateStr)
	if err != nil {
		logger.Error("❌ [SQLiteStore] GetSessionsByDate hatası: %v", err)
		return nil, err
	}
	defer rows.Close()

	var sessions []SessionInfo
	for rows.Next() {
		var info SessionInfo
		var title string
		if err := rows.Scan(&info.SessionID, &info.MessageCount, &info.LastActive, &title); err == nil {
			if len(title) > 50 {
				title = title[:47] + "..."
			}
			info.LastActive = fmt.Sprintf("[%s] Başlık: %s", info.LastActive, title)
			sessions = append(sessions, info)
		}
	}

	if err := rows.Err(); err != nil {
		logger.Error("❌ [SQLiteStore] Rows iteration hatası: %v", err)
		return nil, err
	}

	logger.Debug("✅ [SQLiteStore] Sessions by date alındı: %s (%d oturum)", dateStr, len(sessions))
	return sessions, nil
}
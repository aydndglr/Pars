/*
package heartbeat

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/aydndglr/pars-agent-v3/internal/db_manager" 
)

// SystemTask: Çekirdek (Kernel) seviyesinde, düzenli aralıklarla çalıştırılacak sistem görevlerini temsil eder.
type SystemTask struct {
	Name     string
	Interval time.Duration
	LastRun  time.Time
	Action   func()
}

// taskRecord: Veritabanından okunan kullanıcı görevlerini RAM'de tutmak için geçici yapı
type taskRecord struct {
	id          int
	name        string
	prompt      string
	intervalMin int
	lastRunStr  string
}

// HeartbeatService: Sistemin çift katmanlı nabız motorunu (Otonom Görev Yöneticisi) kontrol eder.
type HeartbeatService struct {
	interval       time.Duration
	stopChan       chan struct{}
	systemTasks    []*SystemTask
	db             *sql.DB
	userTaskRunner func(prompt string) // Pars'ın zihinsel sürecini tetikleyen ana fonksiyon
	mu             sync.Mutex
	runningTasks   map[int]bool // Eşzamanlılık (Concurrency) çakışmalarını önlemek için aktif görev kilit kayıtları
}

// NewHeartbeatService: Merkezi bağlantı havuzunu kullanarak yeni bir otonom görev yöneticisi örneği oluşturur.
func NewHeartbeatService(pulseInterval time.Duration, dbPath string, runner func(string)) *HeartbeatService {
	// 🚀 DEĞİŞİKLİK: Doğrudan sql.Open() yerine, merkezi yöneticiden (Safe-Queue) bağlantı talep ediyoruz.
	db, err := db_manager.GetDB(dbPath)
	if err != nil {
		logger.Error("❌ Heartbeat DB bağlantısı kurulamadı: %v", err)
	}

	// Kullanıcı görevlerini tutacak çekirdek tabloyu (eğer yoksa) oluşturur.
	query := `
	CREATE TABLE IF NOT EXISTS user_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		description TEXT,
		prompt TEXT,
		interval_min INTEGER,
		last_run DATETIME DEFAULT CURRENT_TIMESTAMP,
		is_completed BOOLEAN DEFAULT 0
	);`
	
	if db != nil {
		if _, err := db.Exec(query); err != nil {
			logger.Error("❌ Heartbeat tablosu oluşturulamadı: %v", err)
		}
	}

	return &HeartbeatService{
		interval:       pulseInterval,
		stopChan:       make(chan struct{}),
		systemTasks:    []*SystemTask{},
		db:             db,
		userTaskRunner: runner,
		runningTasks:   make(map[int]bool),
	}
}

// RegisterSystemTask: Sistem seviyesinde, kesintisiz çalışması gereken sabit bir Kernel görevi ekler.
func (s *HeartbeatService) RegisterSystemTask(name string, interval time.Duration, action func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.systemTasks = append(s.systemTasks, &SystemTask{
		Name:     name,
		Interval: interval,
		Action:   action,
	})
	logger.Success("⚙️ Sistem Görevi Eklendi: %s (Aralık: %v)", name, interval)
}

// Start: Nabız motorunu (Heartbeat) asenkron olarak başlatır.
func (s *HeartbeatService) Start(ctx context.Context) {
	logger.Info("💓 Pars Çift Katmanlı Kalp Atışı Başlatıldı (Mikro-DB: pars_tasks.db).")
	ticker := time.NewTicker(s.interval)

	go func() {
		for {
			select {
			case <-ticker.C:
				s.pulse()
			case <-s.stopChan:
				// 🚀 DİKKAT: Artık s.db.Close() yapmıyoruz çünkü bağlantı merkezi havuzda yönetiliyor!
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// pulse: Her döngüde (nabız atışında) önce sistem görevlerini, ardından kullanıcı görevlerini denetler ve icra eder.
func (s *HeartbeatService) pulse() {
	now := time.Now()

	// 1. SİSTEM GÖREVLERİNİ DENETLE
	s.mu.Lock()
	for _, task := range s.systemTasks {
		if now.Sub(task.LastRun) >= task.Interval {
			logger.Action("⚙️ KERNEL GÖREVİ: %s", task.Name)
			go task.Action() // Sistemi bloklamamak için goroutine kullanıyoruz
			task.LastRun = now
		}
	}
	s.mu.Unlock()

	if s.db == nil {
		return
	}

	// 2. KULLANICI GÖREVLERİNİ VERİTABANINDAN ÇEK
	// Sadece 'is_completed = 0' olan (tamamlanmamış) görevler taranır.
	rows, err := s.db.Query(`SELECT id, name, prompt, interval_min, last_run FROM user_tasks WHERE is_completed = 0`)
	if err != nil {
		logger.Error("❌ Otonom görevler okunamadı: %v", err)
		return
	}

	// 🚀 DEADLOCK (KİLİTLENME) ÖNLEYİCİ: MaxOpenConns(1) olduğu için, 'rows' açıkken 'Exec' yapamayız.
	// Önce tüm veriyi RAM'e alıp 'rows' nesnesini derhal kapatmalıyız.
	var pendingTasks []taskRecord
	for rows.Next() {
		var t taskRecord
		if err := rows.Scan(&t.id, &t.name, &t.prompt, &t.intervalMin, &t.lastRunStr); err == nil {
			pendingTasks = append(pendingTasks, t)
		}
	}
	rows.Close() // KİLİDİ SERBEST BIRAK!

	// 3. RAM'E ALINAN GÖREVLERİ ÇALIŞTIR
	for _, t := range pendingTasks {
		// SQLite veritabanından gelen zaman damgasını Go Time nesnesine dönüştür
		lastRun, err := time.Parse("2006-01-02 15:04:05", t.lastRunStr)
		if err != nil {
			lastRun = time.Time{}
		}

		s.mu.Lock()
		isRunning := s.runningTasks[t.id]
		s.mu.Unlock()

		if isRunning {
			logger.Warn("⚠️ Görev hali hazırda yürütülüyor, atlandı: %s", t.name)
			continue
		}

		// 🎯 ÇALIŞMA MANTIĞI VE ZAMANLAMA KONTROLÜ
		shouldRun := false
		isOneOff := (t.intervalMin == 0) // Aralık 0 ise, bu tek seferlik (One-Off) bir görevdir

		if isOneOff {
			shouldRun = true
		} else {
			intervalDur := time.Duration(t.intervalMin) * time.Minute
			if now.Sub(lastRun) >= intervalDur {
				shouldRun = true
			}
		}

		if shouldRun {
			logger.Action("👤 KULLANICI GÖREVİ TETİKLENDİ: %s", t.name)

			// Görevin aynı anda tekrar tetiklenmesini önlemek için RAM üzerinde kilit (lock) uygula
			s.mu.Lock()
			s.runningTasks[t.id] = true
			s.mu.Unlock()

			// 🚀 Artık rows kapalı olduğu için bu UPDATE işlemi kilitlenme yaratmaz!
			s.db.Exec(`UPDATE user_tasks SET last_run = CURRENT_TIMESTAMP WHERE id = ?`, t.id)

			// Görevi arka planda izole bir şekilde icra et
			go func(taskID int, taskName, taskPrompt string, selfDestruct bool) {
				if s.userTaskRunner != nil {
					s.userTaskRunner(taskPrompt) // Pars'ın beynini (LLM) harekete geçiren ana tetikleyici
				}

				// 🚀 GÖREV BİTİŞ PROTOKOLÜ:
				if selfDestruct {
					// Tek seferlik görevleri silmek yerine, 'tamamlandı' olarak arşivle
					s.db.Exec(`UPDATE user_tasks SET is_completed = 1 WHERE id = ?`, taskID)
					logger.Success("✅ Tek seferlik görev tamamlandı ve arşive kaldırıldı: %s", taskName)
				}

				// İşlem bittiğinde RAM üzerindeki görev kilidini serbest bırak
				s.mu.Lock()
				delete(s.runningTasks, taskID)
				s.mu.Unlock()
			}(t.id, t.name, t.prompt, isOneOff)
		}
	}
}
*/








// internal/core/heartbeat/heartbeat.go
// 🚀 DÜZELTME V3: Task Type Ayrımı + TTL + Zombie Avcısı (Garbage Collector)
// ⚠️ DİKKAT: Agent görevleri otomatik temizlenir, User görevleri kalıcıdır
// 📅 Oluşturulma: 2026-03-09 (Pars V5 Critical Fix #3)

package heartbeat

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/aydndglr/pars-agent-v3/internal/db_manager"
)

// 🆕 YENİ: TaskType - Görev tipi enum (string based)
type TaskType string

const (
	TaskTypeUser   TaskType = "user"   // Kullanıcı oluşturdu, sil diyene kadar kalır
	TaskTypeAgent  TaskType = "agent"  // Agent oluşturdu, TTL veya completion'da silinir
	TaskTypeSystem TaskType = "system" // Sistem görevi (heartbeat internal)
)

// 🆕 YENİ: TaskStatus - Görev durum enum
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"   // Beklemede
	TaskStatusRunning   TaskStatus = "running"   // Çalışıyor
	TaskStatusCompleted TaskStatus = "completed" // Tamamlandı
	TaskStatusFailed    TaskStatus = "failed"    // Başarısız
	TaskStatusStale     TaskStatus = "stale"     // Eski/Zombie (cleanup bekliyor)
)

// 🆕 YENİ: Timeout sabitleri
const (
	AgentTaskDefaultTTL      = 30 * time.Minute  // Agent görevleri için varsayılan TTL
	AgentTaskMaxTTL          = 24 * time.Hour    // Maksimum TTL
	StaleTaskCleanupInterval = 5 * time.Minute   // Zombie temizlik aralığı
	MaxConcurrentTasks       = 10                // Maksimum eşzamanlı görev
)

// SystemTask: Çekirdek (Kernel) seviyesinde, düzenli aralıklarla çalıştırılacak sistem görevlerini temsil eder.
type SystemTask struct {
	Name     string
	Interval time.Duration
	LastRun  time.Time
	Action   func()
}

// 🆕 YENİ: taskRecord - Veritabanından okunan kullanıcı görevlerini RAM'de tutmak için geçici yapı
type taskRecord struct {
	id          int
	name        string
	description string
	prompt      string
	intervalMin int
	lastRunStr  string
	isCompleted bool
	taskType    string // 🆕 YENİ: user/agent/system
	ttlMinutes  int    // 🆕 YENİ: Time-to-live (dakika)
	status      string // 🆕 YENİ: pending/running/completed/failed/stale
	createdBy   string // 🆕 YENİ: Session ID veya 'system'
	createdAtStr string // 🆕 YENİ: Oluşturulma zamanı
}

// HeartbeatService: Sistemin çift katmanlı nabız motorunu (Otonom Görev Yöneticisi) kontrol eder.
type HeartbeatService struct {
	interval       time.Duration
	stopChan       chan struct{}
	systemTasks    []*SystemTask
	db             *sql.DB
	userTaskRunner func(prompt string) // Pars'ın zihinsel sürecini tetikleyen ana fonksiyon
	mu             sync.Mutex
	runningTasks   map[int]bool // Eşzamanlılık (Concurrency) çakışmalarını önlemek için aktif görev kilit kayıtları
	
	// 🆕 YENİ: Zombie Cleanup için
	cleanupTicker   *time.Ticker
	cleanupStopChan chan struct{}
}

// 🆕 YENİ: NewHeartbeatService - Merkezi bağlantı havuzunu kullanarak yeni bir otonom görev yöneticisi örneği oluşturur.
func NewHeartbeatService(pulseInterval time.Duration, dbPath string, runner func(string)) *HeartbeatService {
	// 🚀 DEĞİŞİKLİK: Doğrudan sql.Open() yerine, merkezi yöneticiden (Safe-Queue) bağlantı talep ediyoruz.
	db, err := db_manager.GetDB(dbPath)
	if err != nil {
		logger.Error("❌ Heartbeat DB bağlantısı kurulamadı: %v", err)
	}

	// 🆕 YENİ: Kullanıcı görevlerini tutacak GENİŞLETİLMİŞ tabloyu oluşturur.
	query := `
	CREATE TABLE IF NOT EXISTS user_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		description TEXT,
		prompt TEXT NOT NULL,
		interval_min INTEGER DEFAULT 0,
		last_run DATETIME DEFAULT CURRENT_TIMESTAMP,
		is_completed BOOLEAN DEFAULT 0,
		-- 🆕 YENİ KOLONLAR:
		task_type TEXT DEFAULT 'user',        -- user/agent/system
		ttl_minutes INTEGER DEFAULT 0,        -- 0 = kalıcı, >0 = dakika cinsinden ömür
		status TEXT DEFAULT 'pending',        -- pending/running/completed/failed/stale
		created_by TEXT DEFAULT 'system',     -- Session ID veya 'system'
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		completed_at DATETIME
	);
	
	-- 🆕 YENİ: İndeksler (performans için)
	CREATE INDEX IF NOT EXISTS idx_task_type ON user_tasks(task_type);
	CREATE INDEX IF NOT EXISTS idx_status ON user_tasks(status);
	CREATE INDEX IF NOT EXISTS idx_created_at ON user_tasks(created_at);
	`
	
	if db != nil {
		if _, err := db.Exec(query); err != nil {
			logger.Error("❌ Heartbeat tablosu oluşturulamadı: %v", err)
		} else {
			logger.Success("✅ [Heartbeat] Task tablosu genişletildi (task_type, ttl, status kolonları eklendi)")
		}
	}

	hs := &HeartbeatService{
		interval:       pulseInterval,
		stopChan:       make(chan struct{}),
		systemTasks:    []*SystemTask{},
		db:             db,
		userTaskRunner: runner,
		runningTasks:   make(map[int]bool),
		cleanupStopChan: make(chan struct{}),
	}

	// 🆕 YENİ: Zombie Cleanup Worker'ı başlat
	go hs.startCleanupWorker()

	return hs
}

// 🆕 YENİ: startCleanupWorker - Arka planda periyodik olarak eski/zombie görevleri temizler
func (s *HeartbeatService) startCleanupWorker() {
	logger.Info("🧹 [Heartbeat] Zombie Task Cleanup Worker başlatıldı (Aralık: %v)", StaleTaskCleanupInterval)
	s.cleanupTicker = time.NewTicker(StaleTaskCleanupInterval)
	defer s.cleanupTicker.Stop()

	for {
		select {
		case <-s.cleanupTicker.C:
			s.cleanupStaleTasks()
		case <-s.cleanupStopChan:
			logger.Info("🛑 [Heartbeat] Zombie Cleanup Worker durduruldu")
			return
		}
	}
}

// 🆕 YENİ: cleanupStaleTasks - TTL'i dolmuş agent görevlerini temizler
func (s *HeartbeatService) cleanupStaleTasks() {
	if s.db == nil {
		return
	}

	// 🆕 YENİ: TTL'i dolmuş agent görevlerini bul
	// TTL > 0 VE (completed_at + TTL < NOW) VEYA (status = 'running' VE created_at + TTL < NOW)
	query := `
	SELECT id, name, task_type, ttl_minutes, status, created_at 
	FROM user_tasks 
	WHERE task_type = 'agent' 
	AND ttl_minutes > 0
	AND (
		(completed_at IS NOT NULL AND datetime(completed_at, '+' || ttl_minutes || ' minutes') < datetime('now'))
		OR
		(status = 'running' AND datetime(created_at, '+' || ttl_minutes || ' minutes') < datetime('now'))
	)
	LIMIT 100
	`

	rows, err := s.db.Query(query)
	if err != nil {
		logger.Debug("⚠️ [Heartbeat] Cleanup query hatası: %v", err)
		return
	}
	defer rows.Close()

	deletedCount := 0
	for rows.Next() {
		var id int
		var name, taskType, status, createdAt string
		var ttlMinutes int

		if err := rows.Scan(&id, &name, &taskType, &ttlMinutes, &status, &createdAt); err != nil {
			logger.Debug("⚠️ [Heartbeat] Cleanup scan hatası: %v", err)
			continue
		}

		// 🆕 YENİ: Agent görevini sil (user görevlerine dokunma!)
		_, err := s.db.Exec(`DELETE FROM user_tasks WHERE id = ? AND task_type = 'agent'`, id)
		if err != nil {
			logger.Warn("⚠️ [Heartbeat] Zombie task silinemedi (ID: %d): %v", id, err)
		} else {
			logger.Debug("🗑️ [Heartbeat] Zombie task temizlendi: %s (ID: %d, TTL: %d dk)", name, id, ttlMinutes)
			deletedCount++
		}
	}

	if deletedCount > 0 {
		logger.Info("🧹 [Heartbeat] Periyodik temizlik: %d zombie task silindi", deletedCount)
	}
}

// 🆕 YENİ: CreateTask - Yeni görev oluştur (User veya Agent)
func (s *HeartbeatService) CreateTask(name, description, prompt string, intervalMin int, taskType TaskType, ttlMinutes int, createdBy string) (int64, error) {
	if s.db == nil {
		return 0, fmt.Errorf("db bağlantısı yok")
	}

	// 🆕 YENİ: Validation
	if name == "" || prompt == "" {
		return 0, fmt.Errorf("name ve prompt zorunlu")
	}

	// Task type validation
	if taskType == "" {
		taskType = TaskTypeUser // Varsayılan
	}

	// TTL validation
	if taskType == TaskTypeAgent && ttlMinutes <= 0 {
		ttlMinutes = int(AgentTaskDefaultTTL.Minutes()) // Agent görevleri için varsayılan TTL
	}

	if taskType == TaskTypeUser {
		ttlMinutes = 0 // User görevleri kalıcı
	}

	if ttlMinutes > int(AgentTaskMaxTTL.Minutes()) {
		ttlMinutes = int(AgentTaskMaxTTL.Minutes()) // Maksimum TTL limiti
	}

	// CreatedBy validation
	if createdBy == "" {
		createdBy = "system"
	}

	query := `
	INSERT INTO user_tasks (name, description, prompt, interval_min, task_type, ttl_minutes, status, created_by)
	VALUES (?, ?, ?, ?, ?, ?, 'pending', ?)
	`

	result, err := s.db.Exec(query, name, description, prompt, intervalMin, taskType, ttlMinutes, createdBy)
	if err != nil {
		logger.Error("❌ [Heartbeat] Task oluşturulamadı: %v", err)
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		logger.Error("❌ [Heartbeat] Task ID alınamadı: %v", err)
		return 0, err
	}

	logger.Success("✅ [Heartbeat] Task oluşturuldu: %s (Type: %s, TTL: %d dk, CreatedBy: %s)", 
		name, taskType, ttlMinutes, createdBy)

	return id, nil
}

// 🆕 YENİ: UpdateTaskStatus - Görev durumunu güncelle
func (s *HeartbeatService) UpdateTaskStatus(taskID int, status TaskStatus) error {
	if s.db == nil {
		return fmt.Errorf("db bağlantısı yok")
	}

	query := `UPDATE user_tasks SET status = ? WHERE id = ?`
	
	// 🆕 YENİ: Completed ise completed_at'i set et
	if status == TaskStatusCompleted {
		query = `UPDATE user_tasks SET status = ?, completed_at = CURRENT_TIMESTAMP WHERE id = ?`
	}

	_, err := s.db.Exec(query, status, taskID)
	if err != nil {
		logger.Error("❌ [Heartbeat] Task status güncellenemedi (ID: %d): %v", taskID, err)
		return err
	}

	logger.Debug("🔄 [Heartbeat] Task status güncellendi: ID=%d, Status=%s", taskID, status)
	return nil
}

// 🆕 YENİ: GetTask - Tek görev bilgisi al
func (s *HeartbeatService) GetTask(taskID int) (*taskRecord, error) {
	if s.db == nil {
		return nil, fmt.Errorf("db bağlantısı yok")
	}

	query := `
	SELECT id, name, description, prompt, interval_min, last_run, is_completed, 
	       task_type, ttl_minutes, status, created_by, created_at
	FROM user_tasks WHERE id = ?
	`

	row := s.db.QueryRow(query, taskID)
	
	var t taskRecord
	err := row.Scan(&t.id, &t.name, &t.description, &t.prompt, &t.intervalMin, 
		&t.lastRunStr, &t.isCompleted, &t.taskType, &t.ttlMinutes, &t.status, 
		&t.createdBy, &t.createdAtStr)
	
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("task bulunamadı: %d", taskID)
		}
		return nil, err
	}

	return &t, nil
}

// 🆕 YENİ: DeleteTask - Görevi sil (sadece user görevleri için)
func (s *HeartbeatService) DeleteTask(taskID int) error {
	if s.db == nil {
		return fmt.Errorf("db bağlantısı yok")
	}

	// 🆕 YENİ: Önce task type'ı kontrol et (agent görevleri otomatik silinir)
	task, err := s.GetTask(taskID)
	if err != nil {
		return err
	}

	if task.taskType == string(TaskTypeAgent) {
		logger.Warn("⚠️ [Heartbeat] Agent görevleri manuel silinemez, otomatik temizlenir: %s", task.name)
		return fmt.Errorf("agent görevleri manuel silinemez")
	}

	_, err = s.db.Exec(`DELETE FROM user_tasks WHERE id = ?`, taskID)
	if err != nil {
		logger.Error("❌ [Heartbeat] Task silinemedi (ID: %d): %v", taskID, err)
		return err
	}

	logger.Success("🗑️ [Heartbeat] User task silindi: %s (ID: %d)", task.name, taskID)
	return nil
}

// 🆕 YENİ: ListTasks - Görevleri listele (filtreli)
func (s *HeartbeatService) ListTasks(taskType TaskType, status TaskStatus, limit int) ([]taskRecord, error) {
	if s.db == nil {
		return nil, fmt.Errorf("db bağlantısı yok")
	}

	if limit <= 0 || limit > 100 {
		limit = 50 // Varsayılan limit
	}

	query := `
	SELECT id, name, description, prompt, interval_min, last_run, is_completed,
	       task_type, ttl_minutes, status, created_by, created_at
	FROM user_tasks WHERE 1=1
	`

	var args []interface{}

	if taskType != "" {
		query += ` AND task_type = ?`
		args = append(args, taskType)
	}

	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}

	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		logger.Error("❌ [Heartbeat] Task listesi alınamadı: %v", err)
		return nil, err
	}
	defer rows.Close()

	var tasks []taskRecord
	for rows.Next() {
		var t taskRecord
		if err := rows.Scan(&t.id, &t.name, &t.description, &t.prompt, &t.intervalMin,
			&t.lastRunStr, &t.isCompleted, &t.taskType, &t.ttlMinutes, &t.status,
			&t.createdBy, &t.createdAtStr); err == nil {
			tasks = append(tasks, t)
		}
	}

	return tasks, nil
}

// RegisterSystemTask: Sistem seviyesinde, kesintisiz çalışması gereken sabit bir Kernel görevi ekler.
func (s *HeartbeatService) RegisterSystemTask(name string, interval time.Duration, action func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.systemTasks = append(s.systemTasks, &SystemTask{
		Name:     name,
		Interval: interval,
		Action:   action,
	})
	logger.Success("⚙️ Sistem Görevi Eklendi: %s (Aralık: %v)", name, interval)
}

// Start: Nabız motorunu (Heartbeat) asenkron olarak başlatır.
func (s *HeartbeatService) Start(ctx context.Context) {
	logger.Info("💓 Pars Çift Katmanlı Kalp Atışı Başlatıldı (Mikro-DB: pars_tasks.db).")
	ticker := time.NewTicker(s.interval)

	go func() {
		for {
			select {
			case <-ticker.C:
				s.pulse()
			case <-s.stopChan:
				// 🚀 DİKKAT: Artık s.db.Close() yapmıyoruz çünkü bağlantı merkezi havuzda yönetiliyor!
				// 🆕 YENİ: Cleanup worker'ı durdur
				close(s.cleanupStopChan)
				return
			case <-ctx.Done():
				close(s.cleanupStopChan)
				return
			}
		}
	}()
}

// Stop: Heartbeat servisini güvenli şekilde durdurur
func (s *HeartbeatService) Stop() {
	logger.Info("🛑 [Heartbeat] Servis durduruluyor...")
	close(s.stopChan)
	if s.cleanupTicker != nil {
		s.cleanupTicker.Stop()
	}
	close(s.cleanupStopChan)
	logger.Success("✅ [Heartbeat] Servis güvenli şekilde durduruldu")
}

// pulse: Her döngüde (nabız atışında) önce sistem görevlerini, ardından kullanıcı görevlerini denetler ve icra eder.
func (s *HeartbeatService) pulse() {
	now := time.Now()

	// 1. SİSTEM GÖREVLERİNİ DENETLE
	s.mu.Lock()
	for _, task := range s.systemTasks {
		if now.Sub(task.LastRun) >= task.Interval {
			logger.Action("⚙️ KERNEL GÖREVİ: %s", task.Name)
			go task.Action() // Sistemi bloklamamak için goroutine kullanıyoruz
			task.LastRun = now
		}
	}
	s.mu.Unlock()

	if s.db == nil {
		return
	}

	// 2. KULLANICI GÖREVLERİNİ VERİTABANINDAN ÇEK
	// 🆕 YENİ: Sadece 'status != completed' olan görevler taranır (is_completed yerine status kullan)
	rows, err := s.db.Query(`
		SELECT id, name, description, prompt, interval_min, last_run, is_completed,
		       task_type, ttl_minutes, status, created_by, created_at
		FROM user_tasks 
		WHERE status NOT IN ('completed', 'failed', 'stale')
	`)
	if err != nil {
		logger.Error("❌ Otonom görevler okunamadı: %v", err)
		return
	}

	// 🚀 DEADLOCK (KİLİTLENME) ÖNLEYİCİ: MaxOpenConns(1) olduğu için, 'rows' açıkken 'Exec' yapamayız.
	// Önce tüm veriyi RAM'e alıp 'rows' nesnesini derhal kapatmalıyız.
	var pendingTasks []taskRecord
	for rows.Next() {
		var t taskRecord
		if err := rows.Scan(&t.id, &t.name, &t.description, &t.prompt, &t.intervalMin, 
			&t.lastRunStr, &t.isCompleted, &t.taskType, &t.ttlMinutes, &t.status,
			&t.createdBy, &t.createdAtStr); err == nil {
			pendingTasks = append(pendingTasks, t)
		}
	}
	rows.Close() // KİLİDİ SERBEST BIRAK!

	// 3. RAM'E ALINAN GÖREVLERİ ÇALIŞTIR
	for _, t := range pendingTasks {
		// SQLite veritabanından gelen zaman damgasını Go Time nesnesine dönüştür
		lastRun, err := time.Parse("2006-01-02 15:04:05", t.lastRunStr)
		if err != nil {
			lastRun = time.Time{}
		}

		s.mu.Lock()
		isRunning := s.runningTasks[t.id]
		s.mu.Unlock()

		if isRunning {
			logger.Warn("⚠️ Görev hali hazırda yürütülüyor, atlandı: %s", t.name)
			continue
		}

		// 🎯 ÇALIŞMA MANTIĞI VE ZAMANLAMA KONTROLÜ
		shouldRun := false
		isOneOff := (t.intervalMin == 0) // Aralık 0 ise, bu tek seferlik (One-Off) bir görevdir

		if isOneOff {
			shouldRun = true
		} else {
			intervalDur := time.Duration(t.intervalMin) * time.Minute
			if now.Sub(lastRun) >= intervalDur {
				shouldRun = true
			}
		}

		if shouldRun {
			logger.Action("👤 KULLANICI GÖREVİ TETİKLENDİ: %s (Type: %s, TTL: %d dk)", 
				t.name, t.taskType, t.ttlMinutes)

			// 🆕 YENİ: Görevi 'running' olarak işaretle
			s.UpdateTaskStatus(t.id, TaskStatusRunning)

			// Görevin aynı anda tekrar tetiklenmesini önlemek için RAM üzerinde kilit (lock) uygula
			s.mu.Lock()
			s.runningTasks[t.id] = true
			s.mu.Unlock()

			// 🚀 Artık rows kapalı olduğu için bu UPDATE işlemi kilitlenme yaratmaz!
			s.db.Exec(`UPDATE user_tasks SET last_run = CURRENT_TIMESTAMP WHERE id = ?`, t.id)

			// Görevi arka planda izole bir şekilde icra et
			go func(taskID int, taskName, taskPrompt, taskType string, ttlMinutes int, selfDestruct bool) {
				if s.userTaskRunner != nil {
					s.userTaskRunner(taskPrompt) // Pars'ın beynini (LLM) harekete geçiren ana tetikleyici
				}

				// 🚀 GÖREV BİTİŞ PROTOKOLÜ:
				if selfDestruct {
					// 🆕 YENİ: Agent görevleri için farklı protokol
					if taskType == string(TaskTypeAgent) {
						// Agent görevi: 'completed' olarak işaretle, TTL sonunda otomatik silinecek
						s.UpdateTaskStatus(taskID, TaskStatusCompleted)
						logger.Success("✅ Agent görevi tamamlandı (TTL: %d dk sonra otomatik silinecek): %s", 
							ttlMinutes, taskName)
					} else {
						// User görevi: 'completed' olarak işaretle, kullanıcı sil diyene kadar kalır
						s.UpdateTaskStatus(taskID, TaskStatusCompleted)
						logger.Success("✅ User görevi tamamlandı (manuel silme bekleniyor): %s", taskName)
					}
				}

				// İşlem bittiğinde RAM üzerindeki görev kilidini serbest bırak
				s.mu.Lock()
				delete(s.runningTasks, taskID)
				s.mu.Unlock()
			}(t.id, t.name, t.prompt, t.taskType, t.ttlMinutes, isOneOff)
		}
	}
}
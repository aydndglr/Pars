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

type TaskType string

const (
	TaskTypeUser   TaskType = "user"  
	TaskTypeAgent  TaskType = "agent" 
	TaskTypeSystem TaskType = "system" 
)


type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"  
	TaskStatusRunning   TaskStatus = "running"   
	TaskStatusCompleted TaskStatus = "completed" 
	TaskStatusFailed    TaskStatus = "failed"    
	TaskStatusStale     TaskStatus = "stale"   
)

const (
	AgentTaskDefaultTTL      = 30 * time.Minute 
	AgentTaskMaxTTL          = 24 * time.Hour   
	StaleTaskCleanupInterval = 5 * time.Minute  
	MaxConcurrentTasks       = 10               
)

type SystemTask struct {
	Name     string
	Interval time.Duration
	LastRun  time.Time
	Action   func()
}

type taskRecord struct {
	id           int
	name         string
	description  string
	prompt       string
	intervalMin  int
	lastRunStr   string
	isCompleted  bool
	taskType     string 
	ttlMinutes   int   
	status       string 
	createdBy    string
	createdAtStr string
}

type HeartbeatService struct {
	interval         time.Duration
	stopChan         chan struct{}
	systemTasks      []*SystemTask
	db               *sql.DB
	userTaskRunner   func(prompt string) 
	mu               sync.Mutex
	runningTasks     map[int]bool 
	
	NotificationChan chan string

	cleanupTicker    *time.Ticker
	cleanupStopChan  chan struct{}
}

func NewHeartbeatService(pulseInterval time.Duration, dbPath string, runner func(string)) *HeartbeatService {
	db, err := db_manager.GetDB(dbPath)
	if err != nil {
		logger.Error("❌ Heartbeat DB bağlantısı kurulamadı: %v", err)
	}
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
		interval:         pulseInterval,
		stopChan:         make(chan struct{}),
		systemTasks:      []*SystemTask{},
		db:               db,
		userTaskRunner:   runner,
		runningTasks:     make(map[int]bool),
		NotificationChan: make(chan string, 100), 
		cleanupStopChan:  make(chan struct{}),
	}

	go hs.startCleanupWorker()

	return hs
}


func (s *HeartbeatService) notifyUser(msg string) {
	if s.NotificationChan != nil {
		select {
		case s.NotificationChan <- msg:
		default:
			logger.Warn("⚠️ Bildirim kuyruğu dolu, mesaj atlandı: %s", msg)
		}
	}
}

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

func (s *HeartbeatService) cleanupStaleTasks() {
	if s.db == nil {
		return
	}

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

	var tasksToDelete []int
	for rows.Next() {
		var id int
		var name, taskType, status, createdAt string
		var ttlMinutes int

		if err := rows.Scan(&id, &name, &taskType, &ttlMinutes, &status, &createdAt); err != nil {
			logger.Debug("⚠️ [Heartbeat] Cleanup scan hatası: %v", err)
			continue
		}
		tasksToDelete = append(tasksToDelete, id)
	}
	rows.Close() 

	deletedCount := 0
	for _, id := range tasksToDelete {
		_, err := s.db.Exec(`DELETE FROM user_tasks WHERE id = ? AND task_type = 'agent'`, id)
		if err != nil {
			logger.Warn("⚠️ [Heartbeat] Zombie task silinemedi (ID: %d): %v", id, err)
		} else {
			logger.Debug("🗑️ [Heartbeat] Zombie task temizlendi (ID: %d)", id)
			deletedCount++
		}
	}

	if deletedCount > 0 {
		logger.Info("🧹 [Heartbeat] Periyodik temizlik: %d zombie task silindi", deletedCount)
	}
}

func (s *HeartbeatService) CreateTask(name, description, prompt string, intervalMin int, taskType TaskType, ttlMinutes int, createdBy string) (int64, error) {
	if s.db == nil {
		return 0, fmt.Errorf("db bağlantısı yok")
	}

	if name == "" || prompt == "" {
		return 0, fmt.Errorf("name ve prompt zorunlu")
	}

	if taskType == "" {
		taskType = TaskTypeUser 
	}

	if taskType == TaskTypeAgent && ttlMinutes <= 0 {
		ttlMinutes = int(AgentTaskDefaultTTL.Minutes())
	}

	if taskType == TaskTypeUser {
		ttlMinutes = 0 
	}

	if ttlMinutes > int(AgentTaskMaxTTL.Minutes()) {
		ttlMinutes = int(AgentTaskMaxTTL.Minutes())
	}

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


func (s *HeartbeatService) UpdateTaskStatus(taskID int, status TaskStatus) error {
	if s.db == nil {
		return fmt.Errorf("db bağlantısı yok")
	}

	query := `UPDATE user_tasks SET status = ? WHERE id = ?`
	
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

func (s *HeartbeatService) DeleteTask(taskID int) error {
	if s.db == nil {
		return fmt.Errorf("db bağlantısı yok")
	}

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

func (s *HeartbeatService) ListTasks(taskType TaskType, status TaskStatus, limit int) ([]taskRecord, error) {
	if s.db == nil {
		return nil, fmt.Errorf("db bağlantısı yok")
	}

	if limit <= 0 || limit > 100 {
		limit = 50 
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

func (s *HeartbeatService) Start(ctx context.Context) {
	logger.Info("💓 Pars Çift Katmanlı Kalp Atışı Başlatıldı (Mikro-DB: pars_tasks.db).")
	ticker := time.NewTicker(s.interval)

	go func() {
		for {
			select {
			case <-ticker.C:
				s.pulse()
			case <-s.stopChan:
				close(s.cleanupStopChan)
				return
			case <-ctx.Done():
				close(s.cleanupStopChan)
				return
			}
		}
	}()
}

func (s *HeartbeatService) Stop() {
	logger.Info("🛑 [Heartbeat] Servis durduruluyor...")
	close(s.stopChan)
	if s.cleanupTicker != nil {
		s.cleanupTicker.Stop()
	}
	close(s.cleanupStopChan)
	logger.Success("✅ [Heartbeat] Servis güvenli şekilde durduruldu")
}

func (s *HeartbeatService) pulse() {
	now := time.Now()

	s.mu.Lock()
	for _, task := range s.systemTasks {
		if now.Sub(task.LastRun) >= task.Interval {
			logger.Action("⚙️ KERNEL GÖREVİ: %s", task.Name)
			go task.Action() 
			task.LastRun = now
		}
	}
	s.mu.Unlock()

	if s.db == nil {
		return
	}

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

	var pendingTasks []taskRecord
	for rows.Next() {
		var t taskRecord
		if err := rows.Scan(&t.id, &t.name, &t.description, &t.prompt, &t.intervalMin, 
			&t.lastRunStr, &t.isCompleted, &t.taskType, &t.ttlMinutes, &t.status,
			&t.createdBy, &t.createdAtStr); err == nil {
			pendingTasks = append(pendingTasks, t)
		}
	}
	rows.Close() 

	for _, t := range pendingTasks {
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

		shouldRun := false
		isOneOff := (t.intervalMin == 0) 

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
			s.UpdateTaskStatus(t.id, TaskStatusRunning)
			s.mu.Lock()
			s.runningTasks[t.id] = true
			s.mu.Unlock()
			s.db.Exec(`UPDATE user_tasks SET last_run = CURRENT_TIMESTAMP WHERE id = ?`, t.id)
			go func(taskID int, taskName, taskPrompt, taskType string, ttlMinutes int, selfDestruct bool) {
				if s.userTaskRunner != nil {
					s.userTaskRunner(taskPrompt) 
				}

				if selfDestruct {
					if taskType == string(TaskTypeAgent) {
						s.UpdateTaskStatus(taskID, TaskStatusCompleted)
						logger.Success("✅ Agent görevi tamamlandı (TTL: %d dk sonra otomatik silinecek): %s", 
							ttlMinutes, taskName)
						s.notifyUser(fmt.Sprintf("🔔 [GÖREV TAMAMLANDI]: '%s' isimli arka plan görevi başarıyla sonuçlandı!", taskName))
						
					} else {
						s.UpdateTaskStatus(taskID, TaskStatusCompleted)
						logger.Success("✅ User görevi tamamlandı (manuel silme bekleniyor): %s", taskName)
						
						s.notifyUser(fmt.Sprintf("🔔 [GÖREV TAMAMLANDI]: '%s' isimli tek seferlik görev tamamlandı!", taskName))
					}
				}

				s.mu.Lock()
				delete(s.runningTasks, taskID)
				s.mu.Unlock()
			}(t.id, t.name, t.prompt, t.taskType, t.ttlMinutes, isOneOff)
		}
	}
}
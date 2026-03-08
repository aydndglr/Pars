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
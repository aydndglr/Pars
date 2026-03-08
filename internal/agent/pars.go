package agent

import (
	"sync"
	"github.com/aydndglr/pars-agent-v3/internal/core/config"
	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/aydndglr/pars-agent-v3/internal/skills"
)

// Pars: Ana ajan struct'ı - Tüm bileşenleri koordine eder
type Pars struct {
	Config         *config.Config
	Brain          kernel.Brain
	SecondaryBrain kernel.Brain
	Skills         *skills.Manager
	Memory         kernel.Memory
	MaxSteps       int

	Sessions   map[string]*Session
	sessMu     sync.RWMutex

	EventChannel chan string
	AlertHook    func(msg string)
}

// NewPars: Yeni Pars örneği oluşturur ve tüm alt sistemleri başlatır
func NewPars(cfg *config.Config, primary, secondary kernel.Brain, skillMgr *skills.Manager, mem kernel.Memory) *Pars {
	// 🚨 DÜZELTME #1: Config nil kontrolü
	if cfg == nil {
		logger.Error("❌ [NewPars] Config nil! Ajan başlatılamadı.")
		return nil
	}

	// 🚨 DÜZELTME #2: Primary Brain nil kontrolü (Kritik!)
	if primary == nil {
		logger.Error("❌ [NewPars] Primary Brain nil! Ajan başlatılamadı.")
		return nil
	}

	// 🚨 DÜZELTME #3: Skills Manager nil kontrolü
	if skillMgr == nil {
		logger.Error("❌ [NewPars] Skills Manager nil! Ajan başlatılamadı.")
		return nil
	}

	steps := cfg.App.MaxSteps
	if steps <= 0 {
		steps = 25 // Varsayılan değer
	}

	// 🚨 DÜZELTME #4: Channel buffer'ı 200'e çıkar (yüksek yük için)
	eventChan := make(chan string, 200)

	r := &Pars{
		Config:         cfg,
		Brain:          primary,
		SecondaryBrain: secondary, // 🆕 Secondary nil olabilir, normal
		Skills:         skillMgr,
		Memory:         mem, // 🆕 Memory nil olabilir, normal
		MaxSteps:       steps,
		Sessions:       make(map[string]*Session),
		EventChannel:   eventChan,
	}

	// 🚀 Öz-Yönetim: Pars'in kendi süreçlerini denetleme yeteneği
	// 🚨 DÜZELTME #5: Tool registration güvenli
	r.RegisterTool(&ParsControlTool{pars: r})

	// 🛠️ Delege Etme: Eğer ikinci beyin aktifse delegasyon aracını bağla
	// 🚨 DÜZELTME #6: SecondaryBrain ve Config kontrolü
	if cfg.Brain.Secondary.Enabled && secondary != nil {
		r.RegisterTool(&DelegateTaskTool{pars: r})
		logger.Success("🐯 [NewPars] İkincil Beyin (Worker) aktif edildi: %s", cfg.Brain.Secondary.ModelName)
	} else {
		logger.Debug("📝 [NewPars] İkincil Beyin devre dışı veya yapılandırılmamış.")
	}

	// 🚨 DÜZELTME #7: Memory başlatma logu
	if mem != nil {
		logger.Success("🧠 [NewPars] Uzun süreli hafıza aktif.")
	} else {
		logger.Warn("⚠️ [NewPars] Hafıza merkezi aktif değil (Memory nil).")
	}

	// 🩺 SİSTEM DİNLEYİCİSİNİ BAŞLAT
	// (session.go içinde yazdığımız arka plan dinleyicisini tetikler)
	// 🚨 DÜZELTME #8: EventProcessor başlatma logu
	r.startEventProcessor()
	logger.Success("🩺 [NewPars] Sistem olay dinleyicisi başlatıldı.")

	logger.Success("✅ [NewPars] Pars Agent V5 başarıyla başlatıldı! (MaxSteps: %d)", steps)

	return r
}

// RegisterTool: Ajan'a yeni bir yetenek (tool) kaydeder
func (a *Pars) RegisterTool(t kernel.Tool) {
	// 🚨 DÜZELTME #9: Tool ve Skills nil kontrolü
	if a == nil || a.Skills == nil {
		logger.Error("❌ [RegisterTool] Skills manager nil!")
		return
	}

	if t == nil {
		logger.Warn("⚠️ [RegisterTool] Nil tool kayıt edilemez.")
		return
	}

	isUpdate := a.Skills.Register(t)
	if isUpdate {
		logger.Debug("🔄 [RegisterTool] Tool güncellendi: %s", t.Name())
	} else {
		logger.Debug("✅ [RegisterTool] Tool kaydedildi: %s", t.Name())
	}
}

// GetSession: 🆕 Yardımcı fonksiyon - Session'ı güvenli şekilde al
func (a *Pars) GetSession(sessionID string) (*Session, bool) {
	if a == nil {
		return nil, false
	}

	a.sessMu.RLock()
	defer a.sessMu.RUnlock()

	sess, exists := a.Sessions[sessionID]
	return sess, exists
}

// GetActiveSessionCount: 🆕 Yardımcı fonksiyon - Aktif oturum sayısını döndür
func (a *Pars) GetActiveSessionCount() int {
	if a == nil {
		return 0
	}

	a.sessMu.RLock()
	defer a.sessMu.RUnlock()

	return len(a.Sessions)
}

// Shutdown: 🆕 Yardımcı fonksiyon - Graceful shutdown için
func (a *Pars) Shutdown() {
	if a == nil {
		return
	}

	logger.Warn("🛑 [Shutdown] Pars Agent kapatılıyor...")

	// Tüm aktif session'ları iptal et
	a.sessMu.Lock()
	for id, sess := range a.Sessions {
		if sess.Cancel != nil {
			sess.Cancel()
		}
		delete(a.Sessions, id)
	}
	a.sessMu.Unlock()

	// EventChannel'ı kapat
	if a.EventChannel != nil {
		close(a.EventChannel)
	}

	logger.Info("🏁 [Shutdown] Pars Agent güvenli şekilde kapatıldı.")
}
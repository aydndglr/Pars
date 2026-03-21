package agent

import (
	"sync"

	"github.com/aydndglr/pars-agent-v3/internal/core/config"
	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/aydndglr/pars-agent-v3/internal/skills"
)

type Pars struct {
	Config         *config.Config
	Brain          kernel.Brain
	SecondaryBrain kernel.Brain
	Skills         *skills.Manager
	Memory         kernel.Memory
	MaxSteps       int
	sessions map[string]*Session
	sessMu   sync.RWMutex

	EventChannel chan string
	AlertHook    func(msg string)
}


func NewPars(cfg *config.Config, primary, secondary kernel.Brain, skillMgr *skills.Manager, mem kernel.Memory) *Pars {
	if cfg == nil {
		logger.Error("❌ [NewPars] Config nil! Ajan başlatılamadı.")
		return nil
	}

	if primary == nil {
		logger.Error("❌ [NewPars] Primary Brain nil! Ajan başlatılamadı.")
		return nil
	}

	if skillMgr == nil {
		logger.Error("❌ [NewPars] Skills Manager nil! Ajan başlatılamadı.")
		return nil
	}

	steps := cfg.App.MaxSteps
	if steps <= 0 {
		steps = 25 
	}

	eventChan := make(chan string, 200)

	r := &Pars{
		Config:         cfg,
		Brain:          primary,
		SecondaryBrain: secondary, 
		Skills:         skillMgr,
		Memory:         mem, 
		MaxSteps:       steps,
		sessions:       make(map[string]*Session),
		EventChannel:   eventChan,
	}

	r.RegisterTool(&ParsControlTool{pars: r})
	if cfg.Brain.Secondary.Enabled && secondary != nil {
		r.RegisterTool(&DelegateTaskTool{pars: r})
		logger.Success("🐯 [NewPars] İkincil Beyin (Worker) aktif edildi: %s", cfg.Brain.Secondary.ModelName)
	} else {
		logger.Debug("📝 [NewPars] İkincil Beyin devre dışı veya yapılandırılmamış.")
	}

	if mem != nil {
		logger.Success("🧠 [NewPars] Uzun süreli hafıza aktif.")
	} else {
		logger.Warn("⚠️ [NewPars] Hafıza merkezi aktif değil (Memory nil).")
	}

	r.startEventProcessor()
	logger.Success("🩺 [NewPars] Sistem olay dinleyicisi başlatıldı.")

	logger.Success("✅ [NewPars] Pars Agent V5 başarıyla başlatıldı! (MaxSteps: %d)", steps)

	return r
}

func (a *Pars) RegisterTool(t kernel.Tool) {
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

func (a *Pars) GetSession(sessionID string) (*Session, bool) {
	if a == nil {
		return nil, false
	}

	a.sessMu.RLock()
	defer a.sessMu.RUnlock()

	sess, exists := a.sessions[sessionID] 
	return sess, exists
}

func (a *Pars) GetActiveSessionCount() int {
	if a == nil {
		return 0
	}

	a.sessMu.RLock()
	defer a.sessMu.RUnlock()

	return len(a.sessions) 
}

func (a *Pars) Shutdown() {
	if a == nil {
		return
	}

	logger.Warn("🛑 [Shutdown] Pars Agent kapatılıyor...")

	a.sessMu.Lock()
	for id, sess := range a.sessions { 
		if sess.Cancel != nil {
			sess.Cancel()
		}
		delete(a.sessions, id)
	}
	a.sessMu.Unlock()

	if a.EventChannel != nil {
		close(a.EventChannel)
	}

	logger.Info("🏁 [Shutdown] Pars Agent güvenli şekilde kapatıldı.")
}
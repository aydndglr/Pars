package security

import (
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

// =====================================================================
// 👑 SECURITY ENGINE (Ana Orkestra Şefi)
// =====================================================================
type SecurityEngine struct {
	EventChan chan<- string
	auth      *AuthMonitor
	file      *FileGuard
	proc      *ProcessHunter
	netPriv   *NetworkPrivacy
}

// NewSecurityEngine, Pars'ın siber güvenlik kalkanlarını oluşturur.
func NewSecurityEngine(eventChan chan<- string) *SecurityEngine {
	return &SecurityEngine{
		EventChan: eventChan,
		auth:      NewAuthMonitor(eventChan),
		file:      NewFileGuard(eventChan),
		proc:      NewProcessHunter(eventChan),
		netPriv:   NewNetworkPrivacy(eventChan),
	}
}

// StartAll, tüm güvenlik sensörlerini asenkron (goroutine) olarak başlatır.
func (s *SecurityEngine) StartAll() {
	logger.Action("🛡️ Pars EDR (Uç Nokta Tehdit Algılama) Sistemi Başlatılıyor...")
	
	s.auth.Start()
	s.file.Start()
	s.proc.Start()
	s.netPriv.Start()
	
	logger.Success("🏰 Pars EDR: Sistem tamamen ZIRHLANDI! İzinsiz giriş, Fidye Yazılımı ve Tehdit Avı aktif.")
}

// StopAll, Graceful Shutdown prensibiyle tüm kalkanları güvenli bir şekilde kapatır.
func (s *SecurityEngine) StopAll() {
	logger.Warn("🛡️ Pars EDR Devre Dışı Bırakılıyor...")
	
	s.auth.Stop()
	s.file.Stop()
	s.proc.Stop()
	s.netPriv.Stop()
	
	logger.Success("🏰 Pars EDR: Tüm güvenlik kalkanları başarıyla indirildi.")
}
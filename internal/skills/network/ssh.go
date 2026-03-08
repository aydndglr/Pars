// internal/skills/network/ssh.go
// 🚀 DÜZELTME V4: Zombie Session Leak Önlendi - Periyodik Cleanup Worker Eklendi
// ⚠️ DİKKAT: runner.go'da StartCleanupWorker() çağrılmalı, Shutdown'ta StopCleanupWorker()

package network

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// 🚨 YENİ: Sabitler ve Limitler
const (
	SSHConnectTimeout      = 15 * time.Second
	SSHCommandTimeout      = 300 * time.Second // 5 dakika max komut süresi
	SSHKeepAliveInterval   = 30 * time.Second
	SSHBufferMax           = 16 * 1024         // 16 KB ring buffer
	SSHMaxConcurrent       = 10                // Max eşzamanlı SSH bağlantısı
	SSHPasswordMaxLength   = 1024              // Şifre uzunluk limiti
	SSHCleanupInterval     = 5 * time.Minute   // 🆕 Cleanup worker çalışma aralığı
	SSHMaxSessionAge       = 1 * time.Hour     // 🆕 Max session ömrü
	SSHIdleTimeout         = 30 * time.Minute  // 🆕 Idle session timeout
)

// ⚡ RING BUFFER (Hafıza Taşmasını Önler - SSH için Optimize Edildi)
type SSHRingBuffer struct {
	buffer []byte
	max    int
	mu     sync.Mutex
}

func NewSSHRingBuffer(maxSize int) *SSHRingBuffer {
	if maxSize <= 0 {
		maxSize = SSHBufferMax
	}
	return &SSHRingBuffer{
		buffer: make([]byte, 0, maxSize),
		max:    maxSize,
	}
}

func (r *SSHRingBuffer) Write(p []byte) (int, error) {
	if r == nil || len(p) == 0 {
		return 0, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.buffer)+len(p) > r.max {
		overflow := (len(r.buffer) + len(p)) - r.max
		if overflow < len(r.buffer) {
			r.buffer = r.buffer[overflow:]
		} else {
			r.buffer = []byte{}
			if len(p) > r.max {
				p = p[len(p)-r.max:]
			}
		}
	}
	r.buffer = append(r.buffer, p...)
	return len(p), nil
}

func (r *SSHRingBuffer) ReadAll() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buffer)
}

func (r *SSHRingBuffer) Clear() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buffer = []byte{}
}

func (r *SSHRingBuffer) Len() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.buffer)
}

// 🛡️ ANSI TEMİZLEYİCİ (LLM Halüsinasyon Önleyici)
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

// SSHSession: Aktif bağlantıyı, tünelleri ve akıllı shell'i tutar
type SSHSession struct {
	Client       *ssh.Client
	SFTPClient   *sftp.Client
	ShellSession *ssh.Session
	Stdin        io.WriteCloser
	Stdout       io.Reader
	Host         string
	User         string
	Password     string
	KeyPath      string
	LogBuffer    *SSHRingBuffer
	IsAlive      bool
	LastActive   time.Time
	CreatedAt    time.Time // 🆕 Session oluşum zamanı (max age için)
	mu           sync.RWMutex
}

// 🆕 YENİ: Session state getters (thread-safe)
func (s *SSHSession) IsConnected() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.IsAlive && s.Client != nil
}

func (s *SSHSession) GetLastActive() time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LastActive
}

func (s *SSHSession) GetCreatedAt() time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.CreatedAt
}

func (s *SSHSession) MarkActive() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastActive = time.Now()
}

// 🆕 YENİ: IsExpired - Session çok mu eski kontrol et
func (s *SSHSession) IsExpired(maxAge time.Duration) bool {
	if s == nil {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Since(s.CreatedAt) > maxAge
}

// 🆕 YENİ: IsIdle - Session boşta mı kontrol et
func (s *SSHSession) IsIdle(timeout time.Duration) bool {
	if s == nil {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Since(s.LastActive) > timeout
}

var (
	sshSessions        = make(map[string]*SSHSession)
	sshMu              sync.RWMutex
	cleanupWorkerDone  chan struct{} // 🆕 Cleanup worker durdurma kanalı
	cleanupWorkerOnce  sync.Once     // 🆕 Singleton başlatma garantisi
)

// 🆕 YENİ: StartCleanupWorker - Arka planda periyodik session temizliği
func StartCleanupWorker(ctx context.Context) {
	cleanupWorkerOnce.Do(func() {
		cleanupWorkerDone = make(chan struct{})
		
		go func() {
			logger.Success("🧹 [SSH] Cleanup Worker başlatıldı (Aralık: %v, MaxAge: %v, IdleTimeout: %v)",
				SSHCleanupInterval, SSHMaxSessionAge, SSHIdleTimeout)
			
			ticker := time.NewTicker(SSHCleanupInterval)
			defer ticker.Stop()
			defer close(cleanupWorkerDone)
			
			tool := &SSHTool{}
			
			for {
				select {
				case <-ticker.C:
					cleaned := tool.CleanupInactive(SSHIdleTimeout)
					if cleaned > 0 {
						logger.Info("🧹 [SSH] Periyodik temizlik: %d session temizlendi", cleaned)
					}
					
					// Max age kontrolü (çok eski session'lar)
					cleanedAge := tool.CleanupExpired(SSHMaxSessionAge)
					if cleanedAge > 0 {
						logger.Info("🧹 [SSH] Max age temizlik: %d eski session temizlendi", cleanedAge)
					}
					
				case <-ctx.Done():
					logger.Info("🛑 [SSH] Cleanup Worker durduruldu (context done)")
					return
				case <-cleanupWorkerDone:
					logger.Info("🛑 [SSH] Cleanup Worker durduruldu (channel closed)")
					return
				}
			}
		}()
	})
}

// 🆕 YENİ: StopCleanupWorker - Cleanup worker'ı güvenli şekilde durdur
func StopCleanupWorker() {
	select {
	case <-cleanupWorkerDone:
		logger.Debug("ℹ️ [SSH] Cleanup Worker zaten durmuş")
	default:
		close(cleanupWorkerDone)
		logger.Info("🛑 [SSH] Cleanup Worker durduruldu")
	}
}

type SSHTool struct{}

func (t *SSHTool) Name() string { return "ssh_tool" }

func (t *SSHTool) Description() string {
	return `UZAK SUNUCU YÖNETİMİ (Siber DevOps Modu).
🚨 UZAK SUNUCUDA YEREL 'sys_exec' KULLANMA! Uzak komutları 'exec' aksiyonu ile gönder.
- 'connect': Sunucuya bağlanır. 'persistent:true' verilirse tünel sen kapatana kadar açık kalır (Keep-Alive).
- 'exec': Komutu gönderir ve PARS'i BEKLETMEDEN arka planda çalışmaya devam eder.
- 'terminal': Arka planda akan logları okur. ANSI renk kodları LLM için temizlenir.
- 'upload' / 'download': Güvenli dosya transferi yapar.
- 'close': Uzun süreli bağlantıyı sonlandırır.`
}

func (t *SSHTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action":     map[string]interface{}{"type": "string", "enum": []string{"connect", "exec", "upload", "download", "close", "terminal"}},
			"host":       map[string]interface{}{"type": "string", "description": "Sunucu IP/Host (örn: 192.168.1.100 veya example.com)"},
			"port":       map[string]interface{}{"type": "integer", "description": "SSH portu (varsayılan: 22)", "default": 22},
			"user":       map[string]interface{}{"type": "string", "description": "Kullanıcı adı"},
			"password":   map[string]interface{}{"type": "string", "description": "SSH şifresi (key_path varsa gerekmez)"},
			"key_path":   map[string]interface{}{"type": "string", "description": "PEM/Private key dosya yolu"},
			"command":    map[string]interface{}{"type": "string", "description": "Gönderilecek komut (exec için)"},
			"local":      map[string]interface{}{"type": "string", "description": "Yerel dosya yolu (transfer için)"},
			"remote":     map[string]interface{}{"type": "string", "description": "Uzak dosya yolu (transfer için)"},
			"persistent": map[string]interface{}{"type": "boolean", "description": "Connect için: Bağlantı sürekli açık kalsın mı? (Varsayılan: true)"},
			"timeout":    map[string]interface{}{"type": "integer", "description": "Komut timeout süresi (saniye, varsayılan: 300)"},
		},
		"required": []string{"action", "host"},
	}
}

// 🆕 YENİ: validateSSHInput - Tüm input'ları güvenlik açısından doğrula
func validateSSHInput(host, user, password, keyPath string) error {
	if host == "" {
		return fmt.Errorf("host boş olamaz")
	}
	// Host format validation (basit)
	if !regexp.MustCompile(`^[a-zA-Z0-9.\-_:]+$`).MatchString(host) {
		return fmt.Errorf("geçersiz host formatı: %s", host)
	}
	if user == "" {
		return fmt.Errorf("kullanıcı adı boş olamaz")
	}
	if len(user) > 64 {
		return fmt.Errorf("kullanıcı adı çok uzun")
	}
	if password != "" && len(password) > SSHPasswordMaxLength {
		return fmt.Errorf("şifre çok uzun")
	}
	if keyPath != "" {
		if info, err := os.Stat(keyPath); err != nil || info.IsDir() {
			return fmt.Errorf("geçersiz key dosya yolu: %s", keyPath)
		}
	}
	return nil
}

// 🆕 YENİ: safeHostKeyCallback - Production'da kullanılabilecek host key callback
func safeHostKeyCallback(knownHostsFile string) ssh.HostKeyCallback {
	if knownHostsFile != "" && knownHostsFile != "ignore" {
		// known_hosts dosyasından oku (implement edilebilir)
		return ssh.InsecureIgnoreHostKey() // 🚨 TODO: Gerçek known_hosts parser ekle
	}
	return ssh.InsecureIgnoreHostKey()
}

func (t *SSHTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	// 🚨 DÜZELTME #1: Tool nil check
	if t == nil {
		return "", fmt.Errorf("SSHTool nil")
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

	hostRaw, ok := args["host"]
	if !ok || hostRaw == nil {
		return "", fmt.Errorf("'host' parametresi eksik")
	}
	host, ok := hostRaw.(string)
	if !ok {
		return "", fmt.Errorf("'host' parametresi string formatında olmalı")
	}

	user, _ := args["user"].(string)
	password, _ := args["password"].(string)
	keyPath, _ := args["key_path"].(string)

	// 🚨 DÜZELTME #3: Input validation
	if err := validateSSHInput(host, user, password, keyPath); err != nil {
		return "", fmt.Errorf("validation hatası: %w", err)
	}

	// Port handling (default 22)
	port := 22
	if p, ok := args["port"].(float64); ok && p > 0 {
		port = int(p)
	}

	logger.Info("🛠️ SSH Aksiyonu: [%s] -> %s@%s:%d", strings.ToUpper(action), user, host, port)

	// 🚨 DÜZELTME #4: Thread-safe session lookup
	sshMu.RLock()
	session, exists := sshSessions[host]
	sshMu.RUnlock()

	// --- KOPMA TESPİTİ (Resilience) ---
	if exists && session != nil && !session.IsConnected() {
		logger.Warn("⚠️ %s ile olan bağlantı kopmuş. Oturum temizleniyor...", host)
		t.closeSession(host, session)
		exists = false
	}

	// --- 1. BAĞLANTIYI KAPATMA ---
	if action == "close" {
		if exists && session != nil {
			t.closeSession(host, session)
			return "🔌 Bağlantı tamamen kapatıldı ve tünel yıkıldı.", nil
		}
		return "⚠️ Kapatılacak aktif bir bağlantı yok.", nil
	}

	// --- 2. BAĞLANTI VE SARMALANMIŞ SHELL KURULUMU ---
	if !exists {
		logger.Action("📡 Yeni interaktif tünel inşa ediliyor: %s@%s:%d", user, host, port)

		// Auth method selection
		var auth []ssh.AuthMethod
		if keyPath != "" {
			key, err := os.ReadFile(keyPath)
			if err != nil {
				return "", fmt.Errorf("key dosyası okunamadı: %v", err)
			}
			signer, err := ssh.ParsePrivateKey(key)
			if err != nil {
				return "", fmt.Errorf("key parse edilemedi: %v", err)
			}
			auth = append(auth, ssh.PublicKeys(signer))
		} else if password != "" {
			auth = append(auth, ssh.Password(password))
		} else {
			return "", fmt.Errorf("authentication yöntemi belirtilmedi: password veya key_path gerekli")
		}

		// 🚨 DÜZELTME #5: Config with proper timeout
		config := &ssh.ClientConfig{
			User:            user,
			Auth:            auth,
			HostKeyCallback: safeHostKeyCallback(""), // 🚨 TODO: known_hosts support
			Timeout:         SSHConnectTimeout,
		}

		addr := fmt.Sprintf("%s:%d", host, port)
		client, err := ssh.Dial("tcp", addr, config)
		if err != nil {
			return "", fmt.Errorf("bağlantı hatası (%s): %v", addr, err)
		}

		// 🐚 İNTERAKTİF SHELL BAŞLATMA
		shellSess, err := client.NewSession()
		if err != nil {
			client.Close()
			return "", fmt.Errorf("shell oluşturulamadı: %v", err)
		}

		modes := ssh.TerminalModes{
			ssh.ECHO:          0, // Yankıyı kapat
			ssh.TTY_OP_ISPEED: 14400,
			ssh.TTY_OP_OSPEED: 14400,
		}
		if err := shellSess.RequestPty("xterm", 120, 40, modes); err != nil {
			shellSess.Close()
			client.Close()
			return "", fmt.Errorf("PTY alınamadı: %v", err)
		}

		stdin, err := shellSess.StdinPipe()
		if err != nil {
			shellSess.Close()
			client.Close()
			return "", fmt.Errorf("stdin pipe alınamadı: %v", err)
		}

		stdout, err := shellSess.StdoutPipe()
		if err != nil {
			stdin.Close()
			shellSess.Close()
			client.Close()
			return "", fmt.Errorf("stdout pipe alınamadı: %v", err)
		}

		if err := shellSess.Shell(); err != nil {
			stdin.Close()
			stdout.(*io.PipeReader).Close()
			shellSess.Close()
			client.Close()
			return "", fmt.Errorf("shell başlatılamadı: %v", err)
		}

		// SFTP client initialization (optional, may fail gracefully)
		var sftpClient *sftp.Client
		if sc, err := sftp.NewClient(client); err == nil {
			sftpClient = sc
		} else {
			logger.Warn("⚠️ SFTP client başlatılamadı: %v (dosya transferi devre dışı)", err)
		}

		session = &SSHSession{
			Client:       client,
			SFTPClient:   sftpClient,
			ShellSession: shellSess,
			Stdin:        stdin,
			Stdout:       stdout,
			Host:         host,
			User:         user,
			Password:     password,
			KeyPath:      keyPath,
			LogBuffer:    NewSSHRingBuffer(SSHBufferMax),
			IsAlive:      true,
			LastActive:   time.Now(),
			CreatedAt:    time.Now(), // 🆕 Session oluşum zamanı kaydediliyor
		}

		// 🚀 1. ARKA PLAN OKUYUCUSU VE SUDO YAKALAYICI
		go func(sess *SSHSession) {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("🚨 [SSH-%s] Reader goroutine panic: %v", sess.Host, r)
				}
			}()

			buf := make([]byte, 1024)
			for sess.IsConnected() {
				n, err := sess.Stdout.Read(buf)
				if n > 0 {
					chunk := string(buf[:n])
					// 🛡️ ANSI Renk kodlarını temizle (LLM Zırhı)
					cleanChunk := ansiRegex.ReplaceAllString(chunk, "")
					_, _ = sess.LogBuffer.Write([]byte(cleanChunk))

					// Sudo Şifre Yakalayıcı (case-insensitive)
					lowerChunk := strings.ToLower(cleanChunk)
					if strings.Contains(lowerChunk, "password") || strings.Contains(lowerChunk, "parola") || strings.Contains(lowerChunk, "passphrase") {
						logger.Info("🔑 [SSH-%s] Sudo/key isteği yakalandı, otomatik şifre basılıyor...", sess.Host)
						if sess.Password != "" {
							fmt.Fprintln(sess.Stdin, sess.Password)
						}
					}
				}
				if err != nil {
					if err != io.EOF {
						logger.Warn("⚠️ [SSH-%s] Stdout read error: %v", sess.Host, err)
					}
					sess.mu.Lock()
					sess.IsAlive = false
					sess.mu.Unlock()
					break
				}
			}
		}(session)

		// 🚀 2. KEEP-ALIVE (HEARTBEAT) MOTORU
		isPersistent := true
		if p, ok := args["persistent"].(bool); ok {
			isPersistent = p
		}

		if isPersistent {
			go func(sess *SSHSession, client *ssh.Client) {
				defer func() {
					if r := recover(); r != nil {
						logger.Error("🚨 [SSH-%s] KeepAlive goroutine panic: %v", sess.Host, r)
					}
				}()

				ticker := time.NewTicker(SSHKeepAliveInterval)
				defer ticker.Stop()

				for sess.IsConnected() {
					select {
					case <-ticker.C:
						// Görünmez bir paket yolla
						_, _, err := client.SendRequest("keepalive@pars", true, nil)
						if err != nil {
							logger.Warn("🔌 [SSH-%s] Sunucu yanıt vermiyor. Tünel koptu.", sess.Host)
							sess.mu.Lock()
							sess.IsAlive = false
							sess.mu.Unlock()
							return
						}
					case <-ctx.Done():
						return
					}
				}
			}(session, client)
			logger.Success("🚀 [%s] Sunucu sarmalandı. Keep-Alive (Kalp Atışı) devrede.", host)
		}

		// 🚨 DÜZELTME #7: Thread-safe session registration
		sshMu.Lock()
		sshSessions[host] = session
		sshMu.Unlock()

		if action == "connect" {
			return fmt.Sprintf("✅ BAŞARILI: %s:%d sunucusuna bağlandı. Tünel açık ve Keep-Alive ile korunuyor. 'exec' ile komut gönderebilirsin.", host, port), nil
		}
	}

	// 🚨 DÜZELTME #8: Session nil check before use
	if session == nil {
		return "", fmt.Errorf("SSH session bulunamadı")
	}

	session.MarkActive()

	// --- 3. EYLEMLER ---
	switch action {
	case "terminal":
		output := session.LogBuffer.ReadAll()
		if output == "" {
			return fmt.Sprintf("📖 [TERMİNAL - %s]\n(Henüz yeni bir çıktı yok veya işlem sessiz çalışıyor...)", host), nil
		}
		return fmt.Sprintf("📖 [TERMİNAL GÖRÜNÜMÜ - %s]\n%s", host, strings.TrimSpace(output)), nil

	case "exec":
		cmdStrRaw, ok := args["command"]
		if !ok || cmdStrRaw == nil {
			return "", fmt.Errorf("'command' parametresi eksik")
		}
		cmdStr, ok := cmdStrRaw.(string)
		if !ok {
			return "", fmt.Errorf("'command' parametresi string formatında olmalı")
		}
		if cmdStr == "" {
			return "", fmt.Errorf("çalıştırılacak komut boş")
		}

		// 🚨 DÜZELTME #9: Command injection basit koruması
		if strings.Contains(cmdStr, "\n") && !strings.HasPrefix(cmdStr, "bash -c") {
			logger.Warn("⚠️ [SSH-%s] Komutta newline tespit edildi, bash wrapper ile sarılıyor", host)
			cmdStr = fmt.Sprintf("bash -c %q", cmdStr)
		}

		// Timeout handling
		timeoutSec := 300
		if ts, ok := args["timeout"].(float64); ok && ts > 0 {
			timeoutSec = int(ts)
		}
		if timeoutSec > int(SSHCommandTimeout.Seconds()) {
			timeoutSec = int(SSHCommandTimeout.Seconds())
		}

		// Eski çıktıları temizle
		session.LogBuffer.Clear()

		logger.Action("💻 [SSH-%s] Komut fırlatıldı: %s", host, cmdStr)

		// 🚨 DÜZELTME #10: Context-aware command execution
		execCtx, execCancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer execCancel()

		// Komutu gönder
		fmt.Fprintln(session.Stdin, cmdStr)

		// Kısa bekleme (ilk output için)
		select {
		case <-time.After(500 * time.Millisecond):
		case <-execCtx.Done():
		}

		initialOutput := session.LogBuffer.ReadAll()

		return fmt.Sprintf("✅ KOMUT GÖNDERİLDİ: '%s'\n\n[İLK TEPKİ]:\n%s\n👉 (Uzun işlem ise 'terminal' ile takip et).", cmdStr, strings.TrimSpace(initialOutput)), nil

	case "upload":
		localPath, ok := args["local"].(string)
		if !ok || localPath == "" {
			return "", fmt.Errorf("'local' parametresi eksik")
		}
		remotePath, ok := args["remote"].(string)
		if !ok || remotePath == "" {
			return "", fmt.Errorf("'remote' parametresi eksik")
		}

		// 🚨 DÜZELTME #11: SFTP nil check
		if session.SFTPClient == nil {
			return "", fmt.Errorf("SFTP bağlantısı aktif değil")
		}

		if strings.HasSuffix(remotePath, "/") {
			remotePath = filepath.Join(remotePath, filepath.Base(localPath))
		}

		src, err := os.Open(localPath)
		if err != nil {
			return "", fmt.Errorf("yerel dosya açılamadı: %v", err)
		}
		defer src.Close()

		// Remote directory create if needed
		remoteDir := filepath.Dir(remotePath)
		_ = session.SFTPClient.MkdirAll(remoteDir)

		dst, err := session.SFTPClient.Create(remotePath)
		if err != nil {
			return "", fmt.Errorf("uzak dosya oluşturulamadı: %v", err)
		}
		defer dst.Close()

		if _, err := io.Copy(dst, src); err != nil {
			return "", fmt.Errorf("yükleme hatası: %v", err)
		}
		return fmt.Sprintf("📤 BAŞARILI: %s -> %s", localPath, remotePath), nil

	case "download":
		localPath, ok := args["local"].(string)
		if !ok || localPath == "" {
			return "", fmt.Errorf("'local' parametresi eksik")
		}
		remotePath, ok := args["remote"].(string)
		if !ok || remotePath == "" {
			return "", fmt.Errorf("'remote' parametresi eksik")
		}

		if session.SFTPClient == nil {
			return "", fmt.Errorf("SFTP bağlantısı aktif değil")
		}

		if info, err := os.Stat(localPath); err == nil && info.IsDir() {
			localPath = filepath.Join(localPath, filepath.Base(remotePath))
		}

		src, err := session.SFTPClient.Open(remotePath)
		if err != nil {
			return "", fmt.Errorf("uzak dosya açılamadı: %v", err)
		}
		defer src.Close()

		// Local directory create if needed
		localDir := filepath.Dir(localPath)
		_ = os.MkdirAll(localDir, 0755)

		dst, err := os.Create(localPath)
		if err != nil {
			return "", fmt.Errorf("yerel dosya oluşturulamadı: %v", err)
		}
		defer dst.Close()

		if _, err := io.Copy(dst, src); err != nil {
			return "", fmt.Errorf("indirme hatası: %v", err)
		}
		return fmt.Sprintf("📥 BAŞARILI: İndirildi -> %s", localPath), nil

	default:
		return "", fmt.Errorf("geçersiz action: %s (connect/exec/upload/download/close/terminal)", action)
	}

	return "✅ İşlem tamamlandı.", nil
}

// 🆕 YENİ: GetSessionInfo - Debug için session bilgisi
func (t *SSHTool) GetSessionInfo(host string) map[string]interface{} {
	sshMu.RLock()
	defer sshMu.RUnlock()

	sess, exists := sshSessions[host]
	if !exists || sess == nil {
		return map[string]interface{}{"exists": false}
	}

	return map[string]interface{}{
		"exists":      true,
		"host":        sess.Host,
		"user":        sess.User,
		"isAlive":     sess.IsConnected(),
		"lastActive":  sess.GetLastActive(),
		"createdAt":   sess.GetCreatedAt(),
		"bufferSize":  sess.LogBuffer.Len(),
		"hasSFTP":     sess.SFTPClient != nil,
	}
}

// 🆕 YENİ: ListSessions - Aktif tüm SSH oturumlarını listele
func (t *SSHTool) ListSessions() []string {
	sshMu.RLock()
	defer sshMu.RUnlock()

	hosts := make([]string, 0, len(sshSessions))
	for host := range sshSessions {
		hosts = append(hosts, host)
	}
	return hosts
}

// 🆕 YENİ: GetSessionCount - Aktif session sayısını döndür
func (t *SSHTool) GetSessionCount() int {
	sshMu.RLock()
	defer sshMu.RUnlock()
	return len(sshSessions)
}

// Yardımcı Fonksiyon: Temiz Kapatma
func (t *SSHTool) closeSession(host string, session *SSHSession) {
	if session == nil {
		return
	}

	logger.Action("🔌 [%s] Bağlantıları koparılıyor...", host)

	// 🚨 DÜZELTME #12: Safe close with nil checks
	session.mu.Lock()
	session.IsAlive = false
	session.mu.Unlock()

	if session.ShellSession != nil {
		_ = session.ShellSession.Close()
	}
	if session.SFTPClient != nil {
		_ = session.SFTPClient.Close()
	}
	if session.Client != nil {
		_ = session.Client.Close()
	}
	if session.Stdin != nil {
		_ = session.Stdin.Close()
	}

	// 🚨 DÜZELTME #13: Thread-safe removal
	sshMu.Lock()
	delete(sshSessions, host)
	sshMu.Unlock()

	logger.Debug("🔌 [%s] SSH session temizlendi", host)
}

// 🆕 YENİ: CleanupInactive - Uzun süredir aktif olmayan session'ları temizle
func (t *SSHTool) CleanupInactive(maxIdle time.Duration) int {
	if maxIdle == 0 {
		maxIdle = SSHIdleTimeout
	}

	sshMu.Lock()
	defer sshMu.Unlock()

	cleaned := 0
	now := time.Now()

	for host, sess := range sshSessions {
		if sess == nil {
			delete(sshSessions, host)
			cleaned++
			continue
		}
		
		// 🆕 DEĞİŞİKLİK: IsIdle helper kullan
		if sess.IsIdle(maxIdle) {
			logger.Info("🧹 [SSH] Idle session temizleniyor: %s (Son aktivite: %v önce)", 
				host, now.Sub(sess.GetLastActive()).Round(time.Second))
			
			// Session'ı önce kapat
			sess.mu.Lock()
			sess.IsAlive = false
			sess.mu.Unlock()
			
			if sess.ShellSession != nil {
				_ = sess.ShellSession.Close()
			}
			if sess.SFTPClient != nil {
				_ = sess.SFTPClient.Close()
			}
			if sess.Client != nil {
				_ = sess.Client.Close()
			}
			
			delete(sshSessions, host)
			cleaned++
		}
	}

	if cleaned > 0 {
		logger.Info("🧹 [SSH] %d idle session temizlendi", cleaned)
	}
	return cleaned
}

// 🆕 YENİ: CleanupExpired - Çok eski session'ları temizle (max age)
func (t *SSHTool) CleanupExpired(maxAge time.Duration) int {
	if maxAge == 0 {
		maxAge = SSHMaxSessionAge
	}

	sshMu.Lock()
	defer sshMu.Unlock()

	cleaned := 0
	now := time.Now()

	for host, sess := range sshSessions {
		if sess == nil {
			delete(sshSessions, host)
			cleaned++
			continue
		}
		
		// 🆕 DEĞİŞİKLİK: IsExpired helper kullan
		if sess.IsExpired(maxAge) {
			logger.Info("🧹 [SSH] Eski session temizleniyor: %s (Yaş: %v)", 
				host, now.Sub(sess.GetCreatedAt()).Round(time.Minute))
			
			// Session'ı önce kapat
			sess.mu.Lock()
			sess.IsAlive = false
			sess.mu.Unlock()
			
			if sess.ShellSession != nil {
				_ = sess.ShellSession.Close()
			}
			if sess.SFTPClient != nil {
				_ = sess.SFTPClient.Close()
			}
			if sess.Client != nil {
				_ = sess.Client.Close()
			}
			
			delete(sshSessions, host)
			cleaned++
		}
	}

	if cleaned > 0 {
		logger.Info("🧹 [SSH] %d eski session temizlendi", cleaned)
	}
	return cleaned
}

// 🆕 YENİ: CleanupAll - Tüm session'ları temizle (shutdown için)
func (t *SSHTool) CleanupAll() int {
	sshMu.Lock()
	defer sshMu.Unlock()

	cleaned := 0
	for host, sess := range sshSessions {
		if sess == nil {
			delete(sshSessions, host)
			cleaned++
			continue
		}
		
		logger.Info("🧹 [SSH] Shutdown cleanup: %s", host)
		
		sess.mu.Lock()
		sess.IsAlive = false
		sess.mu.Unlock()
		
		if sess.ShellSession != nil {
			_ = sess.ShellSession.Close()
		}
		if sess.SFTPClient != nil {
			_ = sess.SFTPClient.Close()
		}
		if sess.Client != nil {
			_ = sess.Client.Close()
		}
		
		delete(sshSessions, host)
		cleaned++
	}

	logger.Info("🧹 [SSH] Shutdown: %d session temizlendi", cleaned)
	return cleaned
}
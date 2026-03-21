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
	"golang.org/x/crypto/ssh/knownhosts" 
)

const (
	SSHConnectTimeout    = 15 * time.Second
	SSHCommandTimeout    = 300 * time.Second
	SSHKeepAliveInterval = 30 * time.Second
	SSHBufferMax         = 16 * 1024
	SSHMaxConcurrent     = 10
	SSHPasswordMaxLength = 1024

	SSHCleanupInterval = 5 * time.Minute
	SSHMaxSessionAge   = 1 * time.Hour
	SSHIdleTimeout     = 30 * time.Minute
)

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

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

func cleanANSI(text string) string {
	return ansiRegex.ReplaceAllString(text, "")
}

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
	CreatedAt    time.Time
	mu           sync.RWMutex
}

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

func (s *SSHSession) IsExpired(maxAge time.Duration) bool {
	if s == nil {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Since(s.CreatedAt) > maxAge
}

func (s *SSHSession) IsIdle(timeout time.Duration) bool {
	if s == nil {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Since(s.LastActive) > timeout
}

var (
	sshSessions       = make(map[string]*SSHSession)
	sshMu             sync.RWMutex
	cleanupWorkerDone chan struct{}
	cleanupWorkerOnce sync.Once
)

func getStringArg(args map[string]interface{}, key string, def string) string {
	if val, ok := args[key].(string); ok {
		return val
	}
	return def
}

func getIntArg(args map[string]interface{}, key string, def int) int {
	if val, ok := args[key].(float64); ok {
		return int(val)
	}
	return def
}

func getBoolArg(args map[string]interface{}, key string, def bool) bool {
	if val, ok := args[key].(bool); ok {
		return val
	}
	return def
}

func closeSSHSession(sess *SSHSession) {
	if sess == nil {
		return
	}

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
	if sess.Stdin != nil {
		_ = sess.Stdin.Close()
	}
}

func StartCleanupWorker(ctx context.Context) {
	cleanupWorkerOnce.Do(func() {
		cleanupWorkerDone = make(chan struct{})

		go func() {
			logger.Success("🧹 [SSH] Cleanup Worker başlatıldı (Aralık: %v, MaxAge: %v, IdleTimeout: %v)",
				SSHCleanupInterval, SSHMaxSessionAge, SSHIdleTimeout)

			ticker := time.NewTicker(SSHCleanupInterval)
			defer ticker.Stop()

			tool := &SSHTool{}

			for {
				select {
				case <-ticker.C:
					cleaned := tool.CleanupInactive(SSHIdleTimeout)
					if cleaned > 0 {
						logger.Info("🧹 [SSH] Periyodik temizlik: %d idle session temizlendi", cleaned)
					}

					cleanedAge := tool.CleanupExpired(SSHMaxSessionAge)
					if cleanedAge > 0 {
						logger.Info("🧹 [SSH] Max age temizlik: %d eski session temizlendi", cleanedAge)
					}

				case <-ctx.Done():
					logger.Info("🛑 [SSH] Cleanup Worker durduruldu (context done)")
					close(cleanupWorkerDone)
					return

				case <-cleanupWorkerDone:
					logger.Info("🛑 [SSH] Cleanup Worker durduruldu (channel closed)")
					return
				}
			}
		}()
	})
}

func StopCleanupWorker() {
	cleanupWorkerOnce.Do(func() {
		if cleanupWorkerDone != nil {
			close(cleanupWorkerDone)
			logger.Info("🛑 [SSH] Cleanup Worker durduruldu")
		}
	})
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
			// 🆕 YENİ: insecure parametresi (god_mode vb. için)
			"insecure":   map[string]interface{}{"type": "boolean", "description": "MITM riskini göze alıp known_hosts kontrolünü atlar (varsayılan: false)"},
		},
		"required": []string{"action", "host"},
	}
}

func validateSSHInput(host, user, password, keyPath string) error {
	if host == "" {
		return fmt.Errorf("host boş olamaz")
	}
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

func safeHostKeyCallback(knownHostsFile string, allowInsecure bool) (ssh.HostKeyCallback, error) {
	if allowInsecure {
		logger.Warn("🚨 [SECURITY WARNING] SSH Host Key doğrulama devre dışı bırakıldı! MITM saldırılarına açıksınız.")
		return ssh.InsecureIgnoreHostKey(), nil
	}


	if knownHostsFile == "" {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			knownHostsFile = filepath.Join(homeDir, ".ssh", "known_hosts")
		}
	}

	if _, err := os.Stat(knownHostsFile); os.IsNotExist(err) {
		logger.Warn("⚠️ known_hosts dosyası bulunamadı (%s). Bağlantı güvenliği garanti edilemez.", knownHostsFile)
		return ssh.InsecureIgnoreHostKey(), nil
	}

	hostKeyCallback, err := knownhosts.New(knownHostsFile)
	if err != nil {
		return nil, fmt.Errorf("known_hosts dosyası okunamadı: %v", err)
	}

	logger.Debug("🔒 SSH Kimlik doğrulaması aktif: %s", knownHostsFile)
	return hostKeyCallback, nil
}

func (t *SSHTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if t == nil {
		return "", fmt.Errorf("SSHTool nil")
	}

	action := getStringArg(args, "action", "")
	if action == "" {
		return "", fmt.Errorf("'action' parametresi eksik")
	}

	host := getStringArg(args, "host", "")
	if host == "" {
		return "", fmt.Errorf("'host' parametresi eksik")
	}

	user := getStringArg(args, "user", "")
	password := getStringArg(args, "password", "")
	keyPath := getStringArg(args, "key_path", "")

	if err := validateSSHInput(host, user, password, keyPath); err != nil {
		return "", fmt.Errorf("validation hatası: %w", err)
	}

	port := getIntArg(args, "port", 22)
	if port <= 0 {
		port = 22
	}

	logger.Info("🛠️ SSH Aksiyonu: [%s] -> %s@%s:%d", strings.ToUpper(action), user, host, port)
	sshMu.RLock()
	session, exists := sshSessions[host]
	sshMu.RUnlock()
	if exists && session != nil && !session.IsConnected() {
		logger.Warn("⚠️ %s ile olan bağlantı kopmuş. Oturum temizleniyor...", host)
		t.closeAndRemoveSession(host, session)
		exists = false
	}

	if action == "close" {
		if exists && session != nil {
			t.closeAndRemoveSession(host, session)
			return "🔌 Bağlantı tamamen kapatıldı ve tünel yıkıldı.", nil
		}
		return "⚠️ Kapatılacak aktif bir bağlantı yok.", nil
	}

	if !exists {
		newSession, err := t.establishConnection(ctx, host, port, user, password, keyPath, args)
		if err != nil {
			return "", err
		}

		sshMu.Lock()
		sshSessions[host] = newSession
		sshMu.Unlock()

		if action == "connect" {
			return fmt.Sprintf("✅ BAŞARILI: %s:%d sunucusuna bağlandı. Tünel açık ve Keep-Alive ile korunuyor. 'exec' ile komut gönderebilirsin.", host, port), nil
		}
		session = newSession
	}

	if session == nil {
		return "", fmt.Errorf("SSH session bulunamadı")
	}

	session.MarkActive()

	switch action {
	case "terminal":
		return t.handleTerminal(session, host)
	case "exec":
		return t.handleExec(ctx, session, host, args)
	case "upload":
		return t.handleUpload(session, args)
	case "download":
		return t.handleDownload(session, args)
	default:
		return "", fmt.Errorf("geçersiz action: %s (connect/exec/upload/download/close/terminal)", action)
	}
}

func (t *SSHTool) establishConnection(ctx context.Context, host string, port int, user, password, keyPath string, args map[string]interface{}) (*SSHSession, error) {
	logger.Action("📡 Yeni interaktif tünel inşa ediliyor: %s@%s:%d", user, host, port)

	var auth []ssh.AuthMethod
	if keyPath != "" {
		key, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("key dosyası okunamadı: %v", err)
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("key parse edilemedi: %v", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	} else if password != "" {
		auth = append(auth, ssh.Password(password))
	} else {
		return nil, fmt.Errorf("authentication yöntemi belirtilmedi: password veya key_path gerekli")
	}

	insecureMode := getBoolArg(args, "insecure", false)
	hostKeyCallback, err := safeHostKeyCallback("", insecureMode)
	if err != nil {
		return nil, fmt.Errorf("host doğrulayıcı oluşturulamadı: %v", err)
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback,
		Timeout:         SSHConnectTimeout,
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("bağlantı hatası (%s): %v", addr, err)
	}

	shellSess, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("shell oluşturulamadı: %v", err)
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := shellSess.RequestPty("xterm", 120, 40, modes); err != nil {
		shellSess.Close()
		client.Close()
		return nil, fmt.Errorf("PTY alınamadı: %v", err)
	}

	stdin, err := shellSess.StdinPipe()
	if err != nil {
		shellSess.Close()
		client.Close()
		return nil, fmt.Errorf("stdin pipe alınamadı: %v", err)
	}

	stdout, err := shellSess.StdoutPipe()
	if err != nil {
		stdin.Close()
		shellSess.Close()
		client.Close()
		return nil, fmt.Errorf("stdout pipe alınamadı: %v", err)
	}

	if err := shellSess.Shell(); err != nil {
		stdin.Close()
		if pipe, ok := stdout.(*io.PipeReader); ok {
			_ = pipe.Close()
		}
		shellSess.Close()
		client.Close()
		return nil, fmt.Errorf("shell başlatılamadı: %v", err)
	}

	var sftpClient *sftp.Client
	if sc, err := sftp.NewClient(client); err == nil {
		sftpClient = sc
	} else {
		logger.Warn("⚠️ SFTP client başlatılamadı: %v (dosya transferi devre dışı)", err)
	}

	session := &SSHSession{
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
		CreatedAt:    time.Now(),
	}

	go t.startOutputReader(session)

	isPersistent := getBoolArg(args, "persistent", true)
	if isPersistent {
		go t.startKeepAlive(ctx, session, client)
		logger.Success("🚀 [%s] Sunucu sarmalandı. Keep-Alive (Kalp Atışı) devrede.", host)
	}

	return session, nil
}

func (t *SSHTool) startOutputReader(sess *SSHSession) {
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
			cleanChunk := cleanANSI(chunk)
			_, _ = sess.LogBuffer.Write([]byte(cleanChunk))

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
}

func (t *SSHTool) startKeepAlive(ctx context.Context, sess *SSHSession, client *ssh.Client) {
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
}

func (t *SSHTool) handleTerminal(session *SSHSession, host string) (string, error) {
	output := session.LogBuffer.ReadAll()
	if output == "" {
		return fmt.Sprintf("📖 [TERMİNAL - %s]\n(Henüz yeni bir çıktı yok veya işlem sessiz çalışıyor...)", host), nil
	}
	return fmt.Sprintf("📖 [TERMİNAL GÖRÜNÜMÜ - %s]\n%s", host, strings.TrimSpace(output)), nil
}

func (t *SSHTool) handleExec(ctx context.Context, session *SSHSession, host string, args map[string]interface{}) (string, error) {
	cmdStr := getStringArg(args, "command", "")
	if cmdStr == "" {
		return "", fmt.Errorf("'command' parametresi eksik")
	}

	if strings.Contains(cmdStr, "\n") && !strings.HasPrefix(cmdStr, "bash -c") {
		logger.Warn("⚠️ [SSH-%s] Komutta newline tespit edildi, bash wrapper ile sarılıyor", host)
		cmdStr = fmt.Sprintf("bash -c %q", cmdStr)
	}

	timeoutSec := getIntArg(args, "timeout", 300)
	if timeoutSec > int(SSHCommandTimeout.Seconds()) {
		timeoutSec = int(SSHCommandTimeout.Seconds())
	}

	session.LogBuffer.Clear()
	logger.Action("💻 [SSH-%s] Komut fırlatıldı: %s", host, cmdStr)

	execCtx, execCancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer execCancel()
	select {
	case <-execCtx.Done():
		return "", fmt.Errorf("komut gönderimi iptal edildi: %v", execCtx.Err())
	default:
		fmt.Fprintln(session.Stdin, cmdStr)
	}

	select {
	case <-time.After(500 * time.Millisecond):
	case <-execCtx.Done():
	}

	initialOutput := session.LogBuffer.ReadAll()

	return fmt.Sprintf("✅ KOMUT GÖNDERİLDİ: '%s'\n\n[İLK TEPKİ]:\n%s\n👉 (Uzun işlem ise 'terminal' ile takip et).", cmdStr, strings.TrimSpace(initialOutput)), nil
}

func (t *SSHTool) handleUpload(session *SSHSession, args map[string]interface{}) (string, error) {
	localPath := getStringArg(args, "local", "")
	remotePath := getStringArg(args, "remote", "")

	if localPath == "" {
		return "", fmt.Errorf("'local' parametresi eksik")
	}
	if remotePath == "" {
		return "", fmt.Errorf("'remote' parametresi eksik")
	}
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

	_ = session.SFTPClient.MkdirAll(filepath.Dir(remotePath))

	dst, err := session.SFTPClient.Create(remotePath)
	if err != nil {
		return "", fmt.Errorf("uzak dosya oluşturulamadı: %v", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("yükleme hatası: %v", err)
	}
	return fmt.Sprintf("📤 BAŞARILI: %s -> %s", localPath, remotePath), nil
}

func (t *SSHTool) handleDownload(session *SSHSession, args map[string]interface{}) (string, error) {
	localPath := getStringArg(args, "local", "")
	remotePath := getStringArg(args, "remote", "")

	if localPath == "" {
		return "", fmt.Errorf("'local' parametresi eksik")
	}
	if remotePath == "" {
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

	_ = os.MkdirAll(filepath.Dir(localPath), 0755)

	dst, err := os.Create(localPath)
	if err != nil {
		return "", fmt.Errorf("yerel dosya oluşturulamadı: %v", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("indirme hatası: %v", err)
	}
	return fmt.Sprintf("📥 BAŞARILI: İndirildi -> %s", localPath), nil
}

func (t *SSHTool) closeAndRemoveSession(host string, session *SSHSession) {
	logger.Action("🔌 [%s] Bağlantıları koparılıyor...", host)

	closeSSHSession(session)

	sshMu.Lock()
	delete(sshSessions, host)
	sshMu.Unlock()

	logger.Debug("🔌 [%s] SSH session temizlendi", host)
}

func (t *SSHTool) CleanupInactive(maxIdle time.Duration) int {
	if maxIdle == 0 {
		maxIdle = SSHIdleTimeout
	}
	return t.cleanupSessions(func(sess *SSHSession, now time.Time) bool {
		return sess.IsIdle(maxIdle)
	}, "idle")
}

func (t *SSHTool) CleanupExpired(maxAge time.Duration) int {
	if maxAge == 0 {
		maxAge = SSHMaxSessionAge
	}
	return t.cleanupSessions(func(sess *SSHSession, now time.Time) bool {
		return sess.IsExpired(maxAge)
	}, "expired")
}

func (t *SSHTool) cleanupSessions(shouldCleanup func(*SSHSession, time.Time) bool, reason string) int {
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

		if shouldCleanup(sess, now) {
			logger.Info("🧹 [SSH] %s session temizleniyor: %s", reason, host)
			closeSSHSession(sess)
			delete(sshSessions, host)
			cleaned++
		}
	}

	if cleaned > 0 {
		logger.Info("🧹 [SSH] %d %s session temizlendi", cleaned, reason)
	}
	return cleaned
}

func (t *SSHTool) CleanupAll() int {
	sshMu.Lock()
	defer sshMu.Unlock()

	cleaned := 0
	for host, sess := range sshSessions {
		if sess != nil {
			logger.Info("🧹 [SSH] Shutdown cleanup: %s", host)
			closeSSHSession(sess)
		}
		delete(sshSessions, host)
		cleaned++
	}

	logger.Info("🧹 [SSH] Shutdown: %d session temizlendi", cleaned)
	return cleaned
}

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

func (t *SSHTool) ListSessions() []string {
	sshMu.RLock()
	defer sshMu.RUnlock()

	hosts := make([]string, 0, len(sshSessions))
	for host := range sshSessions {
		hosts = append(hosts, host)
	}
	return hosts
}

func (t *SSHTool) GetSessionCount() int {
	sshMu.RLock()
	defer sshMu.RUnlock()
	return len(sshSessions)
}
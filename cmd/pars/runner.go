package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/agent"
	"github.com/aydndglr/pars-agent-v3/internal/brain/providers"
	"github.com/aydndglr/pars-agent-v3/internal/communication/whatsapp"
	"github.com/aydndglr/pars-agent-v3/internal/core/config"
	"github.com/aydndglr/pars-agent-v3/internal/core/heartbeat"
	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	coreSkills "github.com/aydndglr/pars-agent-v3/internal/core/skills"
	"github.com/aydndglr/pars-agent-v3/internal/db_manager"
	"github.com/aydndglr/pars-agent-v3/internal/ipc"
	"github.com/aydndglr/pars-agent-v3/internal/memory"
	"github.com/aydndglr/pars-agent-v3/internal/skills"
	"github.com/aydndglr/pars-agent-v3/internal/skills/browser"
	"github.com/aydndglr/pars-agent-v3/internal/skills/coding"
	"github.com/aydndglr/pars-agent-v3/internal/skills/filesystem"
	"github.com/aydndglr/pars-agent-v3/internal/skills/healt"
	"github.com/aydndglr/pars-agent-v3/internal/skills/kangal"
	"github.com/aydndglr/pars-agent-v3/internal/skills/network"
	"github.com/aydndglr/pars-agent-v3/internal/skills/rag"
	"github.com/aydndglr/pars-agent-v3/internal/skills/system"
	"github.com/aydndglr/pars-agent-v3/internal/skills/security"
)

// =====================================================================
// 🚀 YENİ: ÖZEL HTTP CLIENT (STREAM İÇİN OPTİMİZE EDİLDİ)
// =====================================================================
var streamHTTPClient = &http.Client{
	Timeout: 30 * time.Minute, // Uzun görevler için
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     5 * time.Minute, // ⚠️ 90sn → 5dk
		DisableKeepAlives:   false,
	},
}

// =====================================================================
// 🧠 SYSTEM KERNEL: DAEMON INITIALIZATION
// =====================================================================
func startDaemon() {
	// 📍 ABSOLUTE PATH RESOLUTION: Pars'ın fiziki konumunu (Binary) sabitliyoruz.
	var baseDir string
	exePath, err := os.Executable()
	// Eğer derlenmiş bir dosya ise (arka plan servisi) ve geçici bir go-build dizini değilse:
	if err == nil && !strings.Contains(filepath.ToSlash(exePath), "go-build") && !strings.Contains(filepath.ToSlash(exePath), "Temp") {
		baseDir = filepath.Dir(exePath)
	} else {
		// Geliştirme aşamasındaysa (go run) o anki çalışma dizinini al
		baseDir, _ = os.Getwd()
	}

	configPath := filepath.Join(baseDir, "config", "config.yaml")
	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Error("❌ Configuration file missing or invalid: %v", err)
		os.Exit(1)
	}
	cfg.App.WorkDir = baseDir

	// ---------------------------------------------------------
	// 🗄️ VERİTABANI YOLLARI (MICRO-DB ARCHITECTURE)
	// ---------------------------------------------------------
	dbDir := filepath.Join(baseDir, "db")
	os.MkdirAll(dbDir, 0755)

	// Kilitlenme riskini sıfıra indiren izole veritabanı yollarımız
	memPath := filepath.Join(dbDir, "pars_memory.db")
	docsPath := filepath.Join(dbDir, "pars_docs.db")
	tasksPath := filepath.Join(dbDir, "pars_tasks.db")
	waDBPath := filepath.Join(dbDir, "wa.db")

	// ---------------------------------------------------------
	// 📡 WHATSAPP BRIDGE & STEALTH MODE CHECKS
	// ---------------------------------------------------------
	shouldHide := true
	whatsappNeedsAuth := false

	if cfg.Communication.Whatsapp.Enabled {
		if _, err := os.Stat(waDBPath); os.IsNotExist(err) {
			shouldHide = false
			whatsappNeedsAuth = true
			fmt.Println("\n\033[93m[ SYSTEM ALERT ] WhatsApp database not found. Authentication required.\033[0m")
			fmt.Println("\033[93m[ ACTION REQUIRED ] Please scan the upcoming QR Code to establish the bridge.\033[0m")
			fmt.Println("\033[90m(Once connected, you may close this terminal. Future initializations will run in stealth mode.)\033[0m\n")
		}
	}

	if runtime.GOOS == "windows" && shouldHide {
		hideConsole()
	}

	logDir := filepath.Join(baseDir, "logs")
	logger.Setup(cfg.App.Debug, logDir)
	logger.Info("🚀 Pars Daemon Service Initializing...")
	logger.Info("🔒 Security Clearance Level: %s", cfg.Security.Level)

	// ---------------------------------------------------------
	// 🧠 AI ENGINE (LLM) INITIALIZATION
	// ---------------------------------------------------------
	var primaryBrain kernel.Brain
	var secondaryBrain kernel.Brain

	switch cfg.Brain.Primary.Provider {
	case "gemini":
		if cfg.Brain.APIKeys.Gemini == "" {
			logger.Error("💥 Primary Brain Boot Failure: Gemini API key missing.")
			os.Exit(1)
		}
		primaryBrain = providers.NewGemini(cfg.Brain.Primary.BaseURL, cfg.Brain.APIKeys.Gemini, cfg.Brain.Primary.ModelName)
		logger.Success("🧠 Primary Engine: Google Gemini (%s)", cfg.Brain.Primary.ModelName)

	case "openai":
		if cfg.Brain.APIKeys.OpenAI == "" {
			logger.Error("💥 Primary Brain Boot Failure: OpenAI API key missing.")
			os.Exit(1)
		}
		primaryBrain = providers.NewOpenAI(cfg.Brain.Primary.BaseURL, cfg.Brain.APIKeys.OpenAI, cfg.Brain.Primary.ModelName)
		logger.Success("🧠 Primary Engine: OpenAI (%s)", cfg.Brain.Primary.ModelName)

	case "ollama":
		primaryBrain = providers.NewOllama(cfg.Brain.Primary.BaseURL, cfg.Brain.Primary.ModelName, cfg.Brain.Primary.Temperature, cfg.Brain.Primary.NumCtx, cfg.Brain.APIKeys.Ollama)
		logger.Success("🧠 Primary Engine: Local Ollama (%s)", cfg.Brain.Primary.ModelName)

	default:
		logger.Error("💥 Unknown primary provider specified: %s", cfg.Brain.Primary.Provider)
		os.Exit(1)
	}

	if cfg.Brain.Secondary.Enabled {
		switch cfg.Brain.Secondary.Provider {
		case "ollama", "ollama_remote":
			secondaryBrain = providers.NewOllama(cfg.Brain.Secondary.BaseURL, cfg.Brain.Secondary.ModelName, cfg.Brain.Primary.Temperature, 8192, cfg.Brain.APIKeys.Ollama)
			logger.Success("🐯 Secondary (Worker) Engine: Ollama (%s) Active", cfg.Brain.Secondary.ModelName)
		case "gemini":
			secondaryBrain = providers.NewGemini(cfg.Brain.Secondary.BaseURL, cfg.Brain.APIKeys.Gemini, cfg.Brain.Secondary.ModelName)
			logger.Success("🐯 Secondary (Worker) Engine: Gemini (%s) Active", cfg.Brain.Secondary.ModelName)
		case "openai":
			secondaryBrain = providers.NewOpenAI(cfg.Brain.Secondary.BaseURL, cfg.Brain.APIKeys.OpenAI, cfg.Brain.Secondary.ModelName)
			logger.Success("🐯 Secondary (Worker) Engine: OpenAI (%s) Active", cfg.Brain.Secondary.ModelName)
		}
	}

	// ---------------------------------------------------------
	// 🧠 UZUN SÜRELİ HAFIZA VE DİL KÜTÜPHANESİ BAŞLATMA
	// ---------------------------------------------------------
	memStore, err := memory.NewSQLiteStore(memPath)
	if err != nil {
		logger.Error("💥 Hafıza Merkezi başlatılamadı: %v", err)
	} else {
		logger.Success("🧠 SQLite Uzun Süreli Hafıza Merkezi Aktif (pars_memory.db)")
	}

	docsStore, err := memory.NewSQLiteStore(docsPath)
	if err != nil {
		logger.Error("💥 Dil Kütüphanesi başlatılamadı: %v", err)
	} else {
		logger.Success("📚 SQLite Dil Kütüphanesi (Oracle) Aktif (pars_docs.db)")
	}

	// ---------------------------------------------------------
	// 🐍 PYTHON UV ENVIRONMENT BOOTSTRAPPING
	// ---------------------------------------------------------
	envPath := filepath.Join(baseDir, "tools")

	pythonEnv, err := skills.SetupVenv(envPath)
	if err != nil {
		logger.Error("💥 Python/UV izole ortamı kurulamadı: %v", err)
		os.Exit(1)
	}
	logger.Success("🐍 İzole 'uv' (Hayalet Venv) altyapısı hazır. UV Yolu: %s", pythonEnv.UvPath)

	// ---------------------------------------------------------
	// 🛠️ TOOL REGISTRY & CAPABILITY BINDING
	// ---------------------------------------------------------
	skillMgr := skills.NewManager()

	nativeTools := []kernel.Tool{
		coding.NewAICodeTool(pythonEnv.BaseDir),
		coding.NewDevStudio(pythonEnv.BaseDir, pythonEnv.PipPath, pythonEnv.PythonPath, pythonEnv.UvPath),
		coding.NewEditor(pythonEnv.BaseDir, pythonEnv.PipPath, pythonEnv.PythonPath, pythonEnv.UvPath),
		coding.NewDeleter(pythonEnv.BaseDir),
		&filesystem.ListTool{},
		&filesystem.ReadTool{},
		&filesystem.WriteTool{},
		&filesystem.DeleteTool{},
		&filesystem.SearchTool{},
		&rag.IndexerTool{Store: memStore},
		&rag.AskCodebaseTool{Store: memStore},
		&rag.ListRAGProjectsTool{Store: memStore},
		&network.SSHTool{},
		&network.StackOverflowTool{},
		&network.GitHubTool{},
		&network.DBQueryTool{},
		&network.NetworkMonitoringTool{},
		&system.UniversalShellTool{Config: cfg},
		&browser.BrowserTool{},
		&rag.AskOracleTool{DocStore: docsStore},
		&rag.OracleIndexTool{DocStore: docsStore},
		&rag.ChatRecallTool{Store: memStore},
	}

	for _, t := range nativeTools {
		skillMgr.Register(t)
		coding.RegisterToolToDB(pythonEnv.BaseDir, t.Name(), "native", t.Description(), "", t.Parameters(), false, "")
	}

	// ---------------------------------------------------------
	// ⚙️ DYNAMIC PLUGIN LOADER
	// ---------------------------------------------------------
	loader := skills.NewLoader(skillMgr, pythonEnv.BaseDir, pythonEnv.PythonPath, pythonEnv.UvPath)
	if err := loader.LoadAll(); err != nil {
		logger.Warn("⚠️ Warnings encountered during Python plugin injection: %v", err)
	}

	// ---------------------------------------------------------
	// 🧬 AGENT INSTANTIATION & LIFECYCLE MANAGEMENT
	// ---------------------------------------------------------
	pars := agent.NewPars(cfg, primaryBrain, secondaryBrain, skillMgr, memStore)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 🚨🚨🚨 TELEMETRİ MOTORU 🚨🚨🚨
	logger.Action("🩺 Telemetri Motoru Başlatılıyor...")
	telemetrySvc := healt.NewTelemetryService(pars.EventChannel)
	telemetrySvc.Start()

	// 🛡️🛡️🛡️ PARS EDR (SİBER GÜVENLİK) MOTORU 🛡️🛡️🛡️
	securityEngine := security.NewSecurityEngine(pars.EventChannel)
	securityEngine.StartAll()

	// 🚀 SAĞLIK ARACI
	hcTool := &healt.HealthCheckTool{TelemetrySvc: telemetrySvc}
	pars.RegisterTool(hcTool)
	coding.RegisterToolToDB(pythonEnv.BaseDir, hcTool.Name(), "native", hcTool.Description(), "", hcTool.Parameters(), false, "")

	go ipc.StartServer(ctx, pars)
	go func() {
		userSkillsPath := filepath.Join(baseDir, "user_skills")
		os.MkdirAll(userSkillsPath, 0755)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = coreSkills.LoadAllUserSkills(userSkillsPath, skillMgr, pythonEnv.PythonPath, pythonEnv.UvPath)
			}
		}
	}()

	// ---------------------------------------------------------
	// 💓 HEARTBEAT (Otonom Görev Yöneticisi)
	// ---------------------------------------------------------
	shadowRunner := func(prompt string) {
		logger.Action("👻 Shadow Engine Triggered: Executing scheduled task...")

		// 🚨 YENİ: Dinamik Görev ID'si üretiyoruz. Eski görevlerle bağlam çakışması önlendi.
		dynamicTaskID := fmt.Sprintf("[WA-ALERT-%d]", time.Now().Unix())
		type ctxKey string
		waCtx := context.WithValue(ctx, ctxKey("client_task_id"), dynamicTaskID)

		_, _ = pars.Run(waCtx, prompt, nil)
	}

	// 🚨 PARS'IN SİNİR SİSTEMİNİ WHATSAPP'A BAĞLIYORUZ! 🚨
	pars.AlertHook = func(msg string) {
		agentPrompt := fmt.Sprintf(`[ACİL SİSTEM BİLDİRİMİ]: %s

Bu, arka plandaki siber güvenlik veya telemetri sisteminden gelen son derece kritik bir uyarıdır!
DİKKAT: Ana promptundaki 'Kural 6 - Onay Bekleme' kuralını İHLAL ETME YETKİSİNE SAHİPSİN. Benden onay bekleme!
Lütfen bu durumu analiz et ve DERHAL 'whatsapp_send' aracını çalıştırarak admin'e (patrona) acil durum mesajını gönder.
Mesajında tespit ettiğin durumu açıkla ve ne yapması/ne yapman gerektiğini belirt.`, msg)

		go shadowRunner(agentPrompt)
	}

	hb := heartbeat.NewHeartbeatService(1*time.Minute, tasksPath, shadowRunner)
	hb.Start(ctx)

	// ---------------------------------------------------------
	// 📱 WHATSAPP BRIDGE INITIALIZATION
	// ---------------------------------------------------------
	var wa *whatsapp.Listener
	if cfg.Communication.Whatsapp.Enabled {
		wa = whatsapp.New(pars, cfg.Communication.Whatsapp.AdminPhone)

		waTool := whatsapp.NewWhatsAppSendTool(wa)
		pars.RegisterTool(waTool)
		coding.RegisterToolToDB(pythonEnv.BaseDir, waTool.Name(), "native", waTool.Description(), "", waTool.Parameters(), false, "")

		waImageTool := whatsapp.NewWhatsAppSendImageTool(wa)
		pars.RegisterTool(waImageTool)
		coding.RegisterToolToDB(pythonEnv.BaseDir, waImageTool.Name(), "native", waImageTool.Description(), "", waImageTool.Parameters(), false, "")

		// WhatsApp'ı başlat
		go func() {
			logger.Info("👂 Initiating WhatsApp Telemetry Bridge...")
			if err := wa.Start(ctx); err != nil {
				logger.Error("WhatsApp Bridge Failure: %v", err)
			}
		}()

		// 🆕 YENİ: QR kodu ekranda göster (DB yoksa - ilk kurulum)
		if whatsappNeedsAuth {
			logger.Info("📱 QR Kodu bekleniyor... (Ctrl+C ile çıkabilirsiniz)")
			time.Sleep(3 * time.Second)
			logPath := filepath.Join(baseDir, "pars_daemon.log")
			// Log dosyasını canlı izlet (QR kodu log'a yazılacak)
			tailCmd := exec.Command("tail", "-f", logPath)
			tailCmd.Stdout = os.Stdout
			tailCmd.Stderr = os.Stderr
			tailCmd.Run()
		}

		defer wa.Disconnect()
	}

	// ---------------------------------------------------------
	// 🐕 KANGAL PROAKTİF BEKÇİ SİSTEMİ
	// ---------------------------------------------------------
	var kangalSvc *kangal.Kangal
	if cfg.Kangal.Enabled {
		// 🚨 KRİTİK: watchdog_model boşsa Kangal sınırlı çalışır
		if cfg.Kangal.WatchdogModel == "" {
			logger.Warn("⚠️ [Kangal] watchdog_model boş, Kangal sınırlı özelliklerle çalışacak")
			logger.Info("💡 [Kangal] Config'e watchdog_model ekleyin (örn: qwen3:1.5b)")
		}

		logger.Action("🐕 Kangal Bekçi Sistemi Başlatılıyor...")

		// 🆕 YENİ: Kangal artık primaryConfig alıyor
		kangalSvc = kangal.NewKangal(&cfg.Kangal, cfg, pars, pars.EventChannel)

		if kangalSvc != nil {
			if err := kangalSvc.Start(); err != nil {
				logger.Warn("⚠️ [Kangal] Başlatma hatası: %v", err)
			} else {
				logger.Success("✅ Kangal Aktif: Watchdog=%s, Sensitivity=%s",
					cfg.Kangal.WatchdogModel, cfg.Kangal.SensitivityLevel)

				// 🆕 Kangal control tool'u register et
				kangalTool := kangal.NewKangalControlTool(kangalSvc)
				if kangalTool != nil {
					pars.RegisterTool(kangalTool)
					coding.RegisterToolToDB(pythonEnv.BaseDir, kangalTool.Name(), "native",
						kangalTool.Description(), "", kangalTool.Parameters(), false, "")
					logger.Success("✅ [Kangal] kangal_control tool kaydedildi")
				}
			}
		}
	} else {
		logger.Info("🐕 [Kangal] Devre dışı (config.yaml → kangal.enabled: false)")
		logger.Info("💡 [Kangal] Aktif etmek için config.yaml'de kangal.enabled: true yapın")
	}

	// ---------------------------------------------------------
	// 🛑 GRACEFUL SHUTDOWN
	// ---------------------------------------------------------
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	logger.Info("\n🛑 Terminating background services and persisting state...")

	telemetrySvc.Stop()
	securityEngine.StopAll()

	// 🆕 Kangal'ı güvenli şekilde kapat
	if kangalSvc != nil {
		logger.Info("🐕 [Kangal] Bekçi sistemi kapatılıyor...")
		kangalSvc.Stop()
	}

	db_manager.CloseAll()

	cancel()
	logger.Close()
	os.Exit(0)
}


// =====================================================================
// 💻 CLIENT UI: INTERACTIVE TERMINAL SESSION  /QWEN
// =====================================================================
func startInteractiveCLI() { 
	printFixedHeader()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		fmt.Println("\n\n\033[92m[PA]🐯[RS]:\033[0m Terminating client session. The daemon remains active. Goodbye, Sir.")
		os.Exit(0)
	}()

	clientID := fmt.Sprintf("CLI-%d", time.Now().UnixNano()%1000000)

	// 🚀 YENİ: Stream goroutine için context (graceful cancel için)
	streamCtx, streamCancel := context.WithCancel(context.Background())
	defer streamCancel()

	go func() {
		for {
			select {
			case <-streamCtx.Done():
				return
			default:
			}

			url := fmt.Sprintf("http://localhost:5137/stream?task_id=%s", clientID)

			// 🚨 DÜZELTME #1: Context-aware request
			req, err := http.NewRequestWithContext(streamCtx, "GET", url, nil)
			if err != nil {
				time.Sleep(2 * time.Second)
				continue
			}

			// 🚨 DÜZELTME #2: Özel HTTP client kullan (timeout fix)
			resp, err := streamHTTPClient.Do(req)
			if err != nil {
				time.Sleep(2 * time.Second)
				continue
			}

			// 🚨 DÜZELTME #3: Büyük buffer (64KB → 1MB)
			scanner := bufio.NewScanner(resp.Body)
			buf := make([]byte, 0, 256*1024)
			scanner.Buffer(buf, 1024*1024) // Max 1MB token

			var finalMessageReceived bool
			var lastTokenTime time.Time
			var tokenCount int

			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "data: ") {
					msg := strings.TrimPrefix(line, "data: ")
					msg = strings.TrimSpace(msg)  // ← YENİ: Boşlukları temizle
					if msg == "" {
						continue  // ← YENİ: Boş mesajları atla
					}
					if strings.HasPrefix(msg, "TOKEN::") {
						token := strings.TrimPrefix(msg, "TOKEN::")
						fmt.Print(token)
						lastTokenTime = time.Now()
						tokenCount++
					} else {
						// ← YENİ: Final mesajı daha görünür yap
						fmt.Printf("\n\n\033[36m[🐯]:\033[0m %s\n", msg)
						finalMessageReceived = true
					}
				}
			}

			// 🚨 DÜZELTME #4: Scanner error check
			if err := scanner.Err(); err != nil {
				logger.Warn("⚠️ [CLI] Stream okuma hatası: %v", err)
			}

			resp.Body.Close()

			// 🚨 DÜZELTME #5: DRAIN PERIOD - Final mesaj sonrası buffer boşalmasını bekle
			if finalMessageReceived {
				remaining := 1500*time.Millisecond - time.Since(lastTokenTime)
				if remaining > 0 {
					time.Sleep(remaining) // ← Buffer'ın boşalmasını bekle
				}
			}

			time.Sleep(1 * time.Second)
		}
	}()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\n[👤]: ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())

		if input == "exit" || input == "quit" {
			fmt.Println("\n\033[92m[PA]🐯[RS]:\033[0m Disconnecting. Goodbye, Sir.")
			break
		}
		if input == "" {
			continue
		}

		fmt.Print("\n\033[36m[🐯]:\033[0m ")

		_, err := ipc.SendCommand(clientID, input)
		if err != nil {
			fmt.Printf("\n\033[91m❌ Connection Error:\033[0m %v\n", err)
			fmt.Println("\033[90m(Ensure the Pars Daemon is currently active in the background)\033[0m")
		} else {
			fmt.Println("\n\033[90m--- Görev Sonu ---\033[0m")
		}
	}
}
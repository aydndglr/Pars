package main

import (
	"bufio"
	"bytes"
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

var streamHTTPClient = &http.Client{
	Timeout: 30 * time.Minute,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     5 * time.Minute,
		DisableKeepAlives:   false,
	},
}

func startDaemon() {
	logger.Info("🚀 [Runner] startDaemon başlatılıyor...")

	baseDir := db_manager.GetBaseDir()
	logger.Info("📍 [Runner] BaseDir belirlendi: %s", baseDir)

	configPath := filepath.Join(baseDir, "config", "config.yaml")
	logger.Info("📄 [Runner] Config path: %s", configPath)

	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Error("❌ [Runner] Configuration file missing or invalid: %v", err)
		os.Exit(1)
	}
	cfg.App.WorkDir = baseDir
	logger.Info("✅ [Runner] Config yüklendi, WorkDir: %s", cfg.App.WorkDir)

	dbDir := filepath.Join(baseDir, "db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		logger.Error("❌ [Runner] DB dizini oluşturulamadı: %v", err)
		os.Exit(1)
	}
	logger.Info("✅ [Runner] DB dizini hazır: %s", dbDir)

	memPath, _ := db_manager.GetDBPath("pars_memory.db")
	docsPath, _ := db_manager.GetDBPath("pars_docs.db")
	tasksPath, _ := db_manager.GetDBPath("pars_tasks.db")
	waDBPath, _ := db_manager.GetDBPath("wa.db")

	logger.Debug("🗄️ [Runner] DB Paths:")
	logger.Debug("   - Memory: %s", memPath)
	logger.Debug("   - Docs: %s", docsPath)
	logger.Debug("   - Tasks: %s", tasksPath)
	logger.Debug("   - WhatsApp: %s", waDBPath)

	shouldHide := true
	whatsappNeedsAuth := false

	if cfg.Communication.Whatsapp.Enabled {
		logger.Info("📱 [Runner] WhatsApp enabled, checking auth...")
		if _, err := os.Stat(waDBPath); os.IsNotExist(err) {
			shouldHide = false
			whatsappNeedsAuth = true
			logger.Warn("⚠️ [Runner] WhatsApp database not found, auth required")
			fmt.Println("\n\033[93m[ SYSTEM ALERT ] WhatsApp database not found. Authentication required.\033[0m")
			fmt.Println("\033[93m[ ACTION REQUIRED ] Please scan the upcoming QR Code to establish the bridge.\033[0m")
			fmt.Println("\033[90m(Once connected, you may close this terminal. Future initializations will run in stealth mode.)\033[0m\n")
		} else {
			logger.Info("✅ [Runner] WhatsApp database found, auth OK")
		}
	}

	if runtime.GOOS == "windows" && shouldHide {
		logger.Info("🪟 [Runner] Windows detected, hiding console...")
		hideConsole()
	}

	logDir := filepath.Join(baseDir, "logs")
	logger.Setup(cfg.App.Debug, logDir)
	logger.Info("📝 [Runner] Logger setup complete, logDir: %s", logDir)
	logger.Info("🚀 Pars Daemon Service Initializing...")
	logger.Info("🔒 Security Clearance Level: %s", cfg.Security.Level)

	var primaryBrain kernel.Brain
	var secondaryBrain kernel.Brain

	logger.Info("🧠 [Runner] Primary brain initializing...")
	switch cfg.Brain.Primary.Provider {
	case "gemini":
		if cfg.Brain.APIKeys.Gemini == "" {
			logger.Error("💥 [Runner] Primary Brain Boot Failure: Gemini API key missing.")
			os.Exit(1)
		}
		primaryBrain = providers.NewGemini(cfg.Brain.Primary.BaseURL, cfg.Brain.APIKeys.Gemini, cfg.Brain.Primary.ModelName)
		logger.Success("🧠 [Runner] Primary Engine: Google Gemini (%s)", cfg.Brain.Primary.ModelName)
	case "qwen":
		if cfg.Brain.APIKeys.Qwen == "" {
			logger.Error("💥 [Runner] Primary Brain Boot Failure: Qwen API key missing.")
			os.Exit(1)
		}
		primaryBrain = providers.NewQwen(
			cfg.Brain.Primary.BaseURL,
			cfg.Brain.APIKeys.Qwen,
			cfg.Brain.Primary.ModelName,
			cfg.Brain.Primary.Temperature,
			cfg.Brain.Primary.NumCtx,
		)
		logger.Success("🧠 [Runner] Primary Engine: Alibaba Qwen (%s)", cfg.Brain.Primary.ModelName)
	case "openai":
		if cfg.Brain.APIKeys.OpenAI == "" {
			logger.Error("💥 [Runner] Primary Brain Boot Failure: OpenAI API key missing.")
			os.Exit(1)
		}
		primaryBrain = providers.NewOpenAI(cfg.Brain.Primary.BaseURL, cfg.Brain.APIKeys.OpenAI, cfg.Brain.Primary.ModelName)
		logger.Success("🧠 [Runner] Primary Engine: OpenAI (%s)", cfg.Brain.Primary.ModelName)
	case "ollama":
		primaryBrain = providers.NewOllama(cfg.Brain.Primary.BaseURL, cfg.Brain.Primary.ModelName, cfg.Brain.Primary.Temperature, cfg.Brain.Primary.NumCtx, cfg.Brain.APIKeys.Ollama)
		logger.Success("🧠 [Runner] Primary Engine: Local Ollama (%s)", cfg.Brain.Primary.ModelName)
	default:
		logger.Error("💥 [Runner] Unknown primary provider specified: %s", cfg.Brain.Primary.Provider)
		os.Exit(1)
	}

	if cfg.Brain.Secondary.Enabled {
		logger.Info("🐯 [Runner] Secondary brain enabled, initializing...")
		switch cfg.Brain.Secondary.Provider {
		case "ollama", "ollama_remote":
			secondaryBrain = providers.NewOllama(cfg.Brain.Secondary.BaseURL, cfg.Brain.Secondary.ModelName, cfg.Brain.Primary.Temperature, 8192, cfg.Brain.APIKeys.Ollama)
			logger.Success("🐯 [Runner] Secondary (Worker) Engine: Ollama (%s) Active", cfg.Brain.Secondary.ModelName)
		case "gemini":
			secondaryBrain = providers.NewGemini(cfg.Brain.Secondary.BaseURL, cfg.Brain.APIKeys.Gemini, cfg.Brain.Secondary.ModelName)
			logger.Success("🐯 [Runner] Secondary (Worker) Engine: Gemini (%s) Active", cfg.Brain.Secondary.ModelName)
		case "openai":
			secondaryBrain = providers.NewOpenAI(cfg.Brain.Secondary.BaseURL, cfg.Brain.APIKeys.OpenAI, cfg.Brain.Secondary.ModelName)
			logger.Success("🐯 [Runner] Secondary (Worker) Engine: OpenAI (%s) Active", cfg.Brain.Secondary.ModelName)
		}
	} else {
		logger.Info("ℹ️ [Runner] Secondary brain disabled")
	}

	logger.Info("🧠 [Runner] Memory stores initializing...")
	memStore, err := memory.NewSQLiteStore(memPath)
	if err != nil {
		logger.Error("💥 [Runner] Hafıza Merkezi başlatılamadı: %v", err)
	} else {
		logger.Success("🧠 [Runner] SQLite Uzun Süreli Hafıza Merkezi Aktif (pars_memory.db)")
	}

	docsStore, err := memory.NewSQLiteStore(docsPath)
	if err != nil {
		logger.Error("💥 [Runner] Dil Kütüphanesi başlatılamadı: %v", err)
	} else {
		logger.Success("📚 [Runner] SQLite Dil Kütüphanesi (Oracle) Aktif (pars_docs.db)")
	}

	envPath := filepath.Join(baseDir, "tools")
	logger.Info("🐍 [Runner] Python environment setup, path: %s", envPath)

	pythonEnv, err := skills.SetupVenv(envPath)
	if err != nil {
		logger.Error("💥 [Runner] Python/UV izole ortamı kurulamadı: %v", err)
		os.Exit(1)
	}
	logger.Success("🐍 [Runner] İzole 'uv' (Hayalet Venv) altyapısı hazır. UV Yolu: %s", pythonEnv.UvPath)

	logger.Info("🛠️ [Runner] Skill manager initializing...")
	skillMgr := skills.NewManager()

	nativeTools := []kernel.Tool{
		coding.NewAICodeTool(pythonEnv.BaseDir),
		coding.NewDevStudio(pythonEnv.BaseDir, pythonEnv.PipPath, pythonEnv.PythonPath, pythonEnv.UvPath),
		coding.NewEditor(pythonEnv.BaseDir, pythonEnv.PipPath, pythonEnv.PythonPath, pythonEnv.UvPath),
		coding.NewDeleter(pythonEnv.BaseDir),
		&filesystem.ListTool{},
		&filesystem.ReadTool{},
		&filesystem.WriteTool{SecurityLevel: cfg.Security.Level},
		&filesystem.DeleteTool{SecurityLevel: cfg.Security.Level},
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

	logger.Info("🛠️ [Runner] Registering %d native tools...", len(nativeTools))
	for _, t := range nativeTools {
		skillMgr.Register(t)
		coding.RegisterToolToDB(pythonEnv.BaseDir, t.Name(), "native", t.Description(), "", t.Parameters(), false, "")
		logger.Debug("✅ [Runner] Tool registered: %s", t.Name())
	}
	logger.Success("✅ [Runner] All native tools registered")

	logger.Info("📋 [Runner] Task Management Tools register ediliyor...")

	createTaskTool := network.NewCreateTaskTool(tasksPath)
	updateTaskStatusTool := network.NewUpdateTaskStatusTool(tasksPath)
	listTasksTool := network.NewListTasksTool(tasksPath)
	deleteTaskTool := network.NewDeleteTaskTool(tasksPath)

	if createTaskTool != nil {
		skillMgr.Register(createTaskTool)
		coding.RegisterToolToDB(pythonEnv.BaseDir, createTaskTool.Name(), "native",
			createTaskTool.Description(), "", createTaskTool.Parameters(), false, "")
		logger.Success("✅ [Runner] create_task tool kaydedildi")
	}

	if updateTaskStatusTool != nil {
		skillMgr.Register(updateTaskStatusTool)
		coding.RegisterToolToDB(pythonEnv.BaseDir, updateTaskStatusTool.Name(), "native",
			updateTaskStatusTool.Description(), "", updateTaskStatusTool.Parameters(), false, "")
		logger.Success("✅ [Runner] update_task_status tool kaydedildi")
	}

	if listTasksTool != nil {
		skillMgr.Register(listTasksTool)
		coding.RegisterToolToDB(pythonEnv.BaseDir, listTasksTool.Name(), "native",
			listTasksTool.Description(), "", listTasksTool.Parameters(), false, "")
		logger.Success("✅ [Runner] list_tasks tool kaydedildi")
	}

	if deleteTaskTool != nil {
		skillMgr.Register(deleteTaskTool)
		coding.RegisterToolToDB(pythonEnv.BaseDir, deleteTaskTool.Name(), "native",
			deleteTaskTool.Description(), "", deleteTaskTool.Parameters(), false, "")
		logger.Success("✅ [Runner] delete_task tool kaydedildi")
	}

	logger.Info("⚙️ [Runner] Dynamic plugin loader initializing...")
	loader := skills.NewLoader(skillMgr, pythonEnv.BaseDir, pythonEnv.PythonPath, pythonEnv.UvPath)
	if err := loader.LoadAll(); err != nil {
		logger.Warn("⚠️ [Runner] Warnings encountered during Python plugin injection: %v", err)
	} else {
		logger.Success("✅ [Runner] All Python plugins loaded")
	}

	logger.Info("🤖 [Runner] Pars agent creating...")
	pars := agent.NewPars(cfg, primaryBrain, secondaryBrain, skillMgr, memStore)
	if pars == nil {
		logger.Error("💥 [Runner] Pars agent oluşturulamadı!")
		os.Exit(1)
	}
	logger.Success("✅ [Runner] Pars agent created successfully")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	systemEventChan := make(chan string, 100)
	logger.Info("📡 [Runner] System event channel created")

	for _, tool := range skillMgr.GetAllTools() {
		if pTool, ok := tool.(*skills.PythonTool); ok {
			pTool.SetNotificationChannel(systemEventChan)
		}
	}
	logger.Info("✅ [Runner] Python tool notification channels configured")

	logger.Action("🩺 [Runner] Telemetri Motoru Başlatılıyor...")
	telemetrySvc := healt.NewTelemetryService(pars.EventChannel)
	telemetrySvc.Start()
	logger.Success("✅ [Runner] Telemetry service started")

	securityEngine := security.NewSecurityEngine(pars.EventChannel)
	securityEngine.StartAll()
	logger.Success("✅ [Runner] Security engine started")

	hcTool := &healt.HealthCheckTool{TelemetrySvc: telemetrySvc}
	pars.RegisterTool(hcTool)
	coding.RegisterToolToDB(pythonEnv.BaseDir, hcTool.Name(), "native", hcTool.Description(), "", hcTool.Parameters(), false, "")
	logger.Success("✅ [Runner] Health check tool registered")

	logger.Info("📡 [Runner] IPC Server starting...")
	go ipc.StartServer(ctx, pars)
	logger.Success("✅ [Runner] IPC Server started on port 5137")

	go func() {
		userSkillsPath := filepath.Join(baseDir, "user_skills")
		if err := os.MkdirAll(userSkillsPath, 0755); err != nil {
			logger.Warn("⚠️ [Runner] user_skills dizini oluşturulamadı: %v", err)
		}
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				err := coreSkills.LoadAllUserSkills(userSkillsPath, skillMgr, pythonEnv.PythonPath, pythonEnv.UvPath)
				if err != nil {
					logger.Debug("⚠️ [Runner] User skills load error: %v", err)
				}

				for _, tool := range skillMgr.GetAllTools() {
					if pTool, ok := tool.(*skills.PythonTool); ok {
						pTool.SetNotificationChannel(systemEventChan)
					}
				}
			}
		}
	}()
	logger.Info("✅ [Runner] User skills watcher started")

	shadowRunner := func(prompt string) {
		logger.Action("👻 [Runner] Shadow Engine Triggered: Executing scheduled task...")

		dynamicTaskID := fmt.Sprintf("[WA-ALERT-%d]", time.Now().Unix())
		type ctxKey string
		waCtx := context.WithValue(ctx, ctxKey("client_task_id"), dynamicTaskID)

		logger.Info("📝 [Runner] Shadow runner executing prompt: %s", prompt[:min(100, len(prompt))])
		_, _ = pars.Run(waCtx, prompt, nil)
	}

	hb := heartbeat.NewHeartbeatService(1*time.Minute, tasksPath, shadowRunner)
	hb.NotificationChan = systemEventChan
	hb.Start(ctx)
	logger.Success("✅ [Runner] Heartbeat service started")

	pars.AlertHook = func(msg string) {
		logger.Action("🚨 [Runner] Alert hook triggered")
		agentPrompt := fmt.Sprintf(`[ACİL SİSTEM BİLDİRİMİ]: %s

Bu, arka plandaki siber güvenlik veya telemetri sisteminden gelen son derece kritik bir uyarıdır!
DİKKAT: Ana promptundaki 'Kural 6 - Onay Bekleme' kuralını İHLAL ETME YETKİSİNE SAHİPSİN. Benden onay bekleme!
Lütfen bu durumu analiz et ve DERHAL 'whatsapp_send' aracını çalıştırarak admin'e (patrona) acil durum mesajını gönder.
Mesajında tespit ettiğin durumu açıkla ve ne yapması/ne yapman gerektiğini belirt.`, msg)

		go shadowRunner(agentPrompt)
	}

	logger.Action("🧹 [Runner] SSH Cleanup Worker başlatılıyor...")
	network.StartCleanupWorker(ctx)
	logger.Success("✅ [Runner] SSH Cleanup Worker started")

	var wa *whatsapp.Listener
	var waTool *whatsapp.WhatsAppSendTool

	if cfg.Communication.Whatsapp.Enabled {
		logger.Info("📱 [Runner] WhatsApp bridge initializing...")
		wa = whatsapp.New(pars, cfg.Communication.Whatsapp.AdminPhone)

		waTool = whatsapp.NewWhatsAppSendTool(wa)
		pars.RegisterTool(waTool)
		coding.RegisterToolToDB(pythonEnv.BaseDir, waTool.Name(), "native", waTool.Description(), "", waTool.Parameters(), false, "")
		logger.Success("✅ [Runner] WhatsApp send tool registered")

		waImageTool := whatsapp.NewWhatsAppSendImageTool(wa)
		pars.RegisterTool(waImageTool)
		coding.RegisterToolToDB(pythonEnv.BaseDir, waImageTool.Name(), "native", waImageTool.Description(), "", waImageTool.Parameters(), false, "")
		logger.Success("✅ [Runner] WhatsApp image send tool registered")

		go func() {
			logger.Info("👂 [Runner] Initiating WhatsApp Telemetry Bridge...")
			if err := wa.Start(ctx); err != nil {
				logger.Error("❌ [Runner] WhatsApp Bridge Failure: %v", err)
			} else {
				logger.Success("✅ [Runner] WhatsApp Bridge started successfully")
			}
		}()

		if whatsappNeedsAuth {
			logger.Info("📱 [Runner] QR Kodu bekleniyor... (Ctrl+C ile çıkabilirsiniz)")
			time.Sleep(3 * time.Second)
			logPath := filepath.Join(baseDir, "pars_daemon.log")
			tailCmd := exec.Command("tail", "-f", logPath)
			tailCmd.Stdout = os.Stdout
			tailCmd.Stderr = os.Stderr
			tailCmd.Run()
		}
		defer wa.Disconnect()
	} else {
		logger.Info("ℹ️ [Runner] WhatsApp disabled in config")
	}

	go func() {
		logger.Info("📡 [Runner] System event listener started")
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-systemEventChan:
				logger.Info("📢 [Runner] System event received: %s", msg[:min(100, len(msg))])
				fmt.Printf("\n\033[93m%s\033[0m\n", msg)
				if cfg.Communication.Whatsapp.Enabled && waTool != nil && cfg.Communication.Whatsapp.AdminPhone != "" {
					go func(message string) {
						params := map[string]interface{}{
							"message": message,
							"target":  cfg.Communication.Whatsapp.AdminPhone,
						}
						_, err := waTool.Execute(context.Background(), params)
						if err != nil {
							logger.Warn("⚠️ [Runner] WhatsApp bildirimi gönderilemedi: %v", err)
						} else {
							logger.Info("✅ [Runner] WhatsApp bildirimi gönderildi")
						}
					}(msg)
				}
			}
		}
	}()

	var kangalSvc *kangal.Kangal
	if cfg.Kangal.Enabled {
		if cfg.Kangal.WatchdogModel == "" {
			logger.Warn("⚠️ [Runner] watchdog_model boş, Kangal sınırlı özelliklerle çalışacak")
			logger.Info("💡 [Runner] Config'e watchdog_model ekleyin (örn: qwen3:1.5b)")
		}

		logger.Action("🐕 [Runner] Kangal Bekçi Sistemi Başlatılıyor...")

		kangalSvc = kangal.NewKangal(&cfg.Kangal, cfg, pars, pars.EventChannel)

		if kangalSvc != nil {
			if err := kangalSvc.Start(); err != nil {
				logger.Warn("⚠️ [Runner] Kangal Başlatma hatası: %v", err)
			} else {
				logger.Success("✅ [Runner] Kangal Aktif: Watchdog=%s, Sensitivity=%s",
					cfg.Kangal.WatchdogModel, cfg.Kangal.SensitivityLevel)

				kangalTool := kangal.NewKangalControlTool(kangalSvc)
				if kangalTool != nil {
					pars.RegisterTool(kangalTool)
					coding.RegisterToolToDB(pythonEnv.BaseDir, kangalTool.Name(), "native",
						kangalTool.Description(), "", kangalTool.Parameters(), false, "")
					logger.Success("✅ [Runner] kangal_control tool kaydedildi")
				}
			}
		}
	} else {
		logger.Info("🐕 [Runner] Kangal Devre dışı (config.yaml → kangal.enabled: false)")
		logger.Info("💡 [Runner] Kangal Aktif etmek için config.yaml'de kangal.enabled: true yapın")
	}

	logger.Info("🛑 [Runner] Waiting for shutdown signal...")
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	logger.Info("\n🛑 [Runner] Terminating background services and persisting state...")

	logger.Info("🩺 [Runner] Stopping telemetry service...")
	telemetrySvc.Stop()

	logger.Info("🛡️ [Runner] Stopping security engine...")
	securityEngine.StopAll()

	if kangalSvc != nil {
		logger.Info("🐕 [Runner] Kangal Bekçi sistemi kapatılıyor...")
		kangalSvc.Stop()
	}

	if wa != nil {
		logger.Info("📱 [Runner] WhatsApp Bağlantı kapatılıyor...")
		wa.Disconnect()
	}

	if hb != nil {
		logger.Info("💓 [Runner] Heartbeat Servis durduruluyor...")
		hb.Stop()
	}

	logger.Info("🧹 [Runner] SSH Cleanup Worker durduruluyor...")
	network.StopCleanupWorker()

	logger.Info("📡 [Runner] IPC Server durduruluyor...")
	ipc.StopServer()

	logger.Info("🗄️ [Runner] Veritabanı bağlantıları kapatılıyor...")
	db_manager.CloseAll()

	cancel()

	logger.Close()

	logger.Success("✅ [Runner] Tüm servisler güvenli şekilde kapatıldı.")
	os.Exit(0)
}

func startInteractiveCLI() {
	logger.Info("💻 [Runner] startInteractiveCLI başlatılıyor...")
	printFixedHeader()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		logger.Info("💻 [Runner] Client session terminating...")
		fmt.Println("\n\n\033[92m[PA]🐯[RS]:\033[0m Terminating client session. The daemon remains active. Goodbye, Sir.")
		os.Exit(0)
	}()

	clientID := fmt.Sprintf("CLI-%d", time.Now().UnixNano()%1000000)
	logger.Info("🆔 [Runner] Client ID: %s", clientID)

	streamCtx, streamCancel := context.WithCancel(context.Background())
	defer streamCancel()

	go func() {
		logger.Info("📡 [Runner] Stream listener started")
		for {
			select {
			case <-streamCtx.Done():
				logger.Info("📡 [Runner] Stream listener stopped")
				return
			default:
			}

			url := fmt.Sprintf("http://localhost:5137/stream?task_id=%s", clientID)

			req, err := http.NewRequestWithContext(streamCtx, "GET", url, nil)
			if err != nil {
				logger.Debug("⚠️ [Runner] Stream request error: %v", err)
				time.Sleep(2 * time.Second)
				continue
			}

			resp, err := streamHTTPClient.Do(req)
			if err != nil {
				logger.Debug("⚠️ [Runner] Stream connection error: %v", err)
				time.Sleep(2 * time.Second)
				continue
			}

			// 🔥 HAYAT KURTARAN DÜZELTME BURADA 🔥
			// SSE Protocol okuyucusu eklendi. Artık '\n' karakterinde mesaj kırılmayacak.
			scanner := bufio.NewScanner(resp.Body)
			
			// SSE Split Function: Mesajları tek Enter(\n) yerine çift Enter(\n\n) ile ayır.
			splitFunc := func(data []byte, atEOF bool) (advance int, token []byte, err error) {
				if atEOF && len(data) == 0 {
					return 0, nil, nil
				}
				if i := bytes.Index(data, []byte("\n\n")); i >= 0 {
					return i + 2, data[0:i], nil
				}
				if atEOF {
					return len(data), data, nil
				}
				return 0, nil, nil
			}
			scanner.Split(splitFunc)

			buf := make([]byte, 0, 256*1024)
			scanner.Buffer(buf, 1024*1024)

			var finalMessageReceived bool
			var lastTokenTime time.Time

			for scanner.Scan() {
				eventBlock := scanner.Text()
				
				// SSE Bloğu içindeki 'data: ' satırlarını temizle ve birleştir
				lines := strings.Split(eventBlock, "\n")
				var msgBuilder strings.Builder
				
				for _, line := range lines {
					if strings.HasPrefix(line, "data: ") {
						msgBuilder.WriteString(strings.TrimPrefix(line, "data: "))
						msgBuilder.WriteString("\n") // İçerideki \n leri koru
					}
				}
				
				msg := strings.TrimSpace(msgBuilder.String())
				if msg == "" {
					continue
				}

				if strings.HasPrefix(msg, "TOKEN::") {
					token := strings.TrimPrefix(msg, "TOKEN::")
					fmt.Print(token)
					lastTokenTime = time.Now()
				} else {
					logger.Info("📝 [Runner] Final message received: %s", msg[:min(100, len(msg))])
					fmt.Printf("\n\n\033[36m[🐯]:\033[0m %s\n", msg)
					finalMessageReceived = true
				}
			}

			if err := scanner.Err(); err != nil {
				logger.Warn("⚠️ [Runner] Stream okuma hatası: %v", err)
			}

			resp.Body.Close()

			if finalMessageReceived {
				remaining := 1500*time.Millisecond - time.Since(lastTokenTime)
				if remaining > 0 {
					time.Sleep(remaining)
				}
			}

			time.Sleep(1 * time.Second)
		}
	}()

	logger.Info("💬 [Runner] Interactive CLI ready, waiting for user input...")
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\n[👤]: ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())

		if input == "exit" || input == "quit" {
			logger.Info("💬 [Runner] User requested exit")
			fmt.Println("\n\033[92m[PA]🐯[RS]:\033[0m Disconnecting. Goodbye, Sir.")
			break
		}
		if input == "" {
			continue
		}

		logger.Info("💬 [Runner] User input received: %s", input[:min(100, len(input))])
		fmt.Print("\n\033[36m[🐯]:\033[0m ")

		_, err := ipc.SendCommand(clientID, input)
		if err != nil {
			logger.Error("❌ [Runner] IPC command error: %v", err)
			fmt.Printf("\n\033[91m❌ Connection Error:\033[0m %v\n", err)
			fmt.Println("\033[90m(Ensure the Pars Daemon is currently active in the background)\033[0m")
		} else {
			logger.Info("✅ [Runner] Command sent successfully")
			fmt.Println("\n\033[90m--- Görev Sonu ---\033[0m")
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
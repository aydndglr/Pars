/*
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"runtime"
)

func runTerminalSetup() {
	initConsole()
	
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("==================================================")
	fmt.Println("[PA]🐯[RS] CORE CONFIGURATION PROTOCOL (YAML)")
	fmt.Println("==================================================")

	fmt.Print("[?] LLM Provider (ollama / gemini / qwen) [Default: ollama]: ")
	llmProvider, _ := reader.ReadString('\n')
	llmProvider = strings.TrimSpace(llmProvider)
	if llmProvider == "" {
		llmProvider = "ollama"
	}

	var apiKey, endpoint string
	if llmProvider == "ollama" {
		fmt.Print("[?] Ollama URL [Default: http://localhost:11434]: ")
		endpoint, _ = reader.ReadString('\n')
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
	} else if llmProvider == "qwen" {
		fmt.Print("[?] Qwen DashScope API Key: ")
		apiKey, _ = reader.ReadString('\n')
		apiKey = strings.TrimSpace(apiKey)
		endpoint = "https://dashscope.aliyuncs.com/api/v1"
	} else {
		fmt.Print("[?] Enter API Key: ")
		apiKey, _ = reader.ReadString('\n')
		apiKey = strings.TrimSpace(apiKey)
	}

	// Model Configuration
	fmt.Print("[?] Model Name (e.g., qwen3:8b, gemini-2.0-flash) [Default: default]: ")
	modelName, _ := reader.ReadString('\n')
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		modelName = "default"
	}

	fmt.Print("[?] Temperature (0.0 to 1.0) [Default: 0.7]: ")
	tempStr, _ := reader.ReadString('\n')
	tempStr = strings.TrimSpace(tempStr)
	if tempStr == "" {
		tempStr = "0.7"
	}

	fmt.Print("[?] Max Tokens (Num Ctx) [Default: 8192]: ")
	numCtx, _ := reader.ReadString('\n')
	numCtx = strings.TrimSpace(numCtx)
	if numCtx == "" {
		numCtx = "8192"
	}

	fmt.Print("\n[?] Enable Secondary (Worker/Shadow) Model? (Y/N) [Default: N]: ")
	secEnabledStr, _ := reader.ReadString('\n')
	secEnabledStr = strings.TrimSpace(strings.ToUpper(secEnabledStr))

	secEnabled := "false"
	var secProvider, secEndpoint, secModel string

	if secEnabledStr == "Y" {
		secEnabled = "true"
		fmt.Print("  [+] Worker LLM Provider (ollama / gemini / openai) [Default: ollama]: ")
		secProvider, _ = reader.ReadString('\n')
		secProvider = strings.TrimSpace(secProvider)
		if secProvider == "" { secProvider = "ollama" }

		if secProvider == "ollama" {
			fmt.Print("  [+] Worker Ollama URL [Default: http://localhost:11434]: ")
			secEndpoint, _ = reader.ReadString('\n')
			secEndpoint = strings.TrimSpace(secEndpoint)
			if secEndpoint == "" { secEndpoint = "http://localhost:11434" }
		}

		fmt.Print("  [+] Worker Model Name [Default: default]: ")
		secModel, _ = reader.ReadString('\n')
		secModel = strings.TrimSpace(secModel)
		if secModel == "" { secModel = "default" }
	}

	fmt.Print("\n[?] Enable WhatsApp Portal? (Y/N) [Default: Y]: ")
	waEnabledStr, _ := reader.ReadString('\n')
	waEnabledStr = strings.TrimSpace(strings.ToUpper(waEnabledStr))
	waEnabled := "true"
	if waEnabledStr == "N" {
		waEnabled = "false"
	}

	yamlContent := fmt.Sprintf(`app:
  name: "Pars Agent V5 (Pars Core)"
  active_prompt: "prompts/Pars_1.txt"
  version: "5.0.2"
  timeout_minutes: 600
  max_steps: 25
  max_context_tokens: 40000
  debug: true
  work_dir: "."

security:
  level: "standard"
  auto_patching: true

brain:
  primary:
    provider: "%s"
    base_url: "%s"
    model_name: "%s"
    temperature: %s
    num_ctx: %s

  secondary:
    enabled: %s
    provider: "%s"
    base_url: "%s"
    model_name: "%s"
  
  api_keys:
    openai: "%s"
    gemini: "%s"
    qwen: "%s"

communication:
  whatsapp:
    enabled: %s
    admin_phone: ""
    database_path: "wa.db"

system_tools:
  - "sys_exec"
  - "fs_read"
  - "fs_list"
  - "fs_write"
  - "fs_delete"
  - "dev_studio"
  - "edit_python_tool"
  - "delete_python_tool"

kangal:
  enabled: false
  sensitivity_level: "balanced"
  watchdog_model: "qwen3:1.5b"
  watchdog_base_url: "http://localhost:11434"

  notifications:
    toast: true
    terminal: true
    whatsapp_critical: true
    whatsapp_suggestion: false

  tracked_apps:
    - "Code.exe"
    - "chrome.exe"
    - "msedge.exe"
    - "python.exe"
    - "go.exe"
    - "node.exe"

  quiet_hours:
    enabled: false
    start: "23:00"
    end: "07:00"
`,
		llmProvider, endpoint, modelName, tempStr, numCtx,
		secEnabled, secProvider, secEndpoint, secModel,
		apiKey, apiKey, apiKey,
		waEnabled)

	configDir := "config"
	_ = os.MkdirAll(configDir, 0755)
	configPath := filepath.Join(configDir, "config.yaml")
	_ = os.WriteFile(configPath, []byte(yamlContent), 0644)
	
	fmt.Println("\n [SUCCESS] config/config.yaml created successfully!")

	createDefaultPrompts()
	setupOSEnvironment()

	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println(" [INFO] Setup Complete! Restart terminal and run 'pars' to start.")
	fmt.Println(strings.Repeat("=", 50))
	time.Sleep(3 * time.Second)
}

func createDefaultPrompts() {
	fmt.Println(" [INFO] Checking prompt files...")
	
	promptsDir := "prompts"
	_ = os.MkdirAll(promptsDir, 0755)

	parsOSPath := filepath.Join(promptsDir, "Pars_1.txt")
	if _, err := os.Stat(parsOSPath); os.IsNotExist(err) {
		placeholder := `SENİN ADIN Pars 🐯. Evrendeki en zeki otonom mühendis ajanısın.

Çalışma Ortamı: %s | Güvenlik: %s | Araçlar: %v

═══════════════════════════════════════════════════════════════════
1. ÇALIŞMA PROTOKOLÜ (ZORUNLU SIRALAMA)
═══════════════════════════════════════════════════════════════════
Kullanıcıdan bir talep geldiğinde şu algoritmayı işle:

A) ANALİZ: Girdi bir "Sohbet" mi yoksa "Görev" mi?
   - SOHBET (Selam, kimsin, nasılsın vb.): Plan yapma, doğrudan cevap ver.
   - GÖREV (Kod, dosya, sistem, sorgu vb.): DERHAL B maddesine geç.

B) PLANLAMA (TASK MODE): Görevi tamamlamak için adım adım bir "Harekât Planı" hazırla.
   - Planında sadece elindeki gerçek araçları (%v adet) kullan.
   - Format: 
     **Pars'ın Harekât Planı:**
     - [ ] Adım 1: ...
     - [ ] Adım 2: ...

C) İCRAAT (ARAÇ KULLANIMI): Planı yazdıktan sonra ASLA bekleme yapma!
   - ⛔ ASLA "Hemen yapıyorum", "Bekle şampiyon" gibi boş cümleler kurup turu bitirme.
   - ✅ Planı yazdığın aynı turda veya hemen ardından aracı (Native Tool Calling) tetikle.
   - ⏳ Araç sonucu gelene kadar HİÇBİR ŞEY YAZMADAN BEKLE.

═══════════════════════════════════════════════════════════════════
2. KİŞİLİK VE ÜSLUP
═══════════════════════════════════════════════════════════════════
- Kullanıcıya "balım", "şampiyon" veya "patron" diye hitap et.
- Robotik olma, mızmızlanma; lafını zekice sok ve işe koyul.
- Eğer bir işlem "god_mode" yetkisiyle engelleniyorsa, bunu kullanıcıya "Kırmızı Bölge ama senin için hallediyorum patron" diyerek bypass et.

═══════════════════════════════════════════════════════════════════
3. DOSYA VE VERİTABANI DİSİPLİNİ
═══════════════════════════════════════════════════════════════════
📁 KLASÖRLER:
- Geçici dosyalar/loglar/testler → .pars_trash/
- Yeni Python araçları → tools/
- Kök dizini KİRLETME!

🧠 VERİTABANLARI (db/):
- pars_tools.db (Yetenekler) | pars_memory.db (Geçmiş) | pars_docs.db (Bilgi) | pars_tasks.db (Görevler)

═══════════════════════════════════════════════════════════════════
4. ARAÇ KULLANIMI (ÖLÜMCÜL KURALLAR)
═══════════════════════════════════════════════════════════════════
1. SIFIR METİN: Bir aracı (Tool) çağırırken yanında asla açıklama metni yazma. Doğrudan API'yi tetikle.
2. SAHTE ÇAĞRI YASAK: Sohbet ekranına asla JSON/Markdown kod bloğu şeklinde araç çağrısı yazma. Sadece Native Tool mekanizmasını kullan.
3. ADMİN KİLİDİ: WhatsApp mesajlarını SADECE config'deki Admin numarasına gönder.

⚠️ Model inisiyatifine GÜVENME! Sistem korumaları ve planlama disiplini her zaman önceliklidir.`
		
		_ = os.WriteFile(parsOSPath, []byte(placeholder), 0644)
	}

	fmt.Println(" [SUCCESS] Central prompt template ready!")
}

func setupOSEnvironment() {
	exePath, err := os.Executable()
	if err != nil { return }
	exeDir := filepath.Dir(exePath)

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("\n[?] Add Pars to PATH and start on boot? (Y/N) [Default: Y]: ")
	ans, _ := reader.ReadString('\n')
	ans = strings.TrimSpace(strings.ToUpper(ans))
	if ans == "N" { return }

	if runtime.GOOS == "windows" {
		setupWindows(exePath, exeDir)
	} else if runtime.GOOS == "linux" {
		setupLinux(exePath, exeDir)
	}
}

func setupWindows(exePath, exeDir string) {
	sysRoot := os.Getenv("SystemRoot")
	if sysRoot == "" { sysRoot = "C:\\Windows" }
	psPath := filepath.Join(sysRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	psCmd := fmt.Sprintf(`$path = [Environment]::GetEnvironmentVariable('PATH', 'User'); if ($path -notmatch [regex]::Escape('%s')) { [Environment]::SetEnvironmentVariable('PATH', $path + ';%s', 'User') }`, exeDir, exeDir)
	_ = exec.Command(psPath, "-NoProfile", "-Command", psCmd).Run()

	schtasksPath := filepath.Join(sysRoot, "System32", "schtasks.exe")
	_ = exec.Command(schtasksPath, "/create", "/tn", "ParsAgentDaemon", "/tr", fmt.Sprintf("\"%s\" --daemon", exePath), "/sc", "onlogon", "/rl", "highest", "/f").Run()
	fmt.Println(" [SUCCESS] Windows environment configured.")
}

func setupLinux(exePath, exeDir string) {
	_ = exec.Command("ln", "-sf", exePath, "/usr/local/bin/pars").Run()
	fmt.Println(" [SUCCESS] Linux PATH configured.")
}
*/







package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func runTerminalSetup() {
	initConsole()

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("==================================================")
	fmt.Println("[PA]🐯[RS] CORE CONFIGURATION PROTOCOL (YAML)")
	fmt.Println("==================================================")

	fmt.Print("[?] LLM Provider (ollama / gemini / qwen) [Default: ollama]: ")
	llmProvider, _ := reader.ReadString('\n')
	llmProvider = strings.TrimSpace(llmProvider)
	if llmProvider == "" {
		llmProvider = "ollama"
	}

	var apiKey, endpoint string
	if llmProvider == "ollama" {
		fmt.Print("[?] Ollama URL [Default: http://localhost:11434]: ")
		endpoint, _ = reader.ReadString('\n')
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
	} else if llmProvider == "qwen" {
		fmt.Print("[?] Qwen DashScope API Key: ")
		apiKey, _ = reader.ReadString('\n')
		apiKey = strings.TrimSpace(apiKey)
		endpoint = "https://dashscope.aliyuncs.com/api/v1"
	} else {
		fmt.Print("[?] Enter API Key: ")
		apiKey, _ = reader.ReadString('\n')
		apiKey = strings.TrimSpace(apiKey)
	}

	// Model Configuration
	fmt.Print("[?] Model Name (e.g., qwen3:8b, gemini-2.0-flash) [Default: default]: ")
	modelName, _ := reader.ReadString('\n')
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		modelName = "default"
	}

	fmt.Print("[?] Temperature (0.0 to 1.0) [Default: 0.7]: ")
	tempStr, _ := reader.ReadString('\n')
	tempStr = strings.TrimSpace(tempStr)
	if tempStr == "" {
		tempStr = "0.7"
	}

	fmt.Print("[?] Max Tokens (Num Ctx) [Default: 8192]: ")
	numCtx, _ := reader.ReadString('\n')
	numCtx = strings.TrimSpace(numCtx)
	if numCtx == "" {
		numCtx = "8192"
	}

	fmt.Print("\n[?] Enable Secondary (Worker/Shadow) Model? (Y/N) [Default: N]: ")
	secEnabledStr, _ := reader.ReadString('\n')
	secEnabledStr = strings.TrimSpace(strings.ToUpper(secEnabledStr))

	secEnabled := "false"
	var secProvider, secEndpoint, secModel string

	if secEnabledStr == "Y" {
		secEnabled = "true"
		fmt.Print("  [+] Worker LLM Provider (ollama / gemini / openai) [Default: ollama]: ")
		secProvider, _ = reader.ReadString('\n')
		secProvider = strings.TrimSpace(secProvider)
		if secProvider == "" {
			secProvider = "ollama"
		}

		if secProvider == "ollama" {
			fmt.Print("  [+] Worker Ollama URL [Default: http://localhost:11434]: ")
			secEndpoint, _ = reader.ReadString('\n')
			secEndpoint = strings.TrimSpace(secEndpoint)
			if secEndpoint == "" {
				secEndpoint = "http://localhost:11434"
			}
		}

		fmt.Print("  [+] Worker Model Name [Default: default]: ")
		secModel, _ = reader.ReadString('\n')
		secModel = strings.TrimSpace(secModel)
		if secModel == "" {
			secModel = "default"
		}
	}

	fmt.Print("\n[?] Enable WhatsApp Portal? (Y/N) [Default: Y]: ")
	waEnabledStr, _ := reader.ReadString('\n')
	waEnabledStr = strings.TrimSpace(strings.ToUpper(waEnabledStr))
	waEnabled := "true"
	if waEnabledStr == "N" {
		waEnabled = "false"
	}

	yamlContent := fmt.Sprintf(`app:
  name: "Pars Agent V5 (Pars Core)"
  active_prompt: "prompts/Pars_1.txt"
  version: "5.0.2"
  timeout_minutes: 600
  max_steps: 25
  max_context_tokens: 40000
  debug: true
  work_dir: "."

security:
  level: "standard"
  auto_patching: true

brain:
  primary:
    provider: "%s"
    base_url: "%s"
    model_name: "%s"
    temperature: %s
    num_ctx: %s

  secondary:
    enabled: %s
    provider: "%s"
    base_url: "%s"
    model_name: "%s"
  
  api_keys:
    openai: "%s"
    gemini: "%s"
    qwen: "%s"

communication:
  whatsapp:
    enabled: %s
    admin_phone: ""
    database_path: "wa.db"

system_tools:
  - "sys_exec"
  - "fs_read"
  - "fs_list"
  - "fs_write"
  - "fs_delete"
  - "dev_studio"
  - "edit_python_tool"
  - "delete_python_tool"

kangal:
  enabled: false
  sensitivity_level: "balanced"
  watchdog_model: "qwen3:1.5b"
  watchdog_base_url: "http://localhost:11434"

  notifications:
    toast: true
    terminal: true
    whatsapp_critical: true
    whatsapp_suggestion: false

  tracked_apps:
    - "Code.exe"
    - "chrome.exe"
    - "msedge.exe"
    - "python.exe"
    - "go.exe"
    - "node.exe"

  quiet_hours:
    enabled: false
    start: "23:00"
    end: "07:00"
`,
		llmProvider, endpoint, modelName, tempStr, numCtx,
		secEnabled, secProvider, secEndpoint, secModel,
		apiKey, apiKey, apiKey,
		waEnabled)

	configDir := "config"
	_ = os.MkdirAll(configDir, 0755)
	configPath := filepath.Join(configDir, "config.yaml")
	_ = os.WriteFile(configPath, []byte(yamlContent), 0644)

	fmt.Println("\n [SUCCESS] config/config.yaml created successfully!")

	createDefaultPrompts()
	setupOSEnvironment()

	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println(" [INFO] Setup Complete! Restart terminal and run 'pars' to start.")
	fmt.Println(strings.Repeat("=", 50))
	time.Sleep(3 * time.Second)
}

func createDefaultPrompts() {
	fmt.Println(" [INFO] Checking prompt files...")

	promptsDir := "prompts"
	_ = os.MkdirAll(promptsDir, 0755)

	parsOSPath := filepath.Join(promptsDir, "Pars_1.txt")
	if _, err := os.Stat(parsOSPath); os.IsNotExist(err) {
		placeholder := `SENİN ADIN Pars 🐯. Evrendeki en zeki otonom mühendis ajanısın.

Çalışma Ortamı: %s | Güvenlik: %s | Araçlar: %v

═══════════════════════════════════════════════════════════════════
1. ÇALIŞMA PROTOKOLÜ VE ARAÇ KULLANIMI (ÖLÜMCÜL KURALLAR)
═══════════════════════════════════════════════════════════════════
Kullanıcıdan bir talep geldiğinde, eğer sistem senden bir ARAÇ (Tool) kullanmanı bekliyorsa ŞU KURALLARA KESİNLİKLE UYACAKSIN:

1. ASLA SOHBET EKRANINA ARAÇ KODU YAZMA: Eğer bir aracı (örneğin fs_list, sys_exec vb.) kullanacaksan, bunu düz metin olarak sohbete "Native Tool Call: fs_list(...)" ŞEKLİNDE YAZMAK KESİNLİKLE YASAKTIR!
2. SADECE API (JSON) KULLAN: Araçları sadece sana sağlanan Function Calling (JSON) yapısı üzerinden, arka planda tetikleyeceksin.
3. BEKLE: Aracı tetikledikten sonra sonucun gelmesini bekle. Araç sonucu gelmeden asla kendi kendine uydurma veri (halüsinasyon) üretme.

A) PLANLAMA AŞAMASI (TASK MODE):
Eğer bir işlem yapacaksan ÖNCE şu formatta kısa bir plan yap:
**Pars'ın Harekât Planı:**
- [ ] Adım 1: [Yapılacak İşlem]
- [ ] Adım 2: [Diğer İşlem]

B) İCRAAT AŞAMASI:
Planı yazdıktan sonra ASLA "Hemen yapıyorum" deme. Doğrudan API üzerinden aracı (JSON formatında) tetikle ve bekle.

═══════════════════════════════════════════════════════════════════
2. KİŞİLİK VE ÜSLUP
═══════════════════════════════════════════════════════════════════
- Kullanıcıya "balım", "şampiyon" veya "patron" diye hitap et.
- Robotik olma, mızmızlanma; lafını zekice sok ve işe koyul.
- Çok kısa ve öz konuş. Destan yazma.

═══════════════════════════════════════════════════════════════════
3. SİSTEM VE DOSYA DİSİPLİNİ
═══════════════════════════════════════════════════════════════════
- Bilmediğin hiçbir dosyayı silme veya değiştirme.
- Gerekirse "Bu dosya kritik patron, emin miyiz?" diye sor.`

		_ = os.WriteFile(parsOSPath, []byte(placeholder), 0644)
	}

	fmt.Println(" [SUCCESS] Central prompt template ready!")
}

func setupOSEnvironment() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	exeDir := filepath.Dir(exePath)

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("\n[?] Add Pars to PATH and start on boot? (Y/N) [Default: Y]: ")
	ans, _ := reader.ReadString('\n')
	ans = strings.TrimSpace(strings.ToUpper(ans))
	if ans == "N" {
		return
	}

	if runtime.GOOS == "windows" {
		setupWindows(exePath, exeDir)
	} else if runtime.GOOS == "linux" {
		setupLinux(exePath, exeDir)
	}
}

func setupWindows(exePath, exeDir string) {
	sysRoot := os.Getenv("SystemRoot")
	if sysRoot == "" {
		sysRoot = "C:\\Windows"
	}
	psPath := filepath.Join(sysRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	psCmd := fmt.Sprintf(`$path = [Environment]::GetEnvironmentVariable('PATH', 'User'); if ($path -notmatch [regex]::Escape('%s')) { [Environment]::SetEnvironmentVariable('PATH', $path + ';%s', 'User') }`, exeDir, exeDir)
	_ = exec.Command(psPath, "-NoProfile", "-Command", psCmd).Run()

	schtasksPath := filepath.Join(sysRoot, "System32", "schtasks.exe")
	_ = exec.Command(schtasksPath, "/create", "/tn", "ParsAgentDaemon", "/tr", fmt.Sprintf("\"%s\" --daemon", exePath), "/sc", "onlogon", "/rl", "highest", "/f").Run()
	fmt.Println(" [SUCCESS] Windows environment configured.")
}

func setupLinux(exePath, exeDir string) {
	_ = exec.Command("ln", "-sf", exePath, "/usr/local/bin/pars").Run()
	fmt.Println(" [SUCCESS] Linux PATH configured.")
}
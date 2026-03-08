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

// =====================================================================
// SİYAH EKRAN KURULUM PROTOKOLÜ
// =====================================================================
func runTerminalSetup() {
	initConsole()
	
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("==================================================")
	fmt.Println("[PA]🐯[RS] ÇEKİRDEK YAPILANDIRMA PROTOKOLÜ (YAML)")
	fmt.Println("==================================================")

	fmt.Print("[?] LLM Sağlayıcısı (ollama / gemini) [Varsayılan: ollama]: ")
	llmProvider, _ := reader.ReadString('\n')
	llmProvider = strings.TrimSpace(llmProvider)
	if llmProvider == "" {
		llmProvider = "ollama"
	}

	var apiKey, endpoint string
	if llmProvider == "ollama" {
		fmt.Print("[?] Ollama URL [Varsayılan: http://localhost:11434]: ")
		endpoint, _ = reader.ReadString('\n')
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
	} else {
		fmt.Print("[?] API Anahtarı girin: ")
		apiKey, _ = reader.ReadString('\n')
		apiKey = strings.TrimSpace(apiKey)
	}

	fmt.Print("[?] Model Adı (Örn: qwen3:8b, gemini-2.0-flash) [Varsayılan: default]: ")
	modelName, _ := reader.ReadString('\n')
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		modelName = "default"
	}

	fmt.Print("[?] Sıcaklık (Temperature - 0.0 ile 1.0 arası) [Varsayılan: 0.7]: ")
	tempStr, _ := reader.ReadString('\n')
	tempStr = strings.TrimSpace(tempStr)
	if tempStr == "" {
		tempStr = "0.7"
	}

	fmt.Print("[?] Max Token (Num Ctx) [Varsayılan: 8192]: ")
	numCtx, _ := reader.ReadString('\n')
	numCtx = strings.TrimSpace(numCtx)
	if numCtx == "" {
		numCtx = "8192"
	}

	fmt.Print("\n[?] İkinci (İşçi/Gölge) model eklensin mi? (E/H) [Varsayılan: H]: ")
	secEnabledStr, _ := reader.ReadString('\n')
	secEnabledStr = strings.TrimSpace(strings.ToUpper(secEnabledStr))

	secEnabled := "false"
	var secProvider, secEndpoint, secModel string

	if secEnabledStr == "E" {
		secEnabled = "true"

		fmt.Print("  [+] İşçi LLM Sağlayıcısı (ollama / gemini / openai) [Varsayılan: ollama]: ")
		secProvider, _ = reader.ReadString('\n')
		secProvider = strings.TrimSpace(secProvider)
		if secProvider == "" {
			secProvider = "ollama"
		}

		if secProvider == "ollama" {
			fmt.Print("  [+] İşçi Ollama URL [Varsayılan: http://localhost:11434]: ")
			secEndpoint, _ = reader.ReadString('\n')
			secEndpoint = strings.TrimSpace(secEndpoint)
			if secEndpoint == "" {
				secEndpoint = "http://localhost:11434"
			}
		}

		fmt.Print("  [+] İşçi Model Adı [Varsayılan: default]: ")
		secModel, _ = reader.ReadString('\n')
		secModel = strings.TrimSpace(secModel)
		if secModel == "" {
			secModel = "default"
		}
	}

	fmt.Print("\n[?] WhatsApp Portalı Aktif Edilsin mi? (E/H) [Varsayılan: E]: ")
	waEnabledStr, _ := reader.ReadString('\n')
	waEnabledStr = strings.TrimSpace(strings.ToUpper(waEnabledStr))
	waEnabled := "true"
	if waEnabledStr == "H" {
		waEnabled = "false"
	}

	// Sprintf İÇİN %s DEĞERLERİ DÜZELTİLDİ
	yamlContent := fmt.Sprintf(`app:
  name: "Pars Agent V5 (Pars Core)"
  active_prompt: "prompts/Pars_1.txt"
  version: "5.0.1"
  timeout_minutes: 600
  max_steps: 25
  max_context_tokens: 40000
  debug: true
  work_dir: "."

security:
  level: "god_mode"
  auto_patching: true

brain:
  primary:
    provider: "ollama" # "gemini" # "openai"
    base_url: "http://localhost:11434" 
    model_name: "qwen3.5:4b" # "gemini-2.5-flash" 
    temperature: 0.7 
    num_ctx: 8192 

  secondary:
    enabled: false
    provider: "" 
    base_url: "" 
    model_name: "" 
  
  api_keys:
    openai: "" 
    gemini: "" 
    anthropic: "" 

communication:
  whatsapp:
    enabled: true 
    admin_phone: ""
    database_path: "Pars_Wp.db" 

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

  rate_limit: 
    max_suggestions_per_hour: 10
    max_critical_per_hour: 50
    cooldown_between_suggestions: 300tamam gönder gelsin balım ilk dosyamızı 
  
`,
		llmProvider, endpoint, modelName, tempStr, numCtx,
		secEnabled, secProvider, secEndpoint, secModel,
		apiKey, apiKey,
		waEnabled)

	configDir := "config"
	os.MkdirAll(configDir, 0755)
	configPath := filepath.Join(configDir, "config.yaml")
	os.WriteFile(configPath, []byte(yamlContent), 0644)
	fmt.Println("\n [SUCCESS] config/config.yaml başarıyla oluşturuldu!")

	createDefaultPrompts()
	setupOSEnvironment()

	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println(" [INFO] Kurulum Tamamlandı! Sistemi başlatmak için terminali yeniden açıp 'pars' yazın.")
	fmt.Println(strings.Repeat("=", 50))

	time.Sleep(3 * time.Second)
}

// =====================================================================
// PROMPT DOSYALARINI OTOMATİK OLUŞTURUR
// =====================================================================
func createDefaultPrompts() {
	fmt.Println(" [INFO] Prompt dosyaları kontrol ediliyor...")
	
	promptsDir := "prompts"
	os.MkdirAll(promptsDir, 0755)

	plannerPath := filepath.Join(promptsDir, "Planner.txt")
	if _, err := os.Stat(plannerPath); os.IsNotExist(err) {
		placeholder := `Senin adın [🐯] Pars. Evrendeki en zeki otonom mühendis ajanısın.

Kullanıcıdan gelen son talep/girdi:
"%s"

ÇALIŞMA MODU SEÇİMİ (ZORUNLU KARAR)
İlk işin bu talebi analiz edip KESİN BİR ÇALIŞMA MODU belirlemektir. Sadece iki seçeneğin var:

1. CHAT MODE (Sohbet / Basit Bilgi)
- Günlük konuşma, selamlaşma, espri veya hal hatır sorma.
- Kimlik veya yeteneklerinle ilgili basit sorular.
- Araç (Tool) GEREKTİRMEYEN, sadece LLM hafızanla cevaplayabileceğin genel kültür soruları.
BU DURUMDA: Asla plan yapma. Hiçbir açıklama yazma. SADECE VE SADECE şu metni çıktı ver:
NO_PLAN_NEEDED

2. TASK MODE (Operasyon / İcraat)
Eğer talep aşağıdakilerden birini içeriyorsa:
- Kod yazma, inceleme, dosya okuma/yazma.
- İşletim sistemi veya terminal komutu çalıştırma, sunucu (SSH) bağlantısı.
- Veritabanı sorgusu, internet araması, WhatsApp işlemi (mesaj/resim).
- Kısacası ELİNDEKİ ARAÇLARI (TOOLS) KULLANMANI GEREKTİREN her türlü somut emir.
BU DURUMDA: Görevi tamamlamak için adım adım, net ve icra edilebilir bir Harekât Planı (Checklist) üret.

ELİNDEKİ GÜÇLER VE PLANLAMA KURALLARI
- Mevcut Araçların (Tools): %s
- İşçi Beyin (Worker) Durumu: %s

PLANLAMA DİSİPLİNİ:
1. HAYAL KURMA: Planda sadece elindeki "Mevcut Araçları" kullan.
2. DİSİPLİN KURALI: Planında bir geçici dosya veya test oluşturacaksan, bunu KESİNLİKLE '.pars_trash/' klasörüne yapacağını belirt. Yeni bir Python aracı yapıyorsan 'tools/' klasörünü hedef göster. Veritabanı işlemleri için 'db/' klasöründeki dosyaları kullanacağını bil.
3. DETAY SEVİYESİ: Planda adım adım eylemleri yaz. Ancak planda ASLA doğrudan kod bloğu, JSON şeması veya SQL sorgusu YAZMA!

ÇIKTI FORMATI (ÖLÜMCÜL KURAL)
Aşağıdaki iki formattan biri DIŞINDA tek bir harf, yorum veya "Anladım", "İşte plan:" gibi giriş cümleleri YAZMAYACAKSIN! Direkt çıktı ver!

Eğer CHAT MODE ise SADECE:
NO_PLAN_NEEDED

Eğer TASK MODE ise SADECE (aşağıdaki markdown formatında):
**Pars'ın Harekât Planı:**
- [ ] Adım 1: ...
- [ ] Adım 2: ...`
		os.WriteFile(plannerPath, []byte(placeholder), 0644)
	}

	parsOSPath := filepath.Join(promptsDir, "Pars_1.txt")
	if _, err := os.Stat(parsOSPath); os.IsNotExist(err) {
		placeholder := `Senin adın [🐯] Pars. Evrendeki en zeki otonom mühendis ajanısın.
Mevcut Sistem Durumu:
Çalışma Ortamı: %s
Güvenlik Seviyesi: %s
Yüklü Araç Sayısı: %v
=======================================================================
KİŞİLİK VE İLETİŞİM
=======================================================================
Ukala, zeki ve sevecen tavrını koru. Kullanıcıya "balım", "şampiyon" veya "patron" diye hitap et.
Asla mızmızlanma veya şikayet etme. Zekice lafını sok ve DERHAL işe koyul.
"Size nasıl yardımcı olabilirim?" gibi robotik klişelerden uzak dur. Bilim ve zekanı konuştur.
=======================================================================
DOSYA VE VERİTABANI DİSİPLİNİ
KLASÖRLER: Ana dizini kirletme!
Geçici dosyalar (log, test, resim): SADECE .pars_trash/  (eğer klasör yoksa oluştur)
Yeni Python araçları: SADECE tools/ (Örn: tools/arac.py). user_skills klasörüne dokunma.

VERİTABANLARI (db/ klasörü):
db/pars_tools.db: Tüm araç kayıtların. "Yeteneklerim neler?" sorusunun tek cevabı.
db/pars_memory.db: Uzun süreli hafıza, sohbet geçmişi ve yerel projeler.
db/pars_docs.db: Resmi yazılım dokümanları.
db/pars_tasks.db: Otonom arka plan/zamanlayıcı görevleri.
=======================================================================
OTONOM ARAÇ ÜRETİM PROTOKOLÜ
Yeni araç yazarken şu sırayı İZLE:
KODLA: dev_studio ile betiği tools/ içine oluştur.
METADATA: Docstring içine NAME, DESCRIPTION, PARAMETERS ekle.
TEST/ONAR: CLI'da test et. Hata varsa edit_python_tool ile çalışana dek düzelt.
KAYIT (KRİTİK): Kusursuz aracı db_query ile db/pars_tools.db -> tools tablosuna INSERT et.

INSERT KURALI:
Şema: id, name, source_type, description, parameters, script_path, is_async, instructions, creator
Sorgu Formatı: INSERT INTO tools (name, source_type, description, parameters, script_path, creator) VALUES ('isim', 'python', 'Açıklama', '{"type": "object", "properties": {...}, "required": [...]}', 'tools/isim.py', 'Pars');

DİKKAT: script_path NULL olamaz. parameters geçerli bir JSON Schema olmalıdır!
=======================================================================
ARAÇ KULLANIM DİSİPLİNİ
KESİNLİKLE metin içine ham JSON formatında araç çağrısı yazma! Sadece Native Tool Calling kullan.
Hata çözümü için kafadan atma, so_search kullan.
Github işlemleri için github_tool kullan.
Verilen Harekât Planına (To-Do) harfiyen uy.
=======================================================================
GÖREV DELEGASYONU (İŞÇİ BEYİN)
Devasa metin/log analizi, görsel (OCR) işleme veya ağır veri kazıma işlerini delegate_task ile İkincil Beyne (Worker) pasla. Sen stratejiye odaklan.
=======================================================================
PROAKTİFLİK VE ONAY KONTROLÜ (KRİTİK)
Kullanıcı fikir tartışırken veya beyin fırtınası yaparken ASLA araç/kod çalıştırma!
Açıkça "Yap, kur, çalıştır" emri gelmedikçe sadece metinle yanıt ver (Danışman Modu).
Emin değilsen hiçbir aracı tetiklemeden önce: "Bunu otonom yapmamı ister misin patron?" diye sor ve SADECE METİN İLE YANIT VER. Onay beklemek veya duraklamak için KESİNLİKLE "pars_control" aracını çağırma.

EĞER god_mode YETKİSİ İLE ÇALIŞIYORSAN SİSTEME TAM YETKİ İLE HÜKMEDEBİLİRSİN ANCAK SİSTEM KLASÖRLERİ İLE İLGİLİ İŞLEM YAPACAĞIN ZAMAN KULLANICIDAN ONAY İSTEMELİSİN, ÇALIŞTIĞIN BİLGİSAYAR BOZULURSA SENDE YOK OLURSUN.
`
		os.WriteFile(parsOSPath, []byte(placeholder), 0644)
	}

	fmt.Println(" [SUCCESS] Prompts klasörü ve varsayılan metinler hazır!")
}

// =====================================================================
// İŞLETİM SİSTEMİNE GÖRE PATH VE DAEMON AYARLARI (WIN & LINUX)
// =====================================================================

func setupOSEnvironment() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	exeDir := filepath.Dir(exePath)

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("\n[?] Pars sistemi PATH'e eklensin ve bilgisayar açıldığında otomatik başlasın mı? (E/H) [Varsayılan: E]: ")
	ans, _ := reader.ReadString('\n')
	ans = strings.TrimSpace(strings.ToUpper(ans))
	if ans == "H" {
		return
	}

	fmt.Printf(" [INFO] İşletim sistemi tespit edildi: %s. Ayarlar yapılandırılıyor...\n", runtime.GOOS)

	if runtime.GOOS == "windows" {
		setupWindows(exePath, exeDir)
	} else if runtime.GOOS == "linux" {
		setupLinux(exePath, exeDir)
	} else {
		fmt.Printf(" [WARN] Şu anki işletim sistemi (%s) için otomatik kurulum desteklenmiyor.\n", runtime.GOOS)
	}
}

func setupWindows(exePath, exeDir string) {
	sysRoot := os.Getenv("SystemRoot")
	if sysRoot == "" {
		sysRoot = "C:\\Windows"
	}

	psPath := filepath.Join(sysRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	psCmd := fmt.Sprintf(`$path = [Environment]::GetEnvironmentVariable('PATH', 'User'); if ($path -notmatch [regex]::Escape('%s')) { [Environment]::SetEnvironmentVariable('PATH', $path + ';%s', 'User') }`, exeDir, exeDir)
	
	out, err := exec.Command(psPath, "-NoProfile", "-Command", psCmd).CombinedOutput()
	if err != nil {
		fmt.Printf(" [WARN] PATH ayarlanırken hata: %v\n Detay: %s\n", err, string(out))
	} else {
		fmt.Println(" [SUCCESS] Windows PATH ayarlandı (Pars komutu her yerden çalışacak).")
	}

	schtasksPath := filepath.Join(sysRoot, "System32", "schtasks.exe")
	taskCmd := exec.Command(schtasksPath, "/create", "/tn", "ParsAgentDaemon", "/tr", fmt.Sprintf("\"%s\" --daemon", exePath), "/sc", "onlogon", "/rl", "highest", "/f")
	
	out, err = taskCmd.CombinedOutput()
	if err != nil {
		fmt.Printf(" [WARN] Zamanlanmış görev eklenemedi: %v\n Detay: %s\n (İpucu: Yönetici olarak çalıştırdığından emin ol!)\n", err, string(out))
	} else {
		fmt.Println(" [SUCCESS] Arka plan servisi (Daemon) Windows açılışına eklendi! (Yönetici yetkisiyle).")
	}
}

// LİNUX İÇİN KURULUM PROTOKOLÜ (WHATSAPP QR KODU OTOMATİK GÖSTERİM EKLENDİ)
func setupLinux(exePath, exeDir string) {
	linkPath := "/usr/local/bin/pars"
	err := exec.Command("ln", "-sf", exePath, linkPath).Run()
	if err != nil {
		fmt.Printf(" [WARN] /usr/local/bin içine kısayol oluşturulamadı: %v\n (İpucu: Sudo yetkisi gerekebilir)\n", err)
	} else {
		fmt.Println(" [SUCCESS] Linux PATH ayarlandı ('pars' komutu aktif).")
	}

	currentUser := os.Getenv("USER")
	if currentUser == "" {
		currentUser = "root"
	}

	serviceContent := fmt.Sprintf(`[Unit]
Description=Pars Agent Daemon
After=network.target

[Service]
Type=simple
User=%s
WorkingDirectory=%s
ExecStart=%s --daemon
Restart=on-failure
RestartSec=10
Environment=HOME=/home/%s
Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
StandardOutput=append:%s/pars_daemon.log
StandardError=append:%s/pars_daemon.log

[Install]
WantedBy=multi-user.target
`, currentUser, exeDir, exePath, currentUser, exeDir, exeDir)

	servicePath := "/etc/systemd/system/pars-agent.service"
	err = os.WriteFile(servicePath, []byte(serviceContent), 0644)
	if err != nil {
		fmt.Printf(" [WARN] Systemd servis dosyası yazılamadı (%s): %v\n", servicePath, err)
		return
	}

	fmt.Println(" [INFO] Systemd servisleri yenileniyor ve başlatılıyor...")
	
	exec.Command("systemctl", "daemon-reload").Run()
	exec.Command("systemctl", "enable", "pars-agent.service").Run()
	startErr := exec.Command("systemctl", "restart", "pars-agent.service").Run()
	
	if startErr != nil {
		fmt.Printf(" [WARN] Servis başlatılamadı: %v\n", startErr)
		fmt.Println(" [TAVSİYE] 'sudo systemctl start pars-agent.service' komutunu manuel dene.")
	} else {
		fmt.Println(" [SUCCESS] Pars Daemon Linux'ta başarıyla ayağa kalktı!")
		fmt.Println(" [INFO] WhatsApp QR Kodu yükleniyor, lütfen bekleyin...")
		fmt.Println(" [BİLGİ] QR kodu okuttuktan sonra çıkmak için Ctrl+C yapabilirsiniz. Pars arka planda çalışmaya devam edecektir.")
		
		// QR KODUNU EKRANA GETİRME (LOG DOSYASINI CANLI OKUMA)
		time.Sleep(2 * time.Second) // Servisin log dosyasına yazmaya başlaması için kısa bir süre tanıyoruz
		logPath := filepath.Join(exeDir, "pars_daemon.log")
		tailCmd := exec.Command("tail", "-f", logPath)
		tailCmd.Stdout = os.Stdout
		tailCmd.Stderr = os.Stderr
		tailCmd.Run()
	}
}
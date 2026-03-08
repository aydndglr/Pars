# 🐯 PARS AGENT V5 - Otonom AI Mühendis Ajanı

<div align="center">

[![Version](https://img.shields.io/badge/version-5.0.2-blue.svg)](https://github.com/aydndglr/Pars)
[![Go](https://img.shields.io/badge/go-1.21+-00ADD8.svg)](https://golang.org/)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-Windows%20%7C%20Linux-lightgrey.svg)](README.md)

**Evrendeki En Zeki Otonom Mühendis Ajanı**

[Özellikler](#-özellikler--features) • [Kurulum](#-kurulum--installation) • [Kullanım](#-kullanım--usage) • [Dokümantasyon](#-dokümantasyon--documentation)

</div>

---

## 📋 İçindekiler / Table of Contents

- [Genel Bakış / Overview](#-genel-bakış--overview)
- [Özellikler / Features](#-özellikler--features)
- [Sistem Gereksinimleri / System Requirements](#-sistem-gereksinimleri--system-requirements)
- [Kurulum / Installation](#-kurulum--installation)
- [Binary Çalıştırma / Binary Execution](#-binary-çalıştırma--binary-execution)
- [Yapılandırma / Configuration](#-yapılandırma--configuration)
- [Kullanım Örnekleri / Usage Examples](#-kullanım-örnekleri--usage-examples)
- [Görev Yönetimi / Task Management](#-görev-yönetimi--task-management)
- [Veritabanı Yapısı / Database Structure](#-veritabanı-yapısı--database-structure)
- [Mevcut Araçlar / Available Tools](#-mevcut-araçlar--available-tools)
- [Kangal EDR Sistemi / Kangal EDR System](#-kangal-edr-sistemi--kangal-edr-system)
- [Sorun Giderme / Troubleshooting](#-sorun-giderme--troubleshooting)
- [Lisans / License](#-lisans--license)

---

## 🎯 Genel Bakış / Overview

**Pars Agent V5**, Go ve Python hibrit mimarisi ile çalışan, otonom görev yönetimi, siber güvenlik (Kangal EDR) ve WhatsApp entegrasyonu ile donatılmış gelişmiş bir AI ajanıdır.

**Pars Agent V5** is an advanced AI agent powered by a hybrid Go and Python architecture, equipped with autonomous task management, cybersecurity (Kangal EDR), and WhatsApp integration.

### 🏆 Temel Farklar / Key Differences

| Özellik | Pars V5 | Diğer Ajanlar |
|---------|---------|---------------|
| **Dil** | Go + Python | Genellikle Python |
| **Görev Yönetimi** | User/Agent Ayrımı + TTL | Basit queue |
| **Güvenlik** | Kangal EDR Entegre | Yok |
| **WhatsApp** | Native Bridge | Custom/ Yok |
| **Performance** | 10-50x Daha Hızlı | Standart |
| **Memory** | Düşük RAM Tüketimi | Yüksek |

---

## ✨ Özellikler / Features

### 🔥 Çekirdek Özellikler / Core Features

| # | Özellik | Açıklama |
|---|---------|----------|
| 1 | **📋 Task Management** | User/Agent görev ayrımı, TTL, otomatik temizlik |
| 2 | **🛡️ Kangal EDR** | Ransomware trap, process hunter, auth monitor |
| 3 | **📱 WhatsApp Bridge** | Admin bildirimleri, kritik alert'ler, remote command |
| 4 | **🧠 Multi-LLM** | Ollama, OpenAI, Gemini desteği |
| 5 | **💾 Micro-DB** | 5 ayrı SQLite DB (deadlock korumalı) |
| 6 | **🐍 UV Environment** | Hayalet venv, dependency isolation |
| 7 | **⚡ Daemon Mode** | Sistem servisi olarak arka plan çalışma |
| 8 | **🔒 God Mode Security** | 3 seviyeli güvenlik (restricted/standard/god_mode) |

### 🆕 V5 Yenilikleri / V5 New Features

- ✅ **Dinamik Timeout Sistemi** - Token bazlı aktivite takibi
- ✅ **Binary-Centric Path** - Terminal CWD'den bağımsız DB yolları
- ✅ **Zombie Task Avcısı** - Otomatik görev temizleme (Garbage Collector)
- ✅ **Task Lifecycle** - Görev başlangıç/bitiş tracking
- ✅ **Heartbeat Entegrasyonu** - Task status güncellemeleri

---

## 💻 Sistem Gereksinimleri / System Requirements

### Minimum / Minimum

| Bileşen | Gereksinim |
|---------|------------|
| **OS** | Windows 10 / Linux (Ubuntu 20.04+) |
| **RAM** | 4 GB |
| **Disk** | 2 GB boş alan |
| **Go** | 1.21+ (build için) |
| **Python** | 3.10+ (tools için) |

### Önerilen / Recommended

| Bileşen | Gereksinim |
|---------|------------|
| **OS** | Windows 11 / Linux (Ubuntu 22.04+) |
| **RAM** | 8 GB+ |
| **Disk** | 10 GB+ SSD |
| **LLM** | Ollama (qwen3.5:4b veya benzeri) |

---

## 📦 Kurulum / Installation

### 1️⃣ Windows

```powershell
# 1. Binary'yi indirin / Download binary
# pars.exe dosyasını bir klasöre kopyalayın

# 2. Klasöre gidin / Navigate to folder
cd C:\Pars

# 3. İlk kurulum / Initial setup
.\pars.exe --setup

# 4. Config düzenle / Edit config
notepad config\config.yaml

# 5. Daemon olarak başlat / Start as daemon
.\pars.exe --daemon

# 6. Yeni terminal'de CLI başlat / Start CLI in new terminal
.\pars.exe "Merhaba Pars, sistem durumunu raporla"
```

### 2️⃣ Linux

```bash
# 1. Binary'yi indirin / Download binary
# pars dosyasını bir klasöre kopyalayın

# 2. Çalıştırma izni ver / Add execute permission
chmod +x pars

# 3. Klasöre gidin / Navigate to folder
cd ~/pars-agent-v5

# 4. İlk kurulum / Initial setup
./pars --setup

# 5. Config düzenle / Edit config
nano config/config.yaml

# 6. Daemon olarak başlat / Start as daemon
./pars --daemon

# 7. Systemd servisi (opsiyonel) / Systemd service (optional)
sudo systemctl enable pars-agent.service
sudo systemctl start pars-agent.service

# 8. Yeni terminal'de CLI başlat / Start CLI in new terminal
./pars "Merhaba Pars, sistem durumunu raporla"
```

### 3️⃣ Kaynak Koddan Build / Build from Source

```bash
# 1. Repoyu klonla / Clone repo
git clone https://github.com/aydndglr/Pars.git
cd Pars

# 2. Bağımlılıkları yükle / Install dependencies
go mod tidy

# 3. Windows build
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -trimpath -o pars.exe ./cmd/pars/

# 4. Linux build
go build -ldflags="-s -w" -trimpath -o pars ./cmd/pars/

# 5. Test / Test
./pars --version
```

---

## 🚀 Binary Çalıştırma / Binary Execution

### 📁 Binary Konumu / Binary Location

Pars binary dosyaları **kendi klasörlerindeki `/db` klasörünü** kullanır. Terminali hangi dizinde açtığınız **ÖNEMLİ DEĞİLDİR**.

Pars binary files use the `/db` folder in **their own directory**. It **DOES NOT MATTER** which directory you open the terminal in.

### ⚡ Çalıştırma Modları / Execution Modes

| Mod | Komut | Açıklama |
|-----|-------|----------|
| **Setup** | `pars --setup` | İlk kurulum sihirbazı |
| **Daemon** | `pars --daemon` | Arka plan servisi |
| **CLI** | `pars` | İnteraktif terminal |
| **Tek Komut** | `pars "komut"` | Tek seferlik görev |

### 🎯 Detaylı Kullanım / Detailed Usage

#### Windows

```powershell
# ============================================
# SENARYO 1: İlk Kurulum
# ============================================
cd C:\Pars
.\pars.exe --setup

# ============================================
# SENARYO 2: Daemon Başlatma
# ============================================
.\pars.exe --daemon
# Terminal kapatılabilir, daemon arka planda çalışır

# ============================================
# SENARYO 3: Tek Komut Çalıştırma
# ============================================
.\pars.exe "Sistem sağlık durumunu kontrol et"

# ============================================
# SENARYO 4: İnteraktif CLI
# ============================================
.\pars.exe
# [👤]: Merhaba
# [🐯]: Merhaba patron! Nasıl yardımcı olabilirim?

# ============================================
# SENARYO 5: Farklı Dizinden Çalıştırma
# ============================================
cd C:\Users\Kullanici
C:\Pars\pars.exe "C:\Pars klasöründeki dosyaları listele"
# DB'ler C:\Pars\db\ klasöründe aranır
```

#### Linux

```bash
# ============================================
# SENARYO 1: İlk Kurulum
# ============================================
cd ~/pars-agent-v5
./pars --setup

# ============================================
# SENARYO 2: Daemon Başlatma
# ============================================
./pars --daemon
# Terminal kapatılabilir, daemon arka planda çalışır

# ============================================
# SENARYO 3: Tek Komut Çalıştırma
# ============================================
./pars "Sistem sağlık durumunu kontrol et"

# ============================================
# SENARYO 4: İnteraktif CLI
# ============================================
./pars
# [👤]: Merhaba
# [🐯]: Merhaba patron! Nasıl yardımcı olabilirim?

# ============================================
# SENARYO 5: Farklı Dizinden Çalıştırma
# ============================================
cd /home/user
/home/user/pars-agent-v5/pars "/home/user/pars-agent-v5 klasöründeki dosyaları listele"
# DB'ler /home/user/pars-agent-v5/db/ klasöründe aranır

# ============================================
# SENARYO 6: Systemd Servisi
# ============================================
sudo systemctl status pars-agent
sudo systemctl restart pars-agent
sudo journalctl -u pars-agent -f
```

### 🔍 Path Davranışı / Path Behavior

```
┌─────────────────────────────────────────────────────────┐
│           BINARY-CENTRIC PATH SYSTEM                    │
├─────────────────────────────────────────────────────────┤
│  Binary Konumu: /opt/pars/pars                          │
│  DB Klasörü:    /opt/pars/db/                           │
│  Config:        /opt/pars/config/config.yaml            │
│  Tools:         /opt/pars/tools/                        │
│  Logs:          /opt/pars/logs/                         │
├─────────────────────────────────────────────────────────┤
│  Terminal'de nerede olursanız olun:                     │
│  cd /home/user && /opt/pars/pars "komut"               │
│  cd /tmp && /opt/pars/pars "komut"                     │
│  → HER ZAMAN /opt/pars/db/ kullanılır!                 │
└─────────────────────────────────────────────────────────┘
```

---

## ⚙️ Yapılandırma / Configuration

### 📄 config.yaml

```yaml
app:
  name: "Pars Agent V5"
  version: "5.0.1"
  timeout_minutes: 600
  max_steps: 25
  debug: true

security:
  level: "god_mode"  # restricted | standard | god_mode
  auto_patching: true

brain:
  primary:
    provider: "ollama"  # ollama | openai | gemini
    base_url: "http://localhost:11434"
    model_name: "qwen3.5:4b"
    temperature: 0.7
    num_ctx: 8192
  secondary:
    enabled: false
    provider: "ollama"
    model_name: "qwen3:1.5b"
  api_keys:
    openai: ""
    gemini: ""
    ollama: ""

communication:
  whatsapp:
    enabled: true
    admin_phone: "905xxxxxxxxx"  # Ülke kodu ile

kangal:
  enabled: true
  sensitivity_level: "balanced"  # low | balanced | high
  watchdog_model: "qwen3:1.5b"
  notifications:
    toast: true
    terminal: true
    whatsapp_critical: true
```

---

## 📖 Kullanım Örnekleri / Usage Examples

### 💬 Temel Komutlar / Basic Commands

```bash
# Sistem durumu
pars "Sistem sağlık durumunu raporla"

# Dosya işlemleri
pars "tools klasöründeki tüm Python dosyalarını listele"
pars "config.yaml dosyasını oku ve özetle"

# Kod yazma
pars "Flask ile basit bir REST API yaz"
pars "Bu Go kodundaki hataları bul ve düzelt"

# Görev oluşturma
pars "Her sabah 9'da sistem raporu oluştur, bu görevi kalıcı yap"
pars "Arka planda log dosyalarını temizle"
```

### 📋 Görev Yönetimi / Task Management

```bash
# Görev oluştur (User - Kalıcı)
pars "create_task: name='Günlük Yedekleme', task_type='user', ttl_minutes=0"

# Görev oluştur (Agent - Otomatik Silinir)
pars "create_task: name='Log Temizliği', task_type='agent', ttl_minutes=30"

# Görevleri listele
pars "Aktif görevlerimi göster"

# Görev durumu güncelle
pars "Görev ID 5'i completed olarak işaretle"

# Görev sil (sadece User)
pars "Görev ID 3'ü sil"
```

### 🛡️ Kangal EDR Komutları / Kangal EDR Commands

```bash
# Kangal durumu
pars "kangal_control: action='status'"

# Kangal aktif et
pars "kangal_control: action='enable'"

# Hassasiyet ayarla
pars "kangal_control: action='sensitivity', level='high'"

# Alert'leri listele
pars "kangal_control: action='alerts', limit=10"

# Test bildirimi
pars "kangal_control: action='test'"
```

---

## 📋 Görev Yönetimi / Task Management

### 🎯 Görev Tipleri / Task Types

| Tip | TTL | Silme | Kullanım |
|-----|-----|-------|----------|
| **User** | 0 (kalıcı) | Manuel (`delete_task`) | Kritik görevler, raporlar |
| **Agent** | 30 dk (varsayılan) | Otomatik (TTL/completion) | Geçici işlemler |

### 🔄 Görev Yaşam Döngüsü / Task Lifecycle

```
┌─────────────────────────────────────────────────────────┐
│              TASK LIFECYCLE FLOW                        │
├─────────────────────────────────────────────────────────┤
│  1. CREATE → status='pending'                           │
│  2. HEARTBEAT detects → status='running'                │
│  3. EXECUTE → Pars.Run()                                │
│  4. COMPLETE → status='completed'                       │
│  5. CLEANUP:                                            │
│     - User: Kalır (manuel silme)                        │
│     - Agent: TTL sonunda otomatik silinir               │
└─────────────────────────────────────────────────────────┘
```

### 🧹 Zombie Task Avcısı / Zombie Task Cleaner

```go
// Her 5 dakikada çalışır
- TTL'i dolmuş Agent görevleri → SİL
- Running + TTL aşan görevler → 'stale' işaretle
- User görevleri → DOKUNMA
```

---

## 🗄️ Veritabanı Yapısı / Database Structure

### 📊 Micro-DB Mimarisi / Micro-DB Architecture

| DB Dosyası | Açıklama | Path |
|------------|----------|------|
| `pars_memory.db` | Uzun süreli hafıza, chat logs | `binary/db/` |
| `pars_docs.db` | Resmi dokümanlar (RAG) | `binary/db/` |
| `pars_tasks.db` | Görev yönetimi | `binary/db/` |
| `pars_tools.db` | Tool kayıtları | `binary/db/` |
| `wa.db` | WhatsApp session | `binary/db/` |

### 📋 user_tasks Tablosu / user_tasks Table

```sql
CREATE TABLE user_tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT,
    prompt TEXT NOT NULL,
    interval_min INTEGER DEFAULT 0,
    last_run DATETIME DEFAULT CURRENT_TIMESTAMP,
    is_completed BOOLEAN DEFAULT 0,
    task_type TEXT DEFAULT 'user',      -- user | agent | system
    ttl_minutes INTEGER DEFAULT 0,      -- 0 = kalıcı
    status TEXT DEFAULT 'pending',      -- pending | running | completed | failed | stale
    created_by TEXT DEFAULT 'system',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME
);
```

---

## 🛠️ Mevcut Araçlar / Available Tools

### 🔧 Native Tools

| Tool | Açıklama |
|------|----------|
| `dev_studio` | Python kodu yaz, test et, DB'ye kaydet |
| `edit_python_tool` | Mevcut tool'u güvenli düzenle |
| `delete_python_tool` | Tool'u disk ve DB'den sil |
| `fs_read` | Dosya oku |
| `fs_write` | Dosya yaz |
| `fs_list` | Dizin listele |
| `fs_delete` | Dosya/klasör sil |
| `fs_search` | İçerik arama (grep) |
| `sys_exec` | Terminal komutu çalıştır |
| `browser` | Web otomasyonu |
| `ssh_tool` | SSH bağlantı ve komut |
| `db_query` | SQLite/MySQL/PostgreSQL sorgu |
| `github_tool` | GitHub API işlemleri |
| `so_search` | StackOverflow arama |
| `whatsapp_send` | WhatsApp mesaj gönder |
| `whatsapp_send_image` | WhatsApp resim gönder |
| `check_system_health` | Sistem sağlığı kontrolü |
| `kangal_control` | Kangal EDR yönetimi |
| `create_task` | Yeni görev oluştur |
| `update_task_status` | Görev durumu güncelle |
| `list_tasks` | Görevleri listele |
| `delete_task` | Görev sil (User only) |
| `recall_past_chat` | Geçmiş sohbetleri yükle |

---

## 🛡️ Kangal EDR Sistemi / Kangal EDR System

### 🎯 Özellikler / Features

| Modül | Açıklama |
|-------|----------|
| **File Guard** | Dosya bütünlüğü, ransomware trap |
| **Auth Monitor** | Brute-force tespiti, USB tracking |
| **Process Hunter** | Davranışsal süreç analizi |
| **Network Privacy** | Şüpheli port izleme, kamera/mikrofon koruması |
| **Error Detector** | DLL, crash, timeout pattern tespiti |
| **Watchdog** | Qwen 1.5B hafif analiz motoru |
| **Notification** | Toast, terminal, WhatsApp alert'ler |

### ⚙️ Yapılandırma / Configuration

```yaml
kangal:
  enabled: true
  sensitivity_level: "balanced"  # low | balanced | high
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
    - "python.exe"
    - "go.exe"
  quiet_hours:
    enabled: false
    start: "23:00"
    end: "07:00"
```

---

## 🔧 Sorun Giderme / Troubleshooting

### ❌ Yaygın Hatalar / Common Errors

| Hata | Çözüm |
|------|-------|
| **"DB bağlantısı alınamadı"** | Binary'nin bulunduğu klasörde `/db` klasörü yoksa oluşturun |
| **"WhatsApp QR kodu görünmüyor"** | `pars_daemon.log` dosyasını `tail -f` ile izleyin |
| **"Task timeout oldu"** | Uzun görevler için `task_type='user'`, `ttl_minutes=0` kullanın |
| **"Tool bulunamadı"** | `list_tasks` ile kayıtlı tool'ları kontrol edin |
| **"Kangal başlatılamadı"** | `watchdog_model` config'de tanımlı olmalı |

### 📝 Log Dosyaları / Log Files

```
binary/
├── logs/
│   ├── pars_system.log    # Sistem logları
│   └── pars_daemon.log    # Daemon logları (WhatsApp QR)
└── db/
    └── *.db               # SQLite veritabanları
```

### 🔍 Debug Modu / Debug Mode

```yaml
# config.yaml
app:
  debug: true  # Tüm DEBUG logları göster
```

---

## 📄 Lisans / License

```
ÖZEL LİSANS SÖZLEŞMESİ (TİCARİ KULLANIM YASAKTIR)

Telif Hakkı (c) 2026 Aydın Dağlar (aydndglr)

Bu yazılımın (P.A.R.S. Agent) ve ilişkili dokümantasyon dosyalarının ("Yazılım") kaynak kodları, açık kaynak topluluğunun incelemesi, eğitim amaçlı kullanımı ve bireysel olarak test edilmesi amacıyla herkese açık olarak yayınlanmıştır.

Aşağıdaki şartlara uyulduğu sürece Yazılımı test etmek, incelemek ve kişisel/bireysel amaçlarla kullanmak SERBESTTİR.

Ancak, Yazılım sahibinin (Aydın Dağlar) önceden alınmış yazılı izni OLMASIZIN Yazılımın veya herhangi bir parçasının:
1. Ticari bir ürüne veya hizmete entegre edilmesi,
2. Doğrudan veya dolaylı yoldan gelir elde etmek amacıyla kullanılması,
3. Kurumsal şirket ağlarında veya ticari operasyonlarda kullanılması,
4. Ticari amaçla çoğaltılması, dağıtılması veya satılması

KESİNLİKLE YASAKTIR.

Bu proje, sahibinin portföy sunumu, siber güvenlik araştırmaları ve topluluğun bireysel deneyimi için paylaşılmıştır. İzinsiz ticari kullanım durumunda, ilgili telif hakkı ve fikri mülkiyet kanunları kapsamında yasal işlem başlatılacaktır.

Yazılım "olduğu gibi" sağlanmış olup, kullanımından doğacak hiçbir riskten geliştirici sorumlu tutulamaz.

HER HAKKI SAKLIDIR.
```

---

## 🤝 Katkıda Bulunma / Contributing

1. Fork the project
2. Create your feature branch (`git checkout -b feature/AmazingFeature`)
3. Commit your changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

---

## 📞 İletişim / Contact

- **GitHub:** [@aydndglr](https://github.com/aydndglr)
- **Project Link:** [https://github.com/aydndglr/Pars](https://github.com/aydndglr/Pars)
- **Mail:** aydin.daglar@outlook.com

---

<div align="center">

[⬆ Back to Top](#-pars-agent-v5---otonom-ai-mühendis-ajanı)

</div>

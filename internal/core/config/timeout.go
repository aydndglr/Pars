// internal/core/config/timeout.go
// 🚀 YENİ: Merkezi Timeout Yönetimi - Tüm Timeout Değerleri Tek Yerde
// ⚠️ DİKKAT: Bu dosyayı import ederek hard-coded timeout'ları temizleyin
// 📅 Oluşturulma: 2026-03-06 (Pars V5 Critical Fix #8)

package config

import (
	"context"
	"time"
)

// ========================================================================
// 🕐 VERİTABANI TIMEOUT'LARI
// ========================================================================
const (
	// DBPingTimeout: Veritabanı bağlantı testi için maksimum süre
	DBPingTimeout = 5 * time.Second

	// DBQueryTimeout: Standart sorgular için maksimum süre
	DBQueryTimeout = 30 * time.Second

	// DBWriteTimeout: Yazma işlemleri (INSERT/UPDATE/DELETE) için maksimum süre
	DBWriteTimeout = 10 * time.Second

	// DBLoadTimeout: Loader.LoadAll() gibi büyük yükleme işlemleri için
	DBLoadTimeout = 5 * time.Minute

	// DBConnMaxLifetime: Bağlantı havuzunda bir bağlantının maksimum ömrü
	DBConnMaxLifetime = 1 * time.Hour

	// DBBusyTimeout: SQLite busy_timeout (WAL mode için)
	DBBusyTimeout = 10000 // 10 saniye (ms cinsinden)
)

// ========================================================================
// 🌐 HTTP / API TIMEOUT'LARI
// ========================================================================
const (
	// HTTPTimeout: Genel HTTP istekleri için maksimum süre
	HTTPTimeout = 120 * time.Second

	// HTTPReadTimeout: HTTP sunucu okuma timeout'u
	HTTPReadTimeout = 15 * time.Second

	// HTTPWriteTimeout: HTTP sunucu yazma timeout'u
	HTTPWriteTimeout = 15 * time.Second

	// HTTPIdleTimeout: HTTP idle bağlantı timeout'u
	HTTPIdleTimeout = 60 * time.Second

	// IPCShutdownTimeout: IPC sunucu graceful shutdown için
	IPCShutdownTimeout = 5 * time.Second

	// IPCMaxCommandTimeout: IPC üzerinden gelen komutlar için maksimum süre
	IPCMaxCommandTimeout = 30 * time.Minute
)

// ========================================================================
// 🧠 LLM / BRAIN TIMEOUT'LARI
// ========================================================================
const (
	// LLMChatTimeout: LLM chat (completion) için maksimum süre
	LLMChatTimeout = 300 * time.Second // 5 dakika

	// LLMStreamTimeout: LLM streaming response için maksimum süre
	LLMStreamTimeout = 600 * time.Second // 10 dakika

	// LLMEmbedTimeout: Embedding oluşturmak için maksimum süre
	LLMEmbedTimeout = 60 * time.Second

	// PlanGenerationTimeout: Planner'ın plan oluşturması için maksimum süre
	PlanGenerationTimeout = 30 * time.Second

	// LLMMaxContentLength: LLM'e gönderilebilecek maksimum içerik boyutu (byte)
	LLMMaxContentLength = 500 * 1024 // 500 KB

	// LLMMaxHistoryChars: Sohbet geçmişindeki maksimum karakter sayısı
	LLMMaxHistoryChars = 320000 // ~80K token
)

// ========================================================================
// 🛠️ TOOL ÇALIŞTIRMA TIMEOUT'LARI
// ========================================================================
const (
	// ToolExecTimeout: Genel tool çalıştırma için maksimum süre
	ToolExecTimeout = 180 * time.Second // 3 dakika

	// ToolAsyncTimeout: Asenkron (background) tool'lar için maksimum süre
	ToolAsyncTimeout = 600 * time.Second // 10 dakika

	// ToolMaxOutputSize: Tool çıktısı için maksimum boyut (byte)
	ToolMaxOutputSize = 1 * 1024 * 1024 // 1 MB

	// PythonToolTimeout: Python tool çalıştırma için maksimum süre
	PythonToolTimeout = 180 * time.Second

	// PythonAsyncTimeout: Python asenkron tool için maksimum süre
	PythonAsyncTimeout = 600 * time.Second

	// ShellExecTimeout: Shell komutu çalıştırma için maksimum süre
	ShellExecTimeout = 60 * time.Second

	// ShellMaxTimeout: Shell için绝对 maksimum timeout
	ShellMaxTimeout = 300 * time.Second // 5 dakika
)

// ========================================================================
// 📁 DOSYA SİSTEMİ TIMEOUT'LARI
// ========================================================================
const (
	// FileReadTimeout: Dosya okuma işlemleri için maksimum süre
	FileReadTimeout = 30 * time.Second

	// FileWriteTimeout: Dosya yazma işlemleri için maksimum süre
	FileWriteTimeout = 30 * time.Second

	// FileDeleteTimeout: Dosya silme işlemleri için maksimum süre
	FileDeleteTimeout = 30 * time.Second

	// FileMaxContentLength: Dosya içeriği için maksimum boyut (byte)
	FileMaxContentLength = 10 * 1024 * 1024 // 10 MB

	// FileMaxPathLength: Dosya yolu için maksimum uzunluk (karakter)
	FileMaxPathLength = 4096
)

// ========================================================================
// 🔌 SSH / AĞ TIMEOUT'LARI
// ========================================================================
const (
	// SSHConnectTimeout: SSH bağlantı kurma için maksimum süre
	SSHConnectTimeout = 15 * time.Second

	// SSHCommandTimeout: SSH komut çalıştırma için maksimum süre
	SSHCommandTimeout = 300 * time.Second // 5 dakika

	// SSHKeepAliveInterval: SSH keep-alive paketleri aralığı
	SSHKeepAliveInterval = 30 * time.Second

	// SSHIdleTimeout: SSH boşta session temizleme süresi
	SSHIdleTimeout = 30 * time.Minute

	// SSHMaxSessionAge: SSH session maksimum ömrü
	SSHMaxSessionAge = 1 * time.Hour

	// SSHCleanupInterval: SSH cleanup worker çalışma aralığı
	SSHCleanupInterval = 5 * time.Minute

	// NetworkScanTimeout: Ağ tarama işlemleri için maksimum süre
	NetworkScanTimeout = 60 * time.Second

	// NetworkMonitorInterval: Ağ izleme aralığı
	NetworkMonitorInterval = 20 * time.Second
)

// ========================================================================
// 📱 WHATSAPP / İLETİŞİM TIMEOUT'LARI
// ========================================================================
const (
	// WhatsAppQRTimeout: QR kod okutma için maksimum süre
	WhatsAppQRTimeout = 120 * time.Second // 2 dakika

	// WhatsAppReconnectDelay: Yeniden bağlanma bekleme süresi
	WhatsAppReconnectDelay = 5 * time.Second

	// WhatsAppMaxReconnectAttempts: Maksimum yeniden bağlanma denemesi
	WhatsAppMaxReconnectAttempts = 3

	// WhatsAppSendTimeout: Mesaj gönderme için maksimum süre
	WhatsAppSendTimeout = 60 * time.Second

	// WhatsAppDownloadTimeout: Medya indirme için maksimum süre
	WhatsAppDownloadTimeout = 30 * time.Second

	// WhatsAppHookRateLimit: Logger hook rate limiting (ms)
	WhatsAppHookRateLimit = 100 * time.Millisecond

	// WhatsAppMaxMessageLength: WhatsApp mesaj maksimum uzunluğu
	WhatsAppMaxMessageLength = 4096

	// WhatsAppMaxCaptionLength: WhatsApp caption maksimum uzunluğu
	WhatsAppMaxCaptionLength = 1024

	// WhatsAppMaxImageSize: WhatsApp resim maksimum boyutu
	WhatsAppMaxImageSize = 20 * 1024 * 1024 // 20 MB

	// WhatsAppThumbnailSize: WhatsApp thumbnail maksimum boyutu
	WhatsAppThumbnailSize = 100 * 1024 // 100 KB
)

// ========================================================================
// 🧠 HAFIZA / RAG TIMEOUT'LARI
// ========================================================================
const (
	// MemoryQueryTimeout: Hafıza sorguları için maksimum süre
	MemoryQueryTimeout = 30 * time.Second

	// MemoryWriteTimeout: Hafıza yazma işlemleri için maksimum süre
	MemoryWriteTimeout = 10 * time.Second

	// RAGIndexTimeout: RAG indeksleme için maksimum süre
	RAGIndexTimeout = 5 * time.Minute

	// RAGSearchLimit: RAG arama sonuçları maksimum sayısı
	RAGSearchLimit = 100

	// RAGMaxContentLength: RAG içerik maksimum boyutu
	RAGMaxContentLength = 1 * 1024 * 1024 // 1 MB

	// RAGChunkSize: RAG chunk boyutu (satır)
	RAGChunkSize = 50

	// MaxHistoryMessages: Sohbet geçmişi maksimum mesaj sayısı
	MaxHistoryMessages = 40
)

// ========================================================================
// 🛡️ GÜVENLİK / EDR TIMEOUT'LARI
// ========================================================================
const (
	// SecurityScanInterval: Güvenlik tarama aralığı
	SecurityScanInterval = 30 * time.Second

	// SecurityAlertCooldown: Güvenlik uyarısı cooldown süresi
	SecurityAlertCooldown = 10 * time.Minute

	// ThreatTrackerWindow: Threat tracker zaman penceresi
	ThreatTrackerWindow = 3 * time.Minute

	// ThreatTrackerLimit: Threat tracker eşik değeri
	ThreatTrackerLimit = 4

	// FIMHashInterval: Dosya bütünlüğü hash kontrol aralığı
	FIMHashInterval = 30 * time.Second

	// ProcessHunterInterval: Process avcısı tarama aralığı
	ProcessHunterInterval = 15 * time.Second

	// PersistenceCheckInterval: Kalıcılık kontrol aralığı
	PersistenceCheckInterval = 2 * time.Minute
)

// ========================================================================
// ⚙️ SİSTEM / TELEMETRİ TIMEOUT'LARI
// ========================================================================
const (
	// TelemetryRAMInterval: RAM izleme aralığı
	TelemetryRAMInterval = 15 * time.Second

	// TelemetryNetworkInterval: Ağ izleme aralığı
	TelemetryNetworkInterval = 20 * time.Second

	// TelemetryCPUInterval: CPU izleme aralığı
	TelemetryCPUInterval = 10 * time.Second

	// TelemetryDiskInterval: Disk izleme aralığı
	TelemetryDiskInterval = 5 * time.Minute

	// TelemetryServiceInterval: Servis izleme aralığı
	TelemetryServiceInterval = 30 * time.Second

	// HeartbeatInterval: Kalp atışı (heartbeat) aralığı
	HeartbeatInterval = 1 * time.Minute

	// CleanupWorkerInterval: Cleanup worker çalışma aralığı
	CleanupWorkerInterval = 5 * time.Minute
)

// ========================================================================
// 🔄 UV / PYTHON ENVIRONMENT TIMEOUT'LARI
// ========================================================================
const (
	// VenvCreateTimeout: Virtual environment oluşturma için maksimum süre
	VenvCreateTimeout = 120 * time.Second // 2 dakika

	// UvInstallTimeout: UV paket kurulumu için maksimum süre
	UvInstallTimeout = 300 * time.Second // 5 dakika

	// UvRunTimeout: UV run komutu için maksimum süre
	UvRunTimeout = 180 * time.Second

	// UvMaxVersion: UV maksimum versiyon (pinning için)
	UvMaxVersion = "latest"
)

// ========================================================================
// 📊 BROWSER / WEB TIMEOUT'LARI
// ========================================================================
const (
	// BrowserPageTimeout: Sayfa yükleme için maksimum süre
	BrowserPageTimeout = 30 * time.Second

	// BrowserActionTimeout: Browser action (click, type vb.) için maksimum süre
	BrowserActionTimeout = 15 * time.Second

	// BrowserSearchTimeout: Arama motoru sorgusu için maksimum süre
	BrowserSearchTimeout = 30 * time.Second

	// BrowserMaxScanFiles: Browser workspace scan maksimum dosya sayısı
	BrowserMaxScanFiles = 200

	// BrowserMaxPeekLines: Browser peek modu maksimum satır sayısı
	BrowserMaxPeekLines = 500

	// BrowserMaxFileSize: Browser maksimum dosya boyutu
	BrowserMaxFileSize = 1 * 1024 * 1024 // 1 MB
)

// ========================================================================
// 🎯 HELPER FONKSİYONLAR
// ========================================================================

// WithDBQueryTimeout: DB sorgusu için timeout'lu context oluşturur
func WithDBQueryTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, DBQueryTimeout)
}

// WithDBWriteTimeout: DB yazma işlemi için timeout'lu context oluşturur
func WithDBWriteTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, DBWriteTimeout)
}

// WithHTTPTimeout: HTTP isteği için timeout'lu context oluşturur
func WithHTTPTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, HTTPTimeout)
}

// WithLLMChatTimeout: LLM chat için timeout'lu context oluşturur
func WithLLMChatTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, LLMChatTimeout)
}

// WithToolExecTimeout: Tool çalıştırma için timeout'lu context oluşturur
func WithToolExecTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, ToolExecTimeout)
}

// WithFileWriteTimeout: Dosya yazma için timeout'lu context oluşturur
func WithFileWriteTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, FileWriteTimeout)
}

// WithSSHCommandTimeout: SSH komut için timeout'lu context oluşturur
func WithSSHCommandTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, SSHCommandTimeout)
}

// WithWhatsAppSendTimeout: WhatsApp mesaj gönderme için timeout'lu context oluşturur
func WithWhatsAppSendTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, WhatsAppSendTimeout)
}

// WithMemoryQueryTimeout: Hafıza sorgusu için timeout'lu context oluşturur
func WithMemoryQueryTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, MemoryQueryTimeout)
}

// WithSecurityScanTimeout: Güvenlik taraması için timeout'lu context oluşturur
func WithSecurityScanTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, SecurityScanInterval)
}

// ========================================================================
// 📋 TIMEOUT ÖZET TABLOSU (Referans İçin)
// ========================================================================
//
// Kategori              | Sabit                      | Değer
// --------------------- | -------------------------- | ------------------
// DB                    | DBPingTimeout              | 5s
// DB                    | DBQueryTimeout             | 30s
// DB                    | DBWriteTimeout             | 10s
// DB                    | DBLoadTimeout              | 5m
// HTTP                  | HTTPTimeout                | 120s
// HTTP                  | HTTPReadTimeout            | 15s
// LLM                   | LLMChatTimeout             | 300s (5m)
// LLM                   | LLMStreamTimeout           | 600s (10m)
// Tool                  | ToolExecTimeout            | 180s (3m)
// Tool                  | ToolAsyncTimeout           | 600s (10m)
// File                  | FileWriteTimeout           | 30s
// File                  | FileDeleteTimeout          | 30s
// SSH                   | SSHConnectTimeout          | 15s
// SSH                   | SSHCommandTimeout          | 300s (5m)
// SSH                   | SSHIdleTimeout             | 30m
// WhatsApp              | WhatsAppQRTimeout          | 120s (2m)
// WhatsApp              | WhatsAppSendTimeout        | 60s
// Memory/RAG            | MemoryQueryTimeout         | 30s
// Memory/RAG            | RAGIndexTimeout            | 5m
// Security              | SecurityScanInterval       | 30s
// Security              | SecurityAlertCooldown      | 10m
// Telemetry             | TelemetryCPUInterval       | 10s
// Telemetry             | TelemetryRAMInterval       | 15s
// UV/Python             | VenvCreateTimeout          | 120s (2m)
// UV/Python             | UvInstallTimeout           | 300s (5m)
// Browser               | BrowserPageTimeout         | 30s
// Browser               | BrowserActionTimeout       | 15s
//
// ========================================================================
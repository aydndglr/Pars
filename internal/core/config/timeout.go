package config

import (
	"context"
	"time"
)

const (
	
	DBPingTimeout = 5 * time.Second
	DBQueryTimeout = 30 * time.Second
	DBWriteTimeout = 10 * time.Second
	DBLoadTimeout = 5 * time.Minute
	DBConnMaxLifetime = 1 * time.Hour
	DBBusyTimeout = 10000 
)

const (
	HTTPTimeout = 120 * time.Second
	HTTPReadTimeout = 15 * time.Second
	HTTPWriteTimeout = 15 * time.Second
	HTTPIdleTimeout = 60 * time.Second
	IPCShutdownTimeout = 5 * time.Second
	IPCMaxCommandTimeout = 30 * time.Minute
)

const (
	LLMChatTimeout = 300 * time.Second 
	LLMStreamTimeout = 600 * time.Second
	LLMEmbedTimeout = 60 * time.Second
	PlanGenerationTimeout = 30 * time.Second
	LLMMaxContentLength = 500 * 1024 
	LLMMaxHistoryChars = 320000 
)

const (
	ToolExecTimeout = 180 * time.Second 
	ToolAsyncTimeout = 600 * time.Second 
	ToolMaxOutputSize = 1 * 1024 * 1024 
	PythonToolTimeout = 180 * time.Second
	PythonAsyncTimeout = 600 * time.Second
	ShellExecTimeout = 60 * time.Second
	ShellMaxTimeout = 300 * time.Second 
)

const (
	FileReadTimeout = 30 * time.Second
	FileWriteTimeout = 30 * time.Second
	FileDeleteTimeout = 30 * time.Second
	FileMaxContentLength = 10 * 1024 * 1024
	FileMaxPathLength = 4096
)

const (
	SSHConnectTimeout = 15 * time.Second
	SSHCommandTimeout = 300 * time.Second 
	SSHKeepAliveInterval = 30 * time.Second
	SSHIdleTimeout = 30 * time.Minute
	SSHMaxSessionAge = 1 * time.Hour
	SSHCleanupInterval = 5 * time.Minute
	NetworkScanTimeout = 60 * time.Second
	NetworkMonitorInterval = 20 * time.Second
)

const (
	WhatsAppQRTimeout = 120 * time.Second 
	WhatsAppReconnectDelay = 5 * time.Second
	WhatsAppMaxReconnectAttempts = 3
	WhatsAppSendTimeout = 60 * time.Second
	WhatsAppDownloadTimeout = 30 * time.Second
	WhatsAppHookRateLimit = 100 * time.Millisecond
	WhatsAppMaxMessageLength = 4096
	WhatsAppMaxCaptionLength = 1024
	WhatsAppMaxImageSize = 20 * 1024 * 1024 
	WhatsAppThumbnailSize = 100 * 1024 
)

const (
	MemoryQueryTimeout = 30 * time.Second
	MemoryWriteTimeout = 10 * time.Second
	RAGIndexTimeout = 5 * time.Minute
	RAGSearchLimit = 100
	RAGMaxContentLength = 1 * 1024 * 1024 
	RAGChunkSize = 50
	MaxHistoryMessages = 40
)

const (
	SecurityScanInterval = 30 * time.Second
	SecurityAlertCooldown = 10 * time.Minute
	ThreatTrackerWindow = 3 * time.Minute
	ThreatTrackerLimit = 4
	FIMHashInterval = 30 * time.Second
	ProcessHunterInterval = 15 * time.Second
	PersistenceCheckInterval = 2 * time.Minute
)

const (
	TelemetryRAMInterval = 15 * time.Second
	TelemetryNetworkInterval = 20 * time.Second
	TelemetryCPUInterval = 10 * time.Second
	TelemetryDiskInterval = 5 * time.Minute
	TelemetryServiceInterval = 30 * time.Second
	HeartbeatInterval = 1 * time.Minute
	CleanupWorkerInterval = 5 * time.Minute
)

const (
	VenvCreateTimeout = 120 * time.Second 
	UvInstallTimeout = 300 * time.Second 
	UvRunTimeout = 180 * time.Second
	UvMaxVersion = "latest"
)

const (
	BrowserPageTimeout = 30 * time.Second
	BrowserActionTimeout = 15 * time.Second
	BrowserSearchTimeout = 30 * time.Second
	BrowserMaxScanFiles = 200
	BrowserMaxPeekLines = 500
	BrowserMaxFileSize = 1 * 1024 * 1024 
)

func WithDBQueryTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, DBQueryTimeout)
}

func WithDBWriteTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, DBWriteTimeout)
}

func WithHTTPTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, HTTPTimeout)
}

func WithLLMChatTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, LLMChatTimeout)
}

func WithToolExecTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, ToolExecTimeout)
}

func WithFileWriteTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, FileWriteTimeout)
}

func WithSSHCommandTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, SSHCommandTimeout)
}

func WithWhatsAppSendTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, WhatsAppSendTimeout)
}

func WithMemoryQueryTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, MemoryQueryTimeout)
}

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
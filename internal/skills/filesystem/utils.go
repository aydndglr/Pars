// internal/skills/filesystem/utils.go
// 🚀 DÜZELTME: auditLog fonksiyonu BURADA kalacak (shared helper)

package filesystem

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

// 🆕 YENİ: auditLog - SIEM uyumlu god_mode audit log (delete.go ve write.go paylaşır)
// 📝 Parametreler: operation, path, result, fileSize, extraInfo
func auditLog(operation, path, result string, fileSize int64, extraInfo string) {
	timestamp := time.Now().Format(time.RFC3339)
	
	// SIEM uyumlu format: [TIMESTAMP] [MODULE] [OPERATION] [PATH] [RESULT] [SIZE] [EXTRA]
	logger.Warn("⚡ [GOD MODE AUDIT] [%s] [%s] [%s] [%s] [RESULT:%s] [SIZE:%d] [INFO:%s]",
		timestamp, "FS", operation, path, result, fileSize, extraInfo)
}

func ResolvePath(reqPath string) string {
	if reqPath == "" || strings.ContainsRune(reqPath, '\x00') {
		return "."
	}
	cleanPath := filepath.Clean(reqPath)
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return cleanPath
	}
	if absPath == "/" || absPath == "\\" || absPath == "C:\\" || absPath == "c:\\" {
		return "."
	}
	return absPath
}
// internal/skills/coding/vscode_tools.go
// 🚀 DÜZELTMELER: Context timeout, Nil checks, Logging, Security, File limits
// ⚠️ DİKKAT: Diğer coding tools ile %100 uyumlu

package coding

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

// 🚨 YENİ: Sabitler ve Limitler
const (
	MaxPeekLines      = 500        // peek modu için maksimum satır
	MaxScanFiles      = 200        // workspace_scan için maksimum dosya
	MaxFileSize       = 1 * 1024 * 1024 // 1 MB (büyük dosyaları okuma)
	VSCodeTimeout     = 10 * time.Second // VSCode komutları için timeout
	SearchTimeout     = 30 * time.Second // search_replace için timeout
)

type AICodeTool struct {
	workDir string
}

func NewAICodeTool(workDir string) *AICodeTool {
	// 🚨 DÜZELTME #1: workDir validation
	if workDir == "" {
		logger.Warn("⚠️ [AICodeTool] workDir boş, fallback kullanılıyor")
		workDir = "."
	}
	return &AICodeTool{workDir: filepath.Clean(workDir)}
}

func (t *AICodeTool) Name() string {
	return "ai_code_editor"
}

func (t *AICodeTool) Description() string {
	return `AI için gelişmiş ve tekil kod düzenleme aracı (Cursor/Devin Mimarisi).

Modlar:
- workspace_scan: Projedeki tüm dosyaları ve klasör yapısını listeler (Context toplamak için mükemmeldir).
- peek: Dosyayı satır numaraları ile okur.
- search_replace: Belirli kodu güvenli ve boşluk-duyarsız (smart-match) şekilde değiştirir.
- line_edit: Satır aralığını (start_line, end_line) değiştirir.
- diff: Dosya değişikliklerini gösterir (Git yoksa basit diff yapar).
- open: VSCode'da dosya açar.`
}

func (t *AICodeTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"mode": map[string]interface{}{
				"type":        "string",
				"description": "Kullanılacak mod: 'workspace_scan', 'peek', 'search_replace', 'line_edit', 'diff', 'open'",
				"enum":        []string{"workspace_scan", "peek", "search_replace", "line_edit", "diff", "open"},
			},
			"file_path": map[string]interface{}{
				"type":        "string",
				"description": "İşlem yapılacak dosya veya klasör yolu (workspace_scan için klasör yolu verin, örn: '.')",
			},
			"search_text": map[string]interface{}{
				"type":        "string",
				"description": "(search_replace modu için) Değiştirilecek olan tam kod bloğu",
			},
			"new_code": map[string]interface{}{
				"type":        "string",
				"description": "(search_replace ve line_edit için) Eklenecek yeni kod bloğu",
			},
			"start_line": map[string]interface{}{
				"type":        "integer",
				"description": "(line_edit modu için) Değişimin başlayacağı satır numarası",
			},
			"end_line": map[string]interface{}{
				"type":        "integer",
				"description": "(line_edit modu için) Değişimin biteceği satır numarası",
			},
		},
		"required": []string{"mode", "file_path"},
	}
}

// 🚨 DÜZELTME #2: safePath iyileştirmesi + logging
func (t *AICodeTool) safePath(path string) (string, error) {
	if t == nil {
		return "", fmt.Errorf("AICodeTool nil")
	}

	abs := path
	if !filepath.IsAbs(path) {
		abs = filepath.Join(t.workDir, path)
	}
	abs = filepath.Clean(abs)

	// Path traversal koruması
	if !strings.HasPrefix(filepath.ToSlash(abs), filepath.ToSlash(t.workDir)) {
		logger.Warn("⚠️ [AICodeTool] Path traversal denemesi: %s", abs)
		return "", fmt.Errorf("🔒 Güvenlik İhlali: Çalışma dizini dışına (%s) çıkılamaz", abs)
	}

	logger.Debug("🔍 [AICodeTool] Path çözümlendi: %s -> %s", path, abs)
	return abs, nil
}

// 🌐 Çapraz Platform VSCode Çalıştırıcı + validation
func (t *AICodeTool) getVSCodeCmd() (string, error) {
	cmd := "code"
	if runtime.GOOS == "windows" {
		cmd = "code.cmd"
	}

	// 🚨 DÜZELTME #3: VSCode bulunabilir mi kontrol et
	if _, err := exec.LookPath(cmd); err != nil {
		// Fallback: alternatif yolları dene
		altPaths := []string{
			`C:\Program Files\Microsoft VS Code\bin\code.cmd`,
			`C:\Program Files (x86)\Microsoft VS Code\bin\code.cmd`,
			`/usr/bin/code`,
			`/usr/local/bin/code`,
		}
		for _, alt := range altPaths {
			if _, err := os.Stat(alt); err == nil {
				logger.Debug("🔍 [AICodeTool] VSCode alternatif yolda bulundu: %s", alt)
				return alt, nil
			}
		}
		return "", fmt.Errorf("VSCode bulunamadı (%s). Lütfen PATH'e ekleyin veya kurulumu doğrulayın", cmd)
	}

	return cmd, nil
}

// 🧠 Akıllı Kod Normalizasyonu (Whitespace & CRLF körlüğünü engeller)
func (t *AICodeTool) normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\t", "    ") // Tabları boşluğa çevir
	return strings.TrimSpace(s)
}

func (t *AICodeTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	// 🚨 DÜZELTME #4: Nil check
	if t == nil {
		return "", fmt.Errorf("AICodeTool nil")
	}

	// 🚨 DÜZELTME #5: Type assertion with ok checks
	modeRaw, ok := args["mode"]
	if !ok || modeRaw == nil {
		return "", fmt.Errorf("'mode' parametresi eksik")
	}
	mode, ok := modeRaw.(string)
	if !ok {
		return "", fmt.Errorf("'mode' parametresi string formatında olmalı")
	}

	filePathRaw, ok := args["file_path"]
	if !ok || filePathRaw == nil {
		return "", fmt.Errorf("'file_path' parametresi eksik")
	}
	filePath, ok := filePathRaw.(string)
	if !ok {
		return "", fmt.Errorf("'file_path' parametresi string formatında olmalı")
	}

	absPath, err := t.safePath(filePath)
	if err != nil {
		return "", err
	}

	logger.Action("🔧 [AICodeTool] Mod: %s, Path: %s", mode, absPath)

	// 🛡️ Parametre Doğrulama (Bug Kalkanı)
	newCode := ""
	if ncRaw, ok := args["new_code"]; ok && ncRaw != nil {
		if nc, ok := ncRaw.(string); ok {
			newCode = nc
		}
	}

	if mode == "search_replace" {
		if _, ok := args["search_text"]; !ok || args["search_text"] == nil {
			return "", fmt.Errorf("search_replace modunda 'search_text' zorunludur")
		}
	}

	// 🚨 DÜZELTME #6: Mode-specific context timeout
	var modeCtx context.Context
	var cancel context.CancelFunc

	switch mode {
	case "search_replace", "line_edit":
		modeCtx, cancel = context.WithTimeout(ctx, SearchTimeout)
	default:
		modeCtx, cancel = context.WithTimeout(ctx, VSCodeTimeout)
	}
	defer cancel()

	switch mode {
	case "workspace_scan":
		return t.workspaceScan(modeCtx, absPath)
	case "peek":
		return t.peek(modeCtx, absPath)
	case "search_replace":
		searchTextRaw, ok := args["search_text"]
		if !ok || searchTextRaw == nil {
			return "", fmt.Errorf("search_text eksik")
		}
		searchText, ok := searchTextRaw.(string)
		if !ok {
			return "", fmt.Errorf("search_text string olmalı")
		}
		return t.searchReplace(modeCtx, absPath, searchText, newCode)
	case "line_edit":
		startLineVal, ok1 := args["start_line"].(float64)
		endLineVal, ok2 := args["end_line"].(float64)
		if !ok1 || !ok2 {
			return "", fmt.Errorf("line_edit modu için start_line ve end_line zorunludur")
		}
		return t.lineEdit(modeCtx, absPath, int(startLineVal), int(endLineVal), newCode)
	case "diff":
		return t.diff(modeCtx, absPath)
	case "open":
		return t.openVSCode(modeCtx, absPath)
	}

	return "", fmt.Errorf("bilinmeyen mod: %s", mode)
}

// 🚀 YENİ: Context Toplayıcı (Gerçek Ajan Gücü) + File limit
func (t *AICodeTool) workspaceScan(ctx context.Context, dirPath string) (string, error) {
	// 🚨 DÜZELTME #7: Context cancellation check
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	info, err := os.Stat(dirPath)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("geçersiz klasör yolu: %v", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📂 Workspace Taraması (%s):\n", dirPath))

	fileCount := 0
	err = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		// 🚨 DÜZELTME #8: Context check in walk
		select {
		case <-ctx.Done():
			return fmt.Errorf("tarama iptal edildi")
		default:
		}

		if err != nil {
			logger.Debug("⚠️ [AICodeTool] Walk error: %v", err)
			return nil
		}
		if info.IsDir() && (info.Name() == ".git" || info.Name() == "node_modules" || info.Name() == "__pycache__") {
			return filepath.SkipDir
		}

		if !info.IsDir() {
			fileCount++
			// 🚨 DÜZELTME #9: File count limit
			if fileCount > MaxScanFiles {
				sb.WriteString(fmt.Sprintf("   ⚠️ ... ve %d+ dosya (limit aşıldı)\n", MaxScanFiles))
				return filepath.SkipDir
			}
		}

		relPath, _ := filepath.Rel(t.workDir, path)
		if info.IsDir() {
			sb.WriteString(fmt.Sprintf("📁 %s/\n", relPath))
		} else {
			sb.WriteString(fmt.Sprintf("   📄 %s\n", relPath))
		}
		return nil
	})

	if err != nil {
		logger.Warn("⚠️ [AICodeTool] Workspace scan hatası: %v", err)
	}

	logger.Debug("✅ [AICodeTool] Workspace scan tamamlandı: %d dosya", fileCount)
	return sb.String(), nil
}

// 🚨 DÜZELTME #10: peek fonksiyonuna context + file size limit
func (t *AICodeTool) peek(ctx context.Context, path string) (string, error) {
	// File size check
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("dosya bulunamadı: %v", err)
	}
	if info.Size() > MaxFileSize {
		return "", fmt.Errorf("dosya çok büyük (%.2f MB > %d MB): %s", 
			float64(info.Size())/(1024*1024), MaxFileSize/(1024*1024), path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(content, "\n")

	// 🚨 DÜZELTME #11: Line limit
	if len(lines) > MaxPeekLines {
		lines = lines[:MaxPeekLines]
	}

	var out strings.Builder
	out.WriteString(fmt.Sprintf("---- FILE PEEK: %s (İlk %d satır) ----\n", filepath.Base(path), len(lines)))
	for i, l := range lines {
		// 🚨 DÜZELTME #12: Context check in loop
		select {
		case <-ctx.Done():
			out.WriteString("\n[... tarama iptal edildi ...]")
			break
		default:
		}
		out.WriteString(fmt.Sprintf("%d: %s\n", i+1, l))
	}

	if len(lines) < len(strings.Split(content, "\n")) {
		out.WriteString(fmt.Sprintf("\n[... %d satır daha var, max_lines limitine ulaşıldı ...]", 
			len(strings.Split(content, "\n")) - len(lines)))
	}

	return out.String(), nil
}

// 🚨 DÜZELTME #13: backup fonksiyonunda error propagation
func (t *AICodeTool) backup(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		logger.Error("❌ [AICodeTool] Backup okuma hatası: %v", err)
		return "", err
	}
	backup := fmt.Sprintf("%s.bak_%d", path, time.Now().Unix())
	err = os.WriteFile(backup, data, 0644)
	if err != nil {
		logger.Error("❌ [AICodeTool] Backup yazma hatası: %v", err)
		return "", err
	}
	logger.Debug("💾 [AICodeTool] Backup oluşturuldu: %s", filepath.Base(backup))
	return backup, nil
}

// 🚨 DÜZELTME #14: searchReplace fonksiyonunda context + error handling
func (t *AICodeTool) searchReplace(ctx context.Context, path, search, newCode string) (string, error) {
	// Context check
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	content := string(data)
	normContent := t.normalize(content)
	normSearch := t.normalize(search)

	// AI'nin gönderdiği stringi akıllıca arıyoruz
	if !strings.Contains(normContent, normSearch) {
		return "", fmt.Errorf("aranan kod bloğu bulunamadı. Boşluk veya girinti hatası olabilir, lütfen 'peek' ile tam metni kontrol et")
	}

	// CRLF standardizasyonu ile orijinal metin üzerinde değiştirme yapıyoruz
	stdContent := strings.ReplaceAll(content, "\r\n", "\n")
	stdSearch := strings.ReplaceAll(search, "\r\n", "\n")

	if !strings.Contains(stdContent, stdSearch) {
		return "", fmt.Errorf("kod eşleşti ancak format uyumsuzluğu var (Line_edit modunu kullanmayı dene)")
	}

	// 🚨 DÜZELTME #15: Backup error handling
	backup, err := t.backup(path)
	if err != nil {
		logger.Warn("⚠️ [AICodeTool] Backup oluşturulamadı ama değişiklik devam ediyor: %v", err)
		// Backup başarısız olsa bile değişiklik yapmaya devam et (kullanıcı tercihine bağlı)
	}

	newContent := strings.Replace(stdContent, stdSearch, newCode, 1)

	err = os.WriteFile(path, []byte(newContent), 0644)
	if err != nil {
		// 🚨 DÜZELTME #16: Rollback attempt on write failure
		if backup != "" {
			if rbData, rbErr := os.ReadFile(backup); rbErr == nil {
				_ = os.WriteFile(path, rbData, 0644)
				logger.Warn("⚠️ [AICodeTool] Yazma hatası sonrası rollback yapıldı")
			}
		}
		return "", fmt.Errorf("dosya yazılamadı: %v", err)
	}

	// 🚨 DÜZELTME #17: VSCode command error handling
	vsCmd, vsErr := t.getVSCodeCmd()
	if vsErr == nil {
		cmd := exec.CommandContext(ctx, vsCmd, "--goto", path)
		if err := cmd.Run(); err != nil {
			logger.Debug("⚠️ [AICodeTool] VSCode açılamadı: %v", err)
			// VSCode açılamasa bile değişiklik başarılı, hata dönme
		}
	}

	logger.Success("✅ [AICodeTool] search_replace tamamlandı: %s", filepath.Base(path))
	return fmt.Sprintf("✅ Kod başarıyla değiştirildi.\nBackup: %s", filepath.Base(backup)), nil
}

// 🚨 DÜZELTME #18: lineEdit fonksiyonunda context + validation
func (t *AICodeTool) lineEdit(ctx context.Context, path string, start, end int, newCode string) (string, error) {
	// Context check
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	// 🚨 DÜZELTME #19: Bounds validation
	if start < 1 {
		start = 1
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(content, "\n")

	if end > len(lines) {
		end = len(lines)
	}
	if start > end {
		return "", fmt.Errorf("start_line (%d) end_line (%d)'dan büyük olamaz", start, end)
	}

	backup, err := t.backup(path)
	if err != nil {
		logger.Warn("⚠️ [AICodeTool] Backup oluşturulamadı: %v", err)
	}

	var newLines []string
	newLines = append(newLines, lines[:start-1]...)

	if newCode != "" {
		newLines = append(newLines, strings.Split(strings.ReplaceAll(newCode, "\r\n", "\n"), "\n")...)
	}

	newLines = append(newLines, lines[end:]...)
	final := strings.Join(newLines, "\n")

	err = os.WriteFile(path, []byte(final), 0644)
	if err != nil {
		if backup != "" {
			if rbData, rbErr := os.ReadFile(backup); rbErr == nil {
				_ = os.WriteFile(path, rbData, 0644)
			}
		}
		return "", fmt.Errorf("dosya yazılamadı: %v", err)
	}

	vsCmd, vsErr := t.getVSCodeCmd()
	if vsErr == nil {
		cmd := exec.CommandContext(ctx, vsCmd, "--goto", fmt.Sprintf("%s:%d", path, start))
		if err := cmd.Run(); err != nil {
			logger.Debug("⚠️ [AICodeTool] VSCode açılamadı: %v", err)
		}
	}

	logger.Success("✅ [AICodeTool] line_edit tamamlandı: %s (%d-%d)", filepath.Base(path), start, end)
	return fmt.Sprintf("✅ Satırlar başarıyla değiştirildi.\nBackup: %s", filepath.Base(backup)), nil
}

// 🚨 DÜZELTME #20: diff fonksiyonunda context + fallback iyileştirme
func (t *AICodeTool) diff(ctx context.Context, path string) (string, error) {
	// Context check
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	// 🚨 DÜZELTME #21: Git var mı kontrol et
	if _, err := exec.LookPath("git"); err != nil {
		return "⚠️ Git bulunamadı. Lütfen değişiklikleri doğrudan editörden ('open' veya 'peek' ile) inceleyin.", nil
	}

	cmd := exec.CommandContext(ctx, "git", "diff", path)
	out, err := cmd.CombinedOutput()

	if err != nil {
		// Git repo değilse daha açıklayıcı mesaj
		exitErr, ok := err.(*exec.ExitError)
		if ok && exitErr.ExitCode() == 128 {
			return "⚠️ Bu dizin Git reposu değil. 'git init' ile repo başlatabilir veya değişiklikleri 'peek' ile inceleyebilirsiniz.", nil
		}
		logger.Debug("⚠️ [AICodeTool] Git diff hatası: %v", err)
		return "⚠️ Git diff çalıştırılamadı. Lütfen değişiklikleri doğrudan editörden inceleyin.", nil
	}

	if len(out) == 0 {
		return "✅ Değişiklik bulunamadı (dosya temiz).", nil
	}

	diffStr := string(out)
	// 🚨 DÜZELTME #22: Büyük diff'leri kırp
	if len(diffStr) > 10000 {
		diffStr = diffStr[:10000] + "\n\n[... diff çok uzun, ilk 10000 karakter gösterildi ...]"
	}

	logger.Debug("✅ [AICodeTool] diff tamamlandı: %d karakter", len(diffStr))
	return diffStr, nil
}

// 🚨 DÜZELTME #23: openVSCode fonksiyonunda context + better error
func (t *AICodeTool) openVSCode(ctx context.Context, path string) (string, error) {
	// Context check
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	vsCmd, err := t.getVSCodeCmd()
	if err != nil {
		return "", fmt.Errorf("VSCode bulunamadı: %v. Lütfen VSCode'u kurun ve PATH'e ekleyin", err)
	}

	cmd := exec.CommandContext(ctx, vsCmd, "--goto", path)
	if err := cmd.Run(); err != nil {
		logger.Error("❌ [AICodeTool] VSCode açılamadı: %v", err)
		return "", fmt.Errorf("VSCode tetiklenirken hata oluştu: %v. Dosya yolu: %s", err, path)
	}

	logger.Success("✅ [AICodeTool] VSCode açıldı: %s", path)
	return "✅ Dosya VSCode'da açıldı ve odaklanıldı.", nil
}

// 🆕 YENİ: GetStatus - Tool durumunu sorgula (debug için)
func (t *AICodeTool) GetStatus() map[string]interface{} {
	if t == nil {
		return map[string]interface{}{"error": "tool nil"}
	}
	return map[string]interface{}{
		"name":     t.Name(),
		"workDir":  t.workDir,
		"vsCodeOk": func() bool {
			_, err := t.getVSCodeCmd()
			return err == nil
		}(),
	}
}
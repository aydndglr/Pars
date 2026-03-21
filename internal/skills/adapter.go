package skills

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "time"

    "github.com/aydndglr/pars-agent-v3/internal/core/logger"
)


const (
    PythonToolTimeout      = 180 * time.Second 
    PythonAsyncTimeout     = 600 * time.Second 
    MaxOutputSize          = 1 * 1024 * 1024   
)

type PythonTool struct {
    name         string
    description  string
    scriptPath   string
    interpreter  string
    uvPath       string 
    workDir      string
    parameters   map[string]interface{}
    isAsync      bool
    instructions string 
    NotificationChan chan<- string 
}


func NewPythonTool(name, desc, path, pythonPath, uvPath string, params map[string]interface{}, isAsync bool, instructions string) *PythonTool {

    if name == "" {
        logger.Warn("⚠️ [PythonTool] İsimsiz tool oluşturulmaya çalışıldı, 'unknown_tool' olarak kaydedildi")
        name = "unknown_tool"
    }

    baseScriptName := filepath.Base(path)
    rootDir, err := filepath.Abs(".")
    if err != nil {
        logger.Warn("⚠️ [PythonTool] Kök dizin alınamadı: %v, fallback kullanılıyor", err)
        rootDir = "."
    }

    absWorkDir := filepath.Join(rootDir, "tools")
    absScriptPath := filepath.Join(absWorkDir, baseScriptName)

    absInterpreter, _ := filepath.Abs(pythonPath)
    absUvPath, _ := filepath.Abs(uvPath)

    return &PythonTool{
        name:         name,
        description:  desc,
        scriptPath:   absScriptPath,
        interpreter:  absInterpreter,
        uvPath:       absUvPath,
        workDir:      absWorkDir,
        parameters:   params,
        isAsync:      isAsync,
        instructions: instructions,
    }
}

func (p *PythonTool) notifyUser(msg string) {
    if p.NotificationChan != nil {
        select {
        case p.NotificationChan <- msg:

        default:
            logger.Warn("⚠️ Tool bildirim kuyruğu dolu veya bloklu, mesaj atlandı: %s", msg)
        }
    }
}


func (p *PythonTool) SetNotificationChannel(ch chan<- string) {
    if p != nil {
        p.NotificationChan = ch
    }
}

func (p *PythonTool) Name() string {
    if p == nil {
        return "unknown_tool"
    }
    return p.name
}

func (p *PythonTool) Description() string {
    if p == nil {
        return "Tool tanımlanmamış"
    }
    return p.description
}

func (p *PythonTool) Parameters() map[string]interface{} {
    if p == nil {
        return map[string]interface{}{
            "type":       "object",
            "properties": map[string]interface{}{},
        }
    }

    if p.parameters == nil || len(p.parameters) == 0 {
        return map[string]interface{}{
            "type":       "object",
            "properties": map[string]interface{}{},
        }
    }
    return p.parameters
}

func (p *PythonTool) buildCmd(ctx context.Context, jsonArgs string) *exec.Cmd {
    if p == nil {
        return nil
    }

    if p.instructions != "" {
        packages := strings.FieldsFunc(p.instructions, func(r rune) bool {
            return r == ',' || r == ' ' || r == ';'
        })

        uvArgs := []string{"run"}
        for _, pkg := range packages {
            pkg = strings.TrimSpace(pkg)
            if pkg != "" {
                uvArgs = append(uvArgs, "--with", pkg)
            }
        }

        uvArgs = append(uvArgs, p.interpreter, p.scriptPath, jsonArgs)
        return exec.CommandContext(ctx, p.uvPath, uvArgs...)
    }

    return exec.CommandContext(ctx, p.interpreter, p.scriptPath, jsonArgs)
}

func (p *PythonTool) validatePaths() error {
    if p == nil {
        return fmt.Errorf("tool nil")
    }
    if _, err := os.Stat(p.scriptPath); os.IsNotExist(err) {
        return fmt.Errorf("script dosyası bulunamadı: %s", p.scriptPath)
    }

    if _, err := os.Stat(p.interpreter); os.IsNotExist(err) {
        return fmt.Errorf("python yorumlayıcı bulunamadı: %s", p.interpreter)
    }

    if p.instructions != "" {
        if _, err := os.Stat(p.uvPath); os.IsNotExist(err) {
            return fmt.Errorf("uv bulunamadı: %s", p.uvPath)
        }
    }

    if !strings.HasPrefix(p.scriptPath, p.workDir) {
        return fmt.Errorf("güvenlik ihlali: script tools/ klasörü dışında: %s", p.scriptPath)
    }

    return nil
}

func (p *PythonTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
    if p == nil {
        return "", fmt.Errorf("tool nil")
    }

    if err := p.validatePaths(); err != nil {
        return "", fmt.Errorf("doğrulama hatası: %w", err)
    }

    jsonBytes, err := json.Marshal(args)
    if err != nil {
        return "", fmt.Errorf("argüman paketleme hatası: %v", err)
    }
    jsonArgs := string(jsonBytes)

    logger.Debug("🐍 Araç Tetiklendi: %s | Konum: %s", p.name, p.scriptPath)

    if p.isAsync {
        logger.Action("🔄 [%s] Asenkron görev arka plana atılıyor...", p.name)

        go func() {

            bgCtx, cancel := context.WithTimeout(context.Background(), PythonAsyncTimeout)
            defer cancel()

            cmd := p.buildCmd(bgCtx, jsonArgs)
            if cmd == nil {
                logger.Error("❌ [%s] Komut oluşturulamadı (nil)", p.name)
                p.notifyUser(fmt.Sprintf("❌ [HATA]: '%s' aracı başlatılamadı!", p.name))
                return
            }

            cmd.Dir = p.workDir
            cmd.Env = os.Environ()
            outputBytes, err := cmd.CombinedOutput()
            resultStr := string(outputBytes)
            if len(resultStr) > MaxOutputSize {
                resultStr = resultStr[:MaxOutputSize] + "\n...[OUTPUT KISITLANDI - 1MB LIMIT]..."
            }

            if err != nil {
                if exitErr, ok := err.(*exec.ExitError); ok {
                    logger.Error("❌ [%s] Arka Plan Görevi Çöktü (Kod %d)\nTraceback Detayı:\n%s", p.name, exitErr.ExitCode(), resultStr)
                } else {
                    logger.Error("❌ [%s] Arka Plan Görevi Hatası: %v\nDetay:\n%s", p.name, err, resultStr)
                }
                p.notifyUser(fmt.Sprintf("⚠️ [GÖREV HATASI]: '%s' aracı arka planda çalışırken çöktü. Lütfen logları kontrol et.", p.name))
            } else {
                logger.Success("✅ [%s] Arka Plan Görevi Başarıyla Tamamlandı!\nÇıktı:\n%s", p.name, resultStr)
                p.notifyUser(fmt.Sprintf("✅ [GÖREV TAMAMLANDI]: '%s' aracı arka planda işlemini başarıyla bitirdi!", p.name))
            }
        }()

        return fmt.Sprintf("✅ '%s' aracı arka planda başarıyla başlatıldı. Sen başka komutlar vermeye devam edebilirsin.", p.name), nil
    }
    execCtx, cancel := context.WithTimeout(ctx, PythonToolTimeout)
    defer cancel()

    cmd := p.buildCmd(execCtx, jsonArgs)
    if cmd == nil {
        return "", fmt.Errorf("komut oluşturulamadı (nil)")
    }

    cmd.Dir = p.workDir
    cmd.Env = os.Environ()
    outputBytes, err := cmd.CombinedOutput()
    result := string(outputBytes)

    if len(result) > MaxOutputSize {
        result = result[:MaxOutputSize] + "\n...[OUTPUT KISITLANDI - 1MB LIMIT]..."
    }

    if err != nil {

        if execCtx.Err() == context.DeadlineExceeded {
            logger.Warn("⏰ [%s] Çalıştırma zaman aşımına uğradı (%d sn)", p.name, int(PythonToolTimeout.Seconds()))
            return fmt.Sprintf("⏰ ZAMAN AŞIMI: '%s' aracı %d saniye içinde tamamlanmadı.", p.name, int(PythonToolTimeout.Seconds())), nil
        }

        if exitErr, ok := err.(*exec.ExitError); ok {
            return fmt.Sprintf("❌ Script Hatası (%s) - Exit Code %d:\n%s", p.name, exitErr.ExitCode(), result), nil
        }

        return "", fmt.Errorf("kritik çalıştırma hatası: %v\nDetay: %s", err, result)
    }

    return strings.TrimSpace(result), nil
}

func (p *PythonTool) GetInfo() map[string]interface{} {
    if p == nil {
        return map[string]interface{}{
            "error": "tool nil",
        }
    }

    return map[string]interface{}{
        "name":         p.name,
        "description":  p.description,
        "script_path":  p.scriptPath,
        "interpreter":  p.interpreter,
        "uv_path":      p.uvPath,
        "work_dir":     p.workDir,
        "is_async":     p.isAsync,
        "has_packages": p.instructions != "",
    }
}

func (p *PythonTool) SetAsync(async bool) {
    if p == nil {
        return
    }
    p.isAsync = async
    logger.Debug("🔄 [%s] Async modu değiştirildi: %v", p.name, async)
}
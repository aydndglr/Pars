package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/heartbeat"
	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/aydndglr/pars-agent-v3/internal/memory"
)

const (
	MaxHistoryMessages = 40
	MaxParallelTools   = 4
	DefaultMaxSteps    = 25
	ToolExecTimeout    = 3 * time.Minute
	MemoryWriteTimeout = 5 * time.Second
)

func getTaskTypeFromSessionID(sessionID string) heartbeat.TaskType {
	userPrefixes := []string{"[WA-ALERT-", "TSK-", "CLI-", "CMD-", "IPC-"}
	for _, prefix := range userPrefixes {
		if strings.HasPrefix(sessionID, prefix) {
			return heartbeat.TaskTypeUser
		}
	}
	return heartbeat.TaskTypeAgent
}

func isChatModePlan(plan string) bool {
	if plan == "" {
		return false
	}
	planLower := strings.ToLower(strings.TrimSpace(plan))
	if strings.Contains(planLower, "no_plan_needed") {
		return true
	}
	chatIndicators := []string{"chat mode", "sohbet modu"}
	for _, indicator := range chatIndicators {
		if strings.Contains(planLower, indicator) {
			return true
		}
	}
	return false
}

func writeChatMessageAsync(mem kernel.Memory, sessionID, role, content string) {
	if mem == nil || content == "" {
		return
	}
	sqlStore, ok := mem.(*memory.SQLiteStore)
	if !ok {
		return
	}
	go func() {
		memCtx, memCancel := context.WithTimeout(context.Background(), MemoryWriteTimeout)
		defer memCancel()
		_ = sqlStore.AddChatMessage(memCtx, sessionID, role, content)
	}()
}

func extractToolCallsFromText(text string) []kernel.ToolCall {
	var calls []kernel.ToolCall
	re := regexp.MustCompile(`(?i)(?:(?:json)?Native\s*)?Tool\s*Call:?\s*([a-zA-Z0-9_]+)\s*\((.*?)\)\]?`)
	matches := re.FindAllStringSubmatch(text, -1)

	for _, m := range matches {
		if len(m) >= 3 {
			funcName := strings.TrimSpace(m[1])
			argsStr := strings.TrimSpace(m[2])

			for strings.HasSuffix(argsStr, "]") || strings.HasSuffix(argsStr, ")") {
				argsStr = argsStr[:len(argsStr)-1]
				argsStr = strings.TrimSpace(argsStr)
			}

			if strings.HasPrefix(argsStr, "{") && strings.HasSuffix(argsStr, "}") {
				var argsMap map[string]interface{}
				if err := json.Unmarshal([]byte(argsStr), &argsMap); err == nil {
					calls = append(calls, kernel.ToolCall{
						ID:        fmt.Sprintf("call_fb_%X", time.Now().UnixNano()%0xFFFF),
						Function:  funcName,
						Arguments: argsMap,
					})
				}
			}
		}
	}
	return calls
}

func (a *Pars) Run(ctx context.Context, input string, images []string) (string, error) {
	logger.Info("🚀 [Execution] Run fonksiyonu başlatıldı, input uzunluğu: %d karakter", len(input))

	if a == nil {
		return "", fmt.Errorf("pars instance nil")
	}

	baseCtx, cancel := context.WithCancel(ctx)
	clientTaskID, _ := ctx.Value("client_task_id").(string)

	a.sessMu.Lock()
	sess, exists := a.sessions[clientTaskID]
	if !exists {
		sess = &Session{
			ID:        clientTaskID,
			History:   make([]kernel.Message, 0, MaxHistoryMessages),
			CreatedAt: time.Now(),
			Cancel:    cancel,
		}
		if sess.ID == "" {
			sess.ID = fmt.Sprintf("TSK-%X", time.Now().UnixNano()%0xFFFFF)
		}
		a.sessions[sess.ID] = sess
	} else {
		sess.mu.Lock()
		if sess.Cancel != nil {
			sess.Cancel()
		}
		sess.Cancel = cancel
		sess.mu.Unlock()
	}
	a.sessMu.Unlock()

	isEphemeral := strings.HasPrefix(sess.ID, "[WA-ALERT-")

	defer func() {
		cancel()
		if isEphemeral {
			a.sessMu.Lock()
			delete(a.sessions, sess.ID)
			a.sessMu.Unlock()
		}
	}()

	sessCtx := context.WithValue(baseCtx, "session_id", sess.ID)

	sess.mu.Lock()
	sess.History = append(sess.History, kernel.Message{
		Role:    kernel.RoleUser,
		Content: input,
		Images:  images,
	})
	a.trimHistory(sess)
	sess.mu.Unlock()

	writeChatMessageAsync(a.Memory, sess.ID, kernel.RoleUser, input)

	planCtx := context.WithValue(sessCtx, "stream_chan", nil)
	plan, planErr := a.generatePlan(planCtx, input)

	isChatMode := isChatModePlan(plan)

	if containsTaskKeywords(input) {
		if isChatMode {
			logger.Info("🔧 [%s] Task keyword tespit edildi, CHAT MODE kararı eziliyor → TASK MODE FORCE!", sess.ID)
		} else {
			logger.Debug("🔧 [%s] Task keyword tespit edildi, task mode onaylandı.", sess.ID)
		}
		isChatMode = false
	}

	sess.mu.Lock()
	sess.Plan = plan
	sess.mu.Unlock()

	if !isChatMode && planErr == nil && plan != "" {
		logger.Success("📝 [%s] Harekât Planı:\n%s", sess.ID, plan)
	}

	a.refreshSystemPrompt(sess)

	stepTimer := time.NewTimer(time.Second)
	defer stepTimer.Stop()

	maxSteps := a.MaxSteps
	if maxSteps <= 0 {
		maxSteps = DefaultMaxSteps
	}

	logger.Info("🔄 [Execution] Ana döngü başlatılıyor, maksimum %d adım", maxSteps)
	for step := 0; step < maxSteps; step++ {
		stepTimer.Reset(time.Second)

		select {
		case <-sessCtx.Done():
			a.updateTaskStatus(sess.ID, heartbeat.TaskStatusFailed)
			return fmt.Sprintf("🛑 [%s] İşlem iptal edildi.", sess.ID), nil
		case <-stepTimer.C:
		}

		a.manageContextWindow(sess)

		if a.Skills == nil || a.Brain == nil {
			a.updateTaskStatus(sess.ID, heartbeat.TaskStatusFailed)
			return "", fmt.Errorf("core engine uninitialized")
		}

		sess.mu.Lock()
		currentHistory := make([]kernel.Message, len(sess.History))
		copy(currentHistory, sess.History)
		sess.mu.Unlock()

		var chatTools []kernel.Tool
		if !isChatMode {
			chatTools = a.Skills.ListTools()
		}

		resp, err := a.Brain.Chat(sessCtx, currentHistory, chatTools)
		
		// 🔥 OLLAMA 500 CRASH RECOVERY (HATA AMORTİSÖRÜ) 🔥
		if err != nil && (strings.Contains(err.Error(), "500") || strings.Contains(err.Error(), "invalid character") || strings.Contains(err.Error(), "parse JSON")) {
			logger.Warn("⚠️ [%s] LLM Native Tool motoru çöktü (Ollama 500 Hatası). Native araçlar gizlenerek düz metin (Fallback) modunda tekrar deneniyor...", sess.ID)
			// Araçları 'nil' göndererek Ollama'yı düz metin (text-only) üretmeye zorluyoruz!
			resp, err = a.Brain.Chat(sessCtx, currentHistory, nil)
		}

		if err != nil {
			if sessCtx.Err() != nil {
				return fmt.Sprintf("🛑 [%s] Görev kullanıcı tarafından sonlandırıldı.", sess.ID), nil
			}
			a.updateTaskStatus(sess.ID, heartbeat.TaskStatusFailed)
			return "", fmt.Errorf("brain chat hatası: %w", err)
		}

		toolCalls := resp.GetToolCalls()
		content := resp.GetContent()

		if !isChatMode && len(toolCalls) == 0 {
			a.checkAndParseToolFallback(resp, step)
			toolCalls = resp.GetToolCalls()

			if len(toolCalls) == 0 && content != "" {
				extracted := extractToolCallsFromText(content)
				if len(extracted) > 0 {
					logger.Info("🚑 [Execution] Regex Fallback Başarılı: %d tool call kurtarıldı!", len(extracted))
					toolCalls = extracted
				}
			}
		}

		sess.mu.Lock()
		sess.History = append(sess.History, kernel.Message{
			Role:      kernel.RoleAssistant,
			Content:   content,
			ToolCalls: toolCalls,
		})
		a.trimHistory(sess)
		sess.mu.Unlock()

		if content != "" {
			writeChatMessageAsync(a.Memory, sess.ID, kernel.RoleAssistant, content)
		}

		if len(toolCalls) > 0 && isChatMode {
			isChatMode = false
		}

		if isChatMode {
			return a.finalizeTask(sess, input, content)
		}

		if len(toolCalls) == 0 {
			if step == 0 {
				logger.Warn("⚠️ [%s] Task mode ilk adımında tool tetiklenmedi! Uyarı gönderiliyor.", sess.ID)
				
				if content != "" {
					logger.Debug("🔄 [%s] Modele aracı tetiklemesi için zorunlu komut gönderiliyor...", sess.ID)
					
					sess.mu.Lock()
					sess.History = append(sess.History, kernel.Message{
						Role:    kernel.RoleUser,
						Content: "SİSTEM EMRİ: Görev planını yaptın veya açıklama yazdın, ancak HİÇBİR ARACI TETİKLEMEDİN! Lütfen işlemi yapmak için DERHAL '[Native Tool Call: arac_adi({\"parametre\": \"deger\"})]' formatını kullanarak aracı çağır. Başka hiçbir açıklama yazma.",
					})
					a.trimHistory(sess)
					sess.mu.Unlock()
					
					continue // Döngüyü başa sar ve aracı çağırmasını sağla
				}
				continue
			} else {
				// EĞER STEP > 0 İSE: Model zaten daha önce araç kullandı, sonucunu aldı 
				// ve şimdi bize final özetini yazdı. Bırakalım görevi bitirsin!
				logger.Info("🎯 [%s] Task mode - Araçlar kullanıldı ve görev final metni ile başarıyla bitiriliyor.", sess.ID)
				
				if content == "" {
					content = "Görev başarıyla tamamlandı."
				}
				return a.finalizeTask(sess, input, content)
			}
		}

		toolCtx, toolCancel := context.WithTimeout(sessCtx, ToolExecTimeout)
		var wg sync.WaitGroup
		sem := make(chan struct{}, MaxParallelTools)
		var resultsMu sync.Mutex
		var toolResults []kernel.Message

		for i, call := range toolCalls {
			if call.Function == "" {
				continue
			}
			wg.Add(1)
			go func(c kernel.ToolCall, idx int) {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
				case <-toolCtx.Done():
					return
				case <-sessCtx.Done():
					return
				}
				defer func() { <-sem }()

				if toolCtx.Err() != nil || sessCtx.Err() != nil {
					return
				}

				logger.Action("⚡ [%s] Tool: %s", sess.ID, c.Function)
				output, toolErr := a.executeToolSafe(toolCtx, c)

				var toolImages []string
				if toolErr == nil {
					toolImages = extractImagesFromOutput(output)
				} else {
					output = fmt.Sprintf("❌ HATA: %v\nAraç başarısız oldu.", toolErr)
				}

				resultsMu.Lock()
				toolResults = append(toolResults, kernel.Message{
					Role:       kernel.RoleTool,
					Content:    output,
					Name:       c.Function,
					ToolCallID: c.ID,
					Images:     toolImages,
				})
				resultsMu.Unlock()
			}(call, i+1)
		}

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-toolCtx.Done():
		case <-sessCtx.Done():
		}
		toolCancel()

		if len(toolResults) > 0 {
			sess.mu.Lock()
			sess.History = append(sess.History, toolResults...)
			a.trimHistory(sess)
			sess.mu.Unlock()
		}
	}

	a.updateTaskStatus(sess.ID, heartbeat.TaskStatusFailed)
	return fmt.Sprintf("🛑 [%s] Maksimum adım limiti aşıldı (%d).", sess.ID, maxSteps), nil
}

func extractImagesFromOutput(output string) []string {
	var images []string
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		return images
	}
	if path, ok := parsed["image_path"].(string); ok && path != "" {
		if info, err := os.Stat(path); err == nil && info.Size() < 5*1024*1024 {
			if imgData, err := os.ReadFile(path); err == nil {
				images = append(images, base64.StdEncoding.EncodeToString(imgData))
			}
		}
	}
	return images
}

func (a *Pars) updateTaskStatus(sessionID string, status heartbeat.TaskStatus) {
	if a == nil {
		return
	}
	taskType := getTaskTypeFromSessionID(sessionID)
	if taskType == heartbeat.TaskTypeAgent {
		logger.Debug("🔄 [TaskStatus] Agent görev: %s -> %s", sessionID, status)
	}
}

func (a *Pars) trimHistory(sess *Session) {
	if a == nil || sess == nil || len(sess.History) <= MaxHistoryMessages {
		return
	}

	hasSystem := len(sess.History) > 0 && sess.History[0].Role == kernel.RoleSystem
	keepCount := MaxHistoryMessages

	if hasSystem {
		sysMsg := sess.History[0]
		startIdx := len(sess.History) - (keepCount - 1)
		if startIdx < 1 {
			startIdx = 1
		}
		newHistory := make([]kernel.Message, 0, keepCount)
		newHistory = append(newHistory, sysMsg)
		newHistory = append(newHistory, sess.History[startIdx:]...)
		sess.History = newHistory
	} else {
		sess.History = sess.History[len(sess.History)-keepCount:]
	}
}

func (a *Pars) executeToolSafe(ctx context.Context, call kernel.ToolCall) (output string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()

	if a == nil || a.Skills == nil {
		return "", fmt.Errorf("engine uninitialized")
	}

	tool, err := a.Skills.GetTool(call.Function)
	if err != nil {
		return "", fmt.Errorf("tool '%s' bulunamadı: %w", call.Function, err)
	}

	output, err = tool.Execute(ctx, call.Arguments)
	return output, err
}

func (a *Pars) finalizeTask(sess *Session, input, output string) (string, error) {
	if a == nil || sess == nil {
		return "", fmt.Errorf("session nil")
	}

	sess.mu.Lock()
	plan := sess.Plan
	sess.mu.Unlock()

	a.updateTaskStatus(sess.ID, heartbeat.TaskStatusCompleted)

	var finalOutput string
	if output == "" {
		finalOutput = fmt.Sprintf("[Görev ID: %s]\n%s", sess.ID, input)
	} else {
		memoryContent := fmt.Sprintf("GÖREV: %s\nPLAN: %s\nSONUÇ: %s", input, plan, output)
		go func() {
			if a.Memory != nil {
				memCtx, memCancel := context.WithTimeout(context.Background(), MemoryWriteTimeout)
				defer memCancel()
				_ = a.Memory.Add(memCtx, memoryContent, nil)
			}
		}()
		finalOutput = fmt.Sprintf("[Görev ID: %s]\n%s", sess.ID, output)
	}

	return finalOutput, nil
}

func (a *Pars) CancelSession(sessionID string) {
	if a == nil {
		return
	}

	a.sessMu.RLock()
	sess, exists := a.sessions[sessionID]
	a.sessMu.RUnlock()

	if exists && sess != nil {
		a.updateTaskStatus(sessionID, heartbeat.TaskStatusFailed)

		sess.mu.Lock()
		cancelFunc := sess.Cancel
		sess.Cancel = nil
		sess.mu.Unlock()

		if cancelFunc != nil {
			cancelFunc()
		}

		a.sessMu.Lock()
		delete(a.sessions, sessionID)
		a.sessMu.Unlock()
	}
}
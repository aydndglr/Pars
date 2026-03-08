// internal/agent/execution.go
// 🚀 DÜZELTMELER: Thread-safe BrainResponse erişimi, Nil checks, Error handling
// ⚠️ DİKKAT: kernel.BrainResponse'ın yeni thread-safe metodlarını kullanır

package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/aydndglr/pars-agent-v3/internal/memory"
)

const (
	MaxHistoryMessages = 40
	MaxParallelTools   = 4
	DefaultMaxSteps    = 20
)

func (a *Pars) Run(ctx context.Context, input string, images []string) (string, error) {

	// 🛡️ Context Yönetimi
	baseCtx, cancel := context.WithCancel(ctx)

	clientTaskID, _ := ctx.Value("client_task_id").(string)

	// 🛡️ Oturum Oluşturma ve Kilit Yönetimi (Atomik İşlem)
	a.sessMu.Lock()
	sess, exists := a.Sessions[clientTaskID]

	if !exists {
		sess = &Session{
			ID:        clientTaskID,
			History:   []kernel.Message{},
			CreatedAt: time.Now(),
			Cancel:    cancel,
		}

		if clientTaskID == "" {
			sess.ID = fmt.Sprintf("TSK-%X", time.Now().UnixNano()%0xFFFFF)
		}

		a.Sessions[sess.ID] = sess
		logger.Info("✅ [SESSION] Yeni oturum: %s", sess.ID)
	} else {
		// 🚨 DÜZELTME #1: Mevcut oturum varsa eski iptal sinyalini temizle (Double-call önle)
		if sess.Cancel != nil {
			sess.Cancel()
		}
		sess.Cancel = cancel
	}
	a.sessMu.Unlock()

	isEphemeral := strings.HasPrefix(sess.ID, "[WA-ALERT-")

	// 🛡️ RAM Temizliği ve Çıkış Kalkanı
	defer func() {
		cancel()
		if isEphemeral {
			a.sessMu.Lock()
			delete(a.Sessions, sess.ID)
			a.sessMu.Unlock()
			logger.Debug("🧹 RAM Temizliği: Tek seferlik görev [%s] silindi.", sess.ID)
		}
	}()

	sessCtx := context.WithValue(baseCtx, "session_id", sess.ID)
	logger.Debug("👤 User [%s]: %s", sess.ID, input)

	// 🛡️ Kullanıcı Mesajını Ekleme (Atomik)
	sess.mu.Lock()
	sess.History = append(sess.History, kernel.Message{
		Role:    "user",
		Content: input,
		Images:  images,
	})
	a.trimHistory(sess)
	sess.mu.Unlock()

	// 🛡️ Veritabanı Yazma (Zaman Aşımı Korumalı)
	go func(sID, msg string) {
		if a.Memory == nil {
			return
		}
		sqlStore, ok := a.Memory.(*memory.SQLiteStore)
		if !ok {
			return
		}
		memCtx, memCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer memCancel()
		_ = sqlStore.AddChatMessage(memCtx, sID, "user", msg)
	}(sess.ID, input)

	// 🧠 PLAN OLUŞTURMA
	planCtx := context.WithValue(sessCtx, "stream_chan", nil)
	plan, err := a.generatePlan(planCtx, input)

	if err == nil && !strings.Contains(plan, "NO_PLAN_NEEDED") && plan != "" {
		sess.mu.Lock()
		sess.Plan = plan
		sess.mu.Unlock()

		logger.Success("📝 [%s] Harekât Planı:\n%s", sess.ID, plan)
		logger.Info("🚀 Plan onayı LLM'e bırakıldı.")
	}

	a.refreshSystemPrompt(sess)

	// 🛡️ Zamanlayıcı Sızıntısını (Timer Leak) Önleme
	stepTimer := time.NewTimer(time.Second)
	defer stepTimer.Stop()

	maxSteps := a.MaxSteps
	if maxSteps == 0 {
		maxSteps = DefaultMaxSteps
	}

	// 🔄 ANA YAPAY ZEKA DÖNGÜSÜ
	for i := 0; i < maxSteps; i++ {
		stepTimer.Reset(time.Second)

		select {
		case <-sessCtx.Done():
			return fmt.Sprintf("🛑 [%s] İşlem iptal edildi.", sess.ID), nil
		case <-stepTimer.C:
		}

		a.manageContextWindow(sess)
		tools := a.Skills.ListTools()

		sess.mu.Lock()
		currentHistory := make([]kernel.Message, len(sess.History))
		copy(currentHistory, sess.History)
		sess.mu.Unlock()

		// 🚨 DÜZELTME #2: Nil check ekle
		if a.Brain == nil {
			return "", fmt.Errorf("beyin motoru başlatılmadı")
		}

		resp, err := a.Brain.Chat(sessCtx, currentHistory, tools)
		if err != nil {
			return "", fmt.Errorf("beyin hatası: %v", err)
		}

		// 🚨 DÜZELTME #3: Thread-safe BrainResponse erişimi
		if len(resp.GetToolCalls()) == 0 {
			a.checkAndParseToolFallback(resp, i)
		}

		// 🚨 DÜZELTME #4: Thread-safe Content ve ToolCalls okuma
		sess.mu.Lock()
		sess.History = append(sess.History, kernel.Message{
			Role:      "assistant",
			Content:   resp.GetContent(),
			ToolCalls: resp.GetToolCalls(),
		})
		a.trimHistory(sess)
		sess.mu.Unlock()

		// 🚨 DÜZELTME #5: Thread-safe Content okuma
		if resp.GetContent() != "" {
			go func(sID, msg string) {
				if a.Memory == nil {
					return
				}
				sqlStore, ok := a.Memory.(*memory.SQLiteStore)
				if !ok {
					return
				}
				memCtx, memCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer memCancel()
				_ = sqlStore.AddChatMessage(memCtx, sID, "assistant", msg)
			}(sess.ID, resp.GetContent())
		}

		// 🚨 DÜZELTME #6: Thread-safe ToolCalls kontrolü
		if len(resp.GetToolCalls()) == 0 && resp.GetContent() != "" {
			return a.finalizeTask(sess, input, resp.GetContent())
		}

		// 🛠️ ASENKRON ARAÇ ÇALIŞTIRMA MOTORU
		// Büyük projeleri tarayabilmesi için 30 sn yerine 3 Dakika zaman aşımı verdik!
		toolCtx, toolCancel := context.WithTimeout(sessCtx, 3*time.Minute)

		var wg sync.WaitGroup
		sem := make(chan struct{}, MaxParallelTools)

		var toolResultsMu sync.Mutex
		var toolResults []kernel.Message

		// 🚨 DÜZELTME #7: ToolCalls'ı bir kez al, döngüde kullan (thread-safe)
		toolCalls := resp.GetToolCalls()
		
		for _, call := range toolCalls {
			if call.Function == "" {
				continue
			}

			wg.Add(1)
			go func(c kernel.ToolCall) {
				defer wg.Done()

				select {
				case sem <- struct{}{}:
				case <-toolCtx.Done():
					return
				}
				defer func() { <-sem }()

				logger.Action("⚡ [%s] Tool: %s", sess.ID, c.Function)
				output, toolErr := a.executeToolSafe(toolCtx, c)

				var toolImages []string
				if toolErr == nil {
					var parsed map[string]interface{}
					if err := json.Unmarshal([]byte(output), &parsed); err == nil {
						if path, ok := parsed["image_path"].(string); ok {
							// 5MB Limit Koruması
							if info, err := os.Stat(path); err == nil && info.Size() < 5*1024*1024 {
								if imgData, err := os.ReadFile(path); err == nil {
									b64 := base64.StdEncoding.EncodeToString(imgData)
									toolImages = append(toolImages, b64)
								}
							}
						}
					}
				} else {
					output = fmt.Sprintf("❌ HATA: %v\nAraç başarısız oldu.", toolErr)
				}

				// Sonuçları güvenle topla
				toolResultsMu.Lock()
				toolResults = append(toolResults, kernel.Message{
					Role:       "tool",
					Content:    output,
					Name:       c.Function,
					ToolCallID: c.ID,
					Images:     toolImages,
				})
				toolResultsMu.Unlock()
			}(call)
		}

		// Tool'ların bitmesini veya timeout olmasını bekle
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-toolCtx.Done():
			logger.Warn("⏰ [%s] Araç çalıştırma süresi aşıldı (Timeout)", sess.ID)
		}
		toolCancel()

		// Toplanan sonuçları hafızaya ekle
		sess.mu.Lock()
		sess.History = append(sess.History, toolResults...)
		a.trimHistory(sess)
		sess.mu.Unlock()
	}

	return fmt.Sprintf("🛑 [%s] Döngü limiti aşıldı.", sess.ID), nil
}

// trimHistory, mesaj sınırını korurken Sistem Mesajını güvenle muhafaza eder.
func (a *Pars) trimHistory(sess *Session) {
	// 🚨 DÜZELTME #8: Nil check ekle
	if sess == nil {
		return
	}

	if len(sess.History) <= MaxHistoryMessages {
		return
	}

	hasSystem := len(sess.History) > 0 && sess.History[0].Role == "system"
	keepCount := MaxHistoryMessages

	if hasSystem {
		// Sistem mesajını al, aradakileri at, son (keepCount-1) mesajı al
		sysMsg := sess.History[0]
		
		// 🚨 DÜZELTME #9: Slice bounds hatasını önle
		startIdx := len(sess.History) - (keepCount - 1)
		if startIdx < 1 {
			startIdx = 1
		}
		
		tailMsgs := sess.History[startIdx:]
		
		newHistory := make([]kernel.Message, 0, keepCount)
		newHistory = append(newHistory, sysMsg)
		newHistory = append(newHistory, tailMsgs...)
		
		sess.History = newHistory
	} else {
		sess.History = sess.History[len(sess.History)-keepCount:]
	}
}

func (a *Pars) executeToolSafe(ctx context.Context, call kernel.ToolCall) (output string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
			logger.Error("💥 Tool Crash (%s): %v", call.Function, r)
		}
	}()

	// 🚨 DÜZELTME #10: Nil check ekle
	if a.Skills == nil {
		return "", fmt.Errorf("skills engine uninitialized")
	}

	tool, err := a.Skills.GetTool(call.Function)
	if err != nil {
		return "", fmt.Errorf("'%s' tool bulunamadı", call.Function)
	}

	return tool.Execute(ctx, call.Arguments)
}

func (a *Pars) finalizeTask(sess *Session, input, output string) (string, error) {
	// 🚨 DÜZELTME #11: Nil check ekle
	if sess == nil {
		return "", fmt.Errorf("session nil")
	}

	logger.Debug("🎯 [%s] Görev tamamlandı.", sess.ID)

	// 🚨 DÜZELTME #12: Thread-safe Plan okuma
	sess.mu.Lock()
	plan := sess.Plan
	sess.mu.Unlock()

	memoryContent := fmt.Sprintf("GÖREV: %s\nPLAN: %s\nSONUÇ: %s", input, plan, output)

	go func() {
		if a.Memory == nil {
			return
		}
		memCtx, memCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer memCancel()
		
		if err := a.Memory.Add(memCtx, memoryContent, nil); err != nil {
			logger.Error("❌ Memory write failed: %v", err)
		}
	}()

	finalOutput := fmt.Sprintf("[Görev ID: %s]\n%s", sess.ID, output)
	return finalOutput, nil
}

func (a *Pars) CancelSession(sessionID string) {
	a.sessMu.RLock()
	sess, exists := a.Sessions[sessionID]
	a.sessMu.RUnlock()

	if exists && sess.Cancel != nil {
		logger.Warn("💀 KILL SWITCH: %s", sessionID)
		
		// Çift çağrıyı (Double-call) engelle
		sess.mu.Lock()
		cancelFunc := sess.Cancel
		sess.Cancel = nil
		sess.mu.Unlock()
		
		if cancelFunc != nil {
			cancelFunc()
		}
		
		a.sessMu.Lock()
		delete(a.Sessions, sessionID)
		a.sessMu.Unlock()
	}
}
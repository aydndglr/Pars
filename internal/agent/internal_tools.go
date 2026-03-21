package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

type ParsControlTool struct {
	pars *Pars
}

func (t *ParsControlTool) Name() string {
	return "pars_control"
}

func (t *ParsControlTool) Description() string {
	return "Sistemi ve aktif görevleri kontrol eder. 'cancel/iptal' eylemini SADECE kullanıcı açıkça 'görevi iptal et/durdur' dediğinde veya çok kritik bir hata döngüsüne girildiğinde kullan. Kullanıcıya soru sormak veya ondan onay/cevap beklemek için ASLA BU ARACI KULLANMA!"
}

func (t *ParsControlTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action":     map[string]interface{}{"type": "string", "enum": []string{"list", "cancel"}},
			"session_id": map[string]interface{}{"type": "string", "description": "İptal edilecek görevin ID'si (Örn: TSK-1A2B)."},
		},
		"required": []string{"action"},
	}
}

func (t *ParsControlTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	logger.Info("🔧 [ParsControl] Execute çağrıldı, args: %v", args)

	if t.pars == nil {
		logger.Error("❌ [ParsControl] pars instance nil")
		return "", fmt.Errorf("pars instance nil")
	}

	action, _ := args["action"].(string)
	logger.Info("🔧 [ParsControl] Action: %s", action)

	if action == "list" {
		logger.Info("📋 [ParsControl] Görev listesi isteniyor")
		t.pars.sessMu.RLock()
		defer t.pars.sessMu.RUnlock()

		if len(t.pars.sessions) == 0 {
			logger.Info("ℹ️ [ParsControl] Aktif görev bulunamadı")
			return "Şu an aktif çalışan bir görev bulunmuyor.", nil
		}

		logger.Info("ℹ️ [ParsControl] %d aktif görev bulundu", len(t.pars.sessions))
		var res strings.Builder
		res.WriteString("📋 **Aktif Görev Listesi:**\n")

		type sessionInfo struct {
			id        string
			createdAt string
		}
		var sessions []sessionInfo

		for id, sess := range t.pars.sessions {
			sessions = append(sessions, sessionInfo{
				id:        id,
				createdAt: sess.CreatedAt.Format("15:04:05"),
			})
			logger.Debug("📝 [ParsControl] Session eklendi: %s (Başlangıç: %s)", id, sess.CreatedAt.Format("15:04:05"))
		}

		for _, s := range sessions {
			res.WriteString(fmt.Sprintf("- %s (Başlangıç: %s)\n", s.id, s.createdAt))
		}
		logger.Info("✅ [ParsControl] Görev listesi hazırlandı")
		return res.String(), nil
	}

	if action == "cancel" {
		logger.Info("🛑 [ParsControl] Görev iptal isteği")
		sessID, ok := args["session_id"].(string)
		if !ok || sessID == "" {
			logger.Error("❌ [ParsControl] session_id eksik veya boş")
			return "❌ HATA: İptal edilecek session_id belirtilmedi.", nil
		}
		logger.Info("🛑 [ParsControl] İptal edilecek session: %s", sessID)

		t.pars.sessMu.RLock()
		sess, exists := t.pars.sessions[sessID]
		t.pars.sessMu.RUnlock()

		if !exists {
			logger.Error("❌ [ParsControl] Session bulunamadı: %s", sessID)
			return fmt.Sprintf("❌ HATA: '%s' ID'li görev bulunamadı.", sessID), nil
		}
		logger.Info("✅ [ParsControl] Session bulundu: %s", sessID)

		var cancelFunc context.CancelFunc

		sess.mu.Lock()
		if sess.Cancel != nil {
			cancelFunc = sess.Cancel
			sess.Cancel = nil
			logger.Info("🔓 [ParsControl] Session cancel fonksiyonu alındı")
		} else {
			logger.Warn("⚠️ [ParsControl] Session cancel fonksiyonu nil")
		}
		sess.mu.Unlock()

		if cancelFunc != nil {
			cancelFunc()
			logger.Warn("💀 [ParsControl] [%s] görevi kullanıcı tarafından iptal edildi.", sessID)
			return fmt.Sprintf("✅ BAŞARILI: [%s] görevine iptal sinyali gönderildi. Görev durduruluyor.", sessID), nil
		}

		logger.Warn("⚠️ [ParsControl] [%s] görevi zaten durdurulmuş veya iptal edilmiş.", sessID)
		return fmt.Sprintf("⚠️ [%s] görevi zaten durdurulmuş veya iptal edilmiş.", sessID), nil
	}

	logger.Error("❌ [ParsControl] Geçersiz action: %s", action)
	return "Geçersiz işlem. 'list' veya 'cancel' kullanın.", nil
}

type DelegateTaskTool struct {
	pars *Pars
}

func (t *DelegateTaskTool) Name() string {
	return "delegate_task"
}

func (t *DelegateTaskTool) Description() string {
	return "Uzun sürecek, yoğun takip ve analiz gerektiren, büyük veri setleri üzerinde çalışılacak veya zaman alıcı alt görevleri (log izleme, uzun süreli taramalar, kapsamlı kod/görsel analizleri vb.) İkincil Beyne (İşçiye) devreder. Ana zihin (Stratejist) olarak sen planlamaya odaklanırken, vakit kaybettiren tüm amelelik işlerini bu araca pasla."
}

func (t *DelegateTaskTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"task":   map[string]interface{}{"type": "string", "description": "İşçiye yaptırılacak detaylı ve net görev tanımı."},
			"images": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Görevle ilgili görsel analizi gerekirse URL veya base64 listesi."},
		},
		"required": []string{"task"},
	}
}

func (t *DelegateTaskTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	logger.Info("🐯 [DelegateTask] Execute çağrıldı, args: %v", args)

	if t.pars == nil {
		logger.Error("❌ [DelegateTask] pars instance nil")
		return "", fmt.Errorf("pars instance nil")
	}

	task, ok := args["task"].(string)
	if !ok || task == "" {
		logger.Error("❌ [DelegateTask] task parametresi eksik veya boş")
		return "", fmt.Errorf("task parametresi eksik veya boş")
	}
	logger.Info("📝 [DelegateTask] Task: %s", task)

	var images []string
	if rawImages, ok := args["images"].([]interface{}); ok {
		for _, rawImg := range rawImages {
			if imgStr, isStr := rawImg.(string); isStr {
				images = append(images, imgStr)
				logger.Debug("🖼️ [DelegateTask] Image eklendi: %d karakter", len(imgStr))
			}
		}
	}
	logger.Info("🖼️ [DelegateTask] Toplam %d image", len(images))

	if t.pars.SecondaryBrain == nil {
		logger.Warn("⚠️ [DelegateTask] İkincil beyin yapılandırılmamış, görev ana beyinde çalıştırılıyor.")
		return t.executeOnPrimary(ctx, task, images)
	}
	logger.Info("🧠 [DelegateTask] SecondaryBrain mevcut, delegasyon yapılacak")

	if len(images) == 0 {
		logger.Debug("🔍 [DelegateTask] Image yok, session'dan çekiliyor...")
		if sessID, ok := ctx.Value("session_id").(string); ok {
			t.pars.sessMu.RLock()
			if sess, exists := t.pars.sessions[sessID]; exists {
				sess.mu.Lock()
				if sess.History != nil {
					for i := len(sess.History) - 1; i >= 0; i-- {
						if sess.History[i].Role == "user" && len(sess.History[i].Images) > 0 {
							images = sess.History[i].Images
							logger.Info("📸 [DelegateTask] Mevcut oturumdan %d görsel İşçi'ye (Worker) gizlice aktarıldı.", len(images))
							break
						}
					}
				}
				sess.mu.Unlock()
			}
			t.pars.sessMu.RUnlock()
		}
	}

	logger.Info("🐯 [DelegateTask] Görev İşçiye Delege Ediliyor: %s", task)

	workerHistory := []kernel.Message{
		{
			Role:    "system",
			Content: "Sen bir veri ve görsel analiz motorusun. Sana verilen görevi ve gönderilen görselleri en ince ayrıntısına kadar analiz et. Sadece sonuca, gördüklerine ve teknik doğruluğa odaklanarak rapor ver.",
		},
		{
			Role:    "user",
			Content: task,
			Images:  images,
		},
	}
	logger.Debug("📝 [DelegateTask] Worker history hazırlandı: %d mesaj", len(workerHistory))

	workerCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	logger.Debug("⏱️ [DelegateTask] Worker context timeout: 5 dakika")

	logger.Info("🧠 [DelegateTask] SecondaryBrain.Chat çağrılıyor...")
	resp, err := t.pars.SecondaryBrain.Chat(workerCtx, workerHistory, nil)
	if err != nil {
		logger.Error("❌ [DelegateTask] İşçi beyin hatası: %v", err)
		return "", fmt.Errorf("işçi beyin hatası: %v", err)
	}
	logger.Info("✅ [DelegateTask] İşçi beyin yanıtı alındı: %d karakter", len(resp.Content))

	logger.Success("✅ İşçi (Vision) görevini tamamladı ve raporunu sundu.")

	return fmt.Sprintf("📝 **İşçi Beyin (Worker) Raporu:**\n\n%s", resp.Content), nil
}

func (t *DelegateTaskTool) executeOnPrimary(ctx context.Context, task string, images []string) (string, error) {
	logger.Info("🧠 [DelegateTask] executeOnPrimary çağrıldı, PrimaryBrain kullanılacak")

	if t.pars.Brain == nil {
		logger.Error("❌ [DelegateTask] primary brain nil")
		return "", fmt.Errorf("primary brain nil")
	}

	workerHistory := []kernel.Message{
		{
			Role:    "system",
			Content: "Sen bir veri ve görsel analiz motorusun. Sana verilen görevi detaylıca analiz et ve rapor ver.",
		},
		{
			Role:    "user",
			Content: task,
			Images:  images,
		},
	}
	logger.Debug("📝 [DelegateTask] Primary history hazırlandı: %d mesaj", len(workerHistory))

	logger.Info("🧠 [DelegateTask] PrimaryBrain.Chat çağrılıyor...")
	resp, err := t.pars.Brain.Chat(ctx, workerHistory, nil)
	if err != nil {
		logger.Error("❌ [DelegateTask] ana beyin hatası: %v", err)
		return "", fmt.Errorf("ana beyin hatası: %v", err)
	}
	logger.Info("✅ [DelegateTask] Ana beyin yanıtı alındı: %d karakter", len(resp.Content))

	return fmt.Sprintf("📝 **Ana Beyin Raporu (Secondary yok):**\n\n%s", resp.Content), nil
}
// internal/agent/internal_tools.go
// 🚀 DÜZELTMELER: Nil checks, Thread-safety, Error handling, Validation
// ⚠️ DİKKAT: execution.go, session.go ile %100 uyumlu

package agent

import (
	"context"
	"fmt"
	"strings"
	"time"
	"github.com/aydndglr/pars-agent-v3/internal/core/kernel"
	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

// =====================================================================
// 🚀 PARS CONTROL: Oturum ve Süreç Yönetimi
// =====================================================================

// ParsControlTool: Pars'ın kendi oturumlarını ve süreçlerini yönetmesi için kontrol aracı
type ParsControlTool struct {
	pars *Pars
}

func (t *ParsControlTool) Name() string { return "pars_control" }

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
	// 🚨 DÜZELTME #1: Nil kontrolü
	if t.pars == nil {
		return "", fmt.Errorf("pars instance nil")
	}

	action, _ := args["action"].(string)

	if action == "list" {
		t.pars.sessMu.RLock()
		defer t.pars.sessMu.RUnlock()

		if len(t.pars.Sessions) == 0 {
			return "Şu an aktif çalışan bir görev bulunmuyor.", nil
		}

		var res strings.Builder
		res.WriteString("📋 **Aktif Görev Listesi:**\n")
		
		// 🚨 DÜZELTME #2: Session verilerini lock içinde güvenli şekilde kopyala
		type sessionInfo struct {
			id        string
			createdAt string
		}
		var sessions []sessionInfo
		
		for id, sess := range t.pars.Sessions {
			sessions = append(sessions, sessionInfo{
				id:        id,
				createdAt: sess.CreatedAt.Format("15:04:05"),
			})
		}
		
		for _, s := range sessions {
			res.WriteString(fmt.Sprintf("- %s (Başlangıç: %s)\n", s.id, s.createdAt))
		}
		return res.String(), nil
	}

	if action == "cancel" {
		sessID, ok := args["session_id"].(string)
		// 🚨 DÜZELTME #3: Type assertion kontrolü
		if !ok || sessID == "" {
			return "❌ HATA: İptal edilecek session_id belirtilmedi.", nil
		}

		// 🚨 DÜZELTME #4: Session'ı bul ve Cancel'ı güvenli şekilde çağır
		t.pars.sessMu.RLock()
		sess, exists := t.pars.Sessions[sessID]
		t.pars.sessMu.RUnlock()

		if !exists {
			return fmt.Sprintf("❌ HATA: '%s' ID'li görev bulunamadı.", sessID), nil
		}

		// 🚨 DÜZELTME #5: Double-call önleme (execution.go'daki CancelSession ile aynı mantık)
		var cancelFunc context.CancelFunc
		
		sess.mu.Lock()
		if sess.Cancel != nil {
			cancelFunc = sess.Cancel
			sess.Cancel = nil // Tekrar çağrılmasını önle
		}
		sess.mu.Unlock()

		if cancelFunc != nil {
			cancelFunc()
			logger.Warn("💀 [ParsControl] [%s] görevi kullanıcı tarafından iptal edildi.", sessID)
			return fmt.Sprintf("✅ BAŞARILI: [%s] görevine iptal sinyali gönderildi. Görev durduruluyor.", sessID), nil
		}

		return fmt.Sprintf("⚠️ [%s] görevi zaten durdurulmuş veya iptal edilmiş.", sessID), nil
	}

	return "Geçersiz işlem. 'list' veya 'cancel' kullanın.", nil
}

// =====================================================================
// 🚀 DELEGATE TASK: İkincil Beyne (Worker) Görev Paslama
// =====================================================================

// DelegateTaskTool: Uzun süren görevleri secondary brain'a devretmek için kullanılır
type DelegateTaskTool struct {
	pars *Pars
}

func (t *DelegateTaskTool) Name() string { return "delegate_task" }

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
	// 🚨 DÜZELTME #6: Nil kontrolü
	if t.pars == nil {
		return "", fmt.Errorf("pars instance nil")
	}

	task, ok := args["task"].(string)
	// 🚨 DÜZELTME #7: Task validation
	if !ok || task == "" {
		return "", fmt.Errorf("task parametresi eksik veya boş")
	}

	// JSON'dan dönen array'i güvenli şekilde yakalama
	var images []string
	if rawImages, ok := args["images"].([]interface{}); ok {
		for _, rawImg := range rawImages {
			if imgStr, isStr := rawImg.(string); isStr {
				images = append(images, imgStr)
			}
		}
	}

	// 🚨 DÜZELTME #8: SecondaryBrain nil kontrolü
	if t.pars.SecondaryBrain == nil {
		logger.Warn("⚠️ [DelegateTask] İkincil beyin yapılandırılmamış, görev ana beyinde çalıştırılıyor.")
		// 🆕 Fallback: Secondary yoksa ana beyinde çalıştır
		return t.executeOnPrimary(ctx, task, images)
	}

	// 👁️ GÖRÜ YAMASI: Eğer Pars manuel olarak görsel vermediyse, mevcut oturumdan otomatik çek
	if len(images) == 0 {
		if sessID, ok := ctx.Value("session_id").(string); ok {
			t.pars.sessMu.RLock()
			if sess, exists := t.pars.Sessions[sessID]; exists {
				sess.mu.Lock()
				// 🚨 DÜZELTME #9: History nil kontrolü
				if sess.History != nil {
					// Geçmişteki en son kullanıcı mesajından (genelde resim ordadır) resimleri topla
					for i := len(sess.History) - 1; i >= 0; i-- {
						if sess.History[i].Role == "user" && len(sess.History[i].Images) > 0 {
							images = sess.History[i].Images
							logger.Info("📸 Mevcut oturumdan %d görsel İşçi'ye (Worker) gizlice aktarıldı.", len(images))
							break
						}
					}
				}
				sess.mu.Unlock()
			}
			t.pars.sessMu.RUnlock()
		}
	}

	logger.Info("🐯 Görev İşçiye Delege Ediliyor: %s", task)

	// İşçi için steril ve odaklı bir bağlam hazırlıyoruz
	workerHistory := []kernel.Message{
		{
			Role:    "system",
			Content: "Sen bir veri ve görsel analiz motorusun. Sana verilen görevi ve gönderilen görselleri en ince ayrıntısına kadar analiz et. Sadece sonuca, gördüklerine ve teknik doğruluğa odaklanarak rapor ver.",
		},
		{
			Role:    "user",
			Content: task,
			Images:  images, // Base64 verileri
		},
	}

	// 🚨 DÜZELTME #10: Context timeout ekle (worker için 5 dakika limit)
	workerCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// İşçi beyni koştur
	resp, err := t.pars.SecondaryBrain.Chat(workerCtx, workerHistory, nil)
	if err != nil {
		logger.Error("❌ [DelegateTask] İşçi beyin hatası: %v", err)
		return "", fmt.Errorf("işçi beyin hatası: %v", err)
	}

	logger.Success("✅ İşçi (Vision) görevini tamamladı ve raporunu sundu.")

	return fmt.Sprintf("📝 **İşçi Beyin (Worker) Raporu:**\n\n%s", resp.Content), nil
}

// 🆕 YENİ: SecondaryBrain yoksa primary'de çalıştır (Fallback)
func (t *DelegateTaskTool) executeOnPrimary(ctx context.Context, task string, images []string) (string, error) {
	if t.pars.Brain == nil {
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

	resp, err := t.pars.Brain.Chat(ctx, workerHistory, nil)
	if err != nil {
		return "", fmt.Errorf("ana beyin hatası: %v", err)
	}

	return fmt.Sprintf("📝 **Ana Beyin Raporu (Secondary yok):**\n\n%s", resp.Content), nil
}
package rag

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/memory"
)

type ChatRecallTool struct {
	Store *memory.SQLiteStore
}

func (t *ChatRecallTool) Name() string {
	return "recall_past_chat"
}

func (t *ChatRecallTool) Description() string {
	return "Kullanıcının geçmişteki sohbet oturumlarını gün bazlı aramak ve o oturumdaki tüm konuşmaları bağlama (context) geri yüklemek için kullanılır. 'action' parametresi 'list_by_date' veya 'load_session' olabilir."
}

func (t *ChatRecallTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Yapılacak işlem: 'list_by_date' (Belirli bir gündeki sohbetleri listeler) veya 'load_session' (Belirli bir oturumun tüm mesajlarını getirir)",
				"enum":        []string{"list_by_date", "load_session"},
			},
			"date": map[string]interface{}{
				"type":        "string",
				"description": "'list_by_date' için tarih (YYYY-MM-DD formatında). Bugün ise 'today' yazılabilir.",
			},
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "'load_session' için yüklenecek olan oturumun kimliği (Örn: CLI-12345)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *ChatRecallTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	action, ok := args["action"].(string)
	if !ok {
		return "", fmt.Errorf("action parametresi eksik veya hatalı")
	}

	if t.Store == nil {
		return "", fmt.Errorf("hafıza bağlantısı kurulamadı")
	}

	switch action {
	case "list_by_date":
		dateStr, _ := args["date"].(string)
		
		if dateStr == "today" || dateStr == "" {
			dateStr = time.Now().Format("2006-01-02")
		}

		sessions, err := t.Store.GetSessionsByDate(ctx, dateStr)
		if err != nil {
			return "", fmt.Errorf("sohbetler aranırken hata oluştu: %v", err)
		}

		if len(sessions) == 0 {
			return fmt.Sprintf("🗓️ %s tarihinde kaydedilmiş hiçbir sohbet bulunamadı.", dateStr), nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("🗓️ %s Tarihindeki Sohbetlerin Listesi:\n", dateStr))
		for _, s := range sessions {
			sb.WriteString(fmt.Sprintf("🔑 ID: %s | 💬 Mesaj Sayısı: %d\n", s.SessionID, s.MessageCount))
			sb.WriteString(fmt.Sprintf("📌 %s\n", s.LastActive))
			sb.WriteString(strings.Repeat("-", 40) + "\n")
		}
		sb.WriteString("\n[SİSTEM BİLGİSİ]: Kullanıcıya bu başlıkları sun ve hangisine dönmek istediğini sor. Ardından 'load_session' action'ı ile o ID'yi çağır.")

		return sb.String(), nil

	case "load_session":
		sessionID, ok := args["session_id"].(string)
		if !ok || sessionID == "" {
			return "", fmt.Errorf("session_id parametresi eksik")
		}

		msgs, err := t.Store.GetSessionChat(ctx, sessionID)
		if err != nil {
			return "", fmt.Errorf("sohbet yüklenirken hata oluştu: %v", err)
		}

		if len(msgs) == 0 {
			return "", fmt.Errorf("bu ID'ye ait sohbet bulunamadı")
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("📂 [%s] OTURUM GEÇMİŞİ BAŞARIYLA YÜKLENDİ\n", sessionID))
		sb.WriteString("====================================================\n")
		for _, m := range msgs {
			role := "👤 Kullanıcı"
			if m.Role == "assistant" {
				role = "🐯 Pars"
			}
			sb.WriteString(fmt.Sprintf("%s (%s):\n%s\n\n", role, m.CreatedAt, m.Content))
		}
		sb.WriteString("====================================================\n")
		sb.WriteString("[SİSTEM BİLGİSİ]: Yukarıdaki sohbet senin geçmişte yaptığın bir konuşmadır. Artık konuya tamamen hakimsin. Kullanıcıya 'Geçmişi hatırladım, kaldığımız yerden devam edelim' diyebilirsin.")

		return sb.String(), nil

	default:
		return "", fmt.Errorf("geçersiz action: %s", action)
	}
}
package network

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
)

type StackOverflowTool struct{}

func (t *StackOverflowTool) Name() string { return "so_search" }

func (t *StackOverflowTool) Description() string {
	return "StackOverflow üzerinde hata kodlarını veya algoritmaları araştırır. Linklerle birlikte doğrudan KABUL EDİLEN ÇÖZÜMÜN (Accepted Answer) kodunu ve metnini getirir. Hızlı sorun çözümü için mükemmeldir."
}

func (t *StackOverflowTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string", "description": "Aranacak hata mesajı veya soru (Örn: 'golang panic index out of range')"},
			"tags":  map[string]interface{}{"type": "string", "description": "İsteğe bağlı etiketler, virgülle ayır (Örn: 'go,python')"},
		},
		"required": []string{"query"},
	}
}

// 🛡️ API YANIT YAPILARI
type SOQuestion struct {
	QuestionID       int    `json:"question_id"`
	Title            string `json:"title"`
	Link             string `json:"link"`
	IsAnswered       bool   `json:"is_answered"`
	Score            int    `json:"score"`
	AcceptedAnswerID int    `json:"accepted_answer_id"`
}

type SOAnswer struct {
	AnswerID int    `json:"answer_id"`
	Score    int    `json:"score"`
	Body     string `json:"body"`
}

func (t *StackOverflowTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	query, _ := args["query"].(string)
	tags, _ := args["tags"].(string)

	logger.Action("🔍 SO İstihbaratı Aranıyor: '%s'", query)

	// 1. AŞAMA: SORULARI ARA
	searchURL := fmt.Sprintf("https://api.stackexchange.com/2.3/search/advanced?order=desc&sort=relevance&q=%s&site=stackoverflow", url.QueryEscape(query))
	if tags != "" {
		searchURL += "&tagged=" + url.QueryEscape(tags)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("StackOverflow API'ye ulaşılamadı: %v", err)
	}
	defer resp.Body.Close()

	var searchResult struct {
		Items []SOQuestion `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&searchResult); err != nil {
		return "", fmt.Errorf("API arama yanıtı okunamadı: %v", err)
	}

	if len(searchResult.Items) == 0 {
		return "📭 StackOverflow'da bu sorguya uygun bir sonuç bulunamadı balım. Başka kelimelerle veya hata kodunun sadece ana kısmıyla dene.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🔍 StackOverflow Sonuçları: '%s'\n", query))
	sb.WriteString(strings.Repeat("-", 60) + "\n")

	// 2. AŞAMA: EN İYİ ÇÖZÜMÜ (ACCEPTED ANSWER) BUL VE ÇEK
	var bestAnswerBody string
	var bestAnswerTitle string

	// İlk 3 soru içinde "Kabul Edilmiş Cevap" (Accepted Answer) arıyoruz
	for _, item := range searchResult.Items {
		if item.AcceptedAnswerID != 0 {
			answerURL := fmt.Sprintf("https://api.stackexchange.com/2.3/answers/%d?order=desc&sort=activity&site=stackoverflow&filter=withbody", item.AcceptedAnswerID)
			
			ansReq, _ := http.NewRequestWithContext(ctx, "GET", answerURL, nil)
			ansResp, ansErr := client.Do(ansReq)
			
			if ansErr == nil {
				var answerResult struct {
					Items []SOAnswer `json:"items"`
				}
				json.NewDecoder(ansResp.Body).Decode(&answerResult)
				ansResp.Body.Close()

				if len(answerResult.Items) > 0 {
					bestAnswerTitle = item.Title
					bestAnswerBody = cleanHTMLToMarkdown(answerResult.Items[0].Body)
					break // İlk ve en iyi çözümü bulduk, döngüden çık!
				}
			}
		}
	}

	// 3. AŞAMA: RAPORU HAZIRLA (LLM İçin Optimizasyon)
	if bestAnswerBody != "" {
		sb.WriteString(fmt.Sprintf("🏆 EN İYİ ÇÖZÜM BULUNDU (Kabul Edilen Yanıt)\n"))
		sb.WriteString(fmt.Sprintf("Soru: %s\n\n", bestAnswerTitle))
		sb.WriteString(bestAnswerBody)
		sb.WriteString("\n\n" + strings.Repeat("-", 60) + "\n")
	} else {
		sb.WriteString("⚠️ Bu konuyla ilgili doğrudan kabul edilmiş bir çözüm bulunamadı, ancak aşağıdaki tartışmalar faydalı olabilir.\n\n")
	}

	sb.WriteString("DİĞER ALAKALI BAŞLIKLAR:\n")
	limit := 4
	for i, item := range searchResult.Items {
		if i >= limit { break }
		status := "❌ Çözümsüz"
		if item.IsAnswered { status = "✅ Çözüldü" }
		
		sb.WriteString(fmt.Sprintf("%d. [%s] (Oy: %d) - %s\n   🔗 %s\n", i+1, status, item.Score, item.Title, item.Link))
	}

	return sb.String(), nil
}

// 🛡️ HTML TO MARKDOWN CLEANER (LLM Zırhı)
// StackOverflow API'den gelen HTML body'sini, Pars'in beynini yormayacak
// saf bir Markdown (Code snippet) formatına çevirir.
func cleanHTMLToMarkdown(htmlStr string) string {
	// 1. Kod bloklarını korumaya al
	s := strings.ReplaceAll(htmlStr, "<pre><code>", "\n```python\n") // Genel bir markdown belirteci koyuyoruz
	s = strings.ReplaceAll(s, "</code></pre>", "\n```\n")
	s = strings.ReplaceAll(s, "<code>", "`")
	s = strings.ReplaceAll(s, "</code>", "`")
	
	// 2. Paragraf ve boşlukları ayarla
	s = strings.ReplaceAll(s, "<p>", "")
	s = strings.ReplaceAll(s, "</p>", "\n\n")
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	
	// 3. Geriye kalan tüm pis HTML etiketlerini (<a>, <strong> vb) sil
	re := regexp.MustCompile(`<[^>]*>`)
	s = re.ReplaceAllString(s, "")
	
	// 4. HTML karakterlerini (örn: &lt; -> < ) normale çevir
	s = html.UnescapeString(s)
	
	// İçerik çok uzunsa Pars'in jetonlarını patlatmamak için kırp (Guardrail)
	if len(s) > 3000 {
		s = s[:3000] + "\n\n...(Çözümün devamı çok uzun olduğu için kesildi)..."
	}

	return strings.TrimSpace(s)
}
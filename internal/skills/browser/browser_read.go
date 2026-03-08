package browser

import (
	"context"
	"fmt"
	"strings"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/chromedp/chromedp"
)

func (t *BrowserTool) doRead(ctx context.Context, args map[string]interface{}) (string, error) {
	targetUrl, _ := args["url"].(string)
	if targetUrl == "" {
		return "", fmt.Errorf("HATA: 'read' modu için 'url' parametresi zorunludur")
	}

	// 🚀 V6 TURBO: İzole sekme
	cCtx, cancel := GetChromeTab(ctx)
	defer cancel()

	var title, body string
	logger.Action("📖 Sayfa Okunuyor: %s", targetUrl)

	err := chromedp.Run(cCtx,
		chromedp.Navigate(targetUrl),
		// 🧠 AKILLI BEKLEME: Sabit 3 saniye yerine, DOM state 'complete' olana kadar Poll (Anket) yap!
		chromedp.Poll(`document.readyState === 'complete'`, nil),
		chromedp.Title(&title),
		chromedp.Evaluate(`
			(() => {
				const clone = document.body.cloneNode(true);
				// Gereksiz etiketleri (reklam, menü, kod blokları) çöpe at
				['script', 'style', 'nav', 'footer', 'iframe', 'noscript'].forEach(tag => clone.querySelectorAll(tag).forEach(el => el.remove()));
				return clone.innerText;
			})()
		`, &body),
	)

	if err != nil {
		return "", fmt.Errorf("okuma hatası: %v", err)
	}

	// Metni temizle ve LLM'in jetonlarını sömürmemesi için kırp
	cleanBody := strings.Join(strings.Fields(body), " ")
	if len(cleanBody) > 3000 {
		cleanBody = cleanBody[:3000] + "\n...(Makalenin devamı çok uzun olduğu için kırpıldı)..."
	}
	
	return fmt.Sprintf("📄 BAŞLIK: %s\n\nİÇERİK:\n%s", title, cleanBody), nil
}
package browser

import (
	"context"
	"fmt"
	"net/url"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/chromedp/chromedp"
)

func (t *BrowserTool) doSearch(ctx context.Context, args map[string]interface{}) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("HATA: 'search' modu için 'query' parametresi zorunludur")
	}

	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))
	
	// 🚀 V6 TURBO: Yeni bir sekme (Tab) iste (Sıfırdan Chrome açmaz!)
	cCtx, cancel := GetChromeTab(ctx)
	defer cancel() // İş bitince sekmeyi kapat

	var res string
	logger.Action("🔍 Arama Motoru: %s", query)

	jsEval := `
		(() => {
			if (document.body.innerText.includes("If this is not a bot")) return "BOT_BLOCKED";
			return Array.from(document.querySelectorAll('.result')).slice(0, 5).map(el => {
				const title = el.querySelector('.result__title')?.innerText.trim() || '';
				const link = el.querySelector('.result__a')?.href || '';
				const snippet = el.querySelector('.result__snippet')?.innerText.trim() || '';
				return title ? ("### " + title + "\n🔗 " + link + "\n📝 " + snippet + "\n") : "";
			}).join("\n");
		})()
	`
	
	err := chromedp.Run(cCtx,
		chromedp.Navigate(searchURL),
		// 🧠 AKILLI BEKLEME: Sayfanın gövdesi render olana kadar bekle, sonra ak!
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Evaluate(jsEval, &res),
	)

	if err != nil {
		return "", fmt.Errorf("arama başarısız: %v", err)
	}
	if res == "BOT_BLOCKED" {
		return "⚠️ Arama motoru bot korumasına takıldı. Lütfen daha belirgin bir kelimeyle tekrar dene.", nil
	}
	if res == "" {
		return "⚠️ Hiçbir sonuç bulunamadı.", nil
	}

	return fmt.Sprintf("🔍 ARAMA SONUÇLARI (%s):\n\n%s", query, res), nil
}
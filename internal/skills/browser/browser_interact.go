package browser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/chromedp/chromedp"
)

func (t *BrowserTool) doInteract(ctx context.Context, args map[string]interface{}) (string, error) {
	targetUrl, _ := args["url"].(string)
	device, _ := args["device"].(string) // Opsiyonel: "mobile"

	actionsRaw, ok := args["actions"].([]interface{})
	if !ok {
		// Fallback: Eski stil tek action atarsa kurtar
		if singleAction, hasSingle := args["action"].(string); hasSingle {
			actionsRaw = []interface{}{map[string]interface{}{"action": singleAction, "selector": args["selector"], "value": args["value"]}}
		} else {
			return "", fmt.Errorf("HATA: 'interact' modu için 'actions' dizisi gereklidir")
		}
	}

	// 🚀 V6 TURBO: Sihirli geçiş!
	cCtx, cancel := GetChromeTab(ctx)
	defer cancel()

	var report strings.Builder
	report.WriteString(fmt.Sprintf("🐯 PARS WEB MAKRO RAPORU\n%s\n", strings.Repeat("=", 30)))

	// 1. EKRAN BOYUTU VE URL
	var initTasks []chromedp.Action
	
	if device == "mobile" {
		logger.Action("📱 Mobil Görünüm (375x812) aktif ediliyor.")
		initTasks = append(initTasks, chromedp.EmulateViewport(375, 812))
	} else {
		logger.Action("💻 Masaüstü Görünüm (1920x1080) aktif ediliyor.")
		initTasks = append(initTasks, chromedp.EmulateViewport(1920, 1080))
	}

	if targetUrl != "" {
		logger.Action("🌐 Tarayıcı Yönlendiriliyor: %s", targetUrl)
		initTasks = append(initTasks, chromedp.Navigate(targetUrl))
		report.WriteString(fmt.Sprintf("📍 Hedef: %s\n", targetUrl))
	}

	if len(initTasks) > 0 {
		if err := chromedp.Run(cCtx, initTasks...); err != nil {
			return "", fmt.Errorf("URL/Viewport ayarı başarısız: %v", err)
		}
	}

	// 2. MAKRO ADIMLARI
	for i, actRaw := range actionsRaw {
		act, ok := actRaw.(map[string]interface{})
		if !ok { continue }

		action, _ := act["action"].(string)
		selector, _ := act["selector"].(string)
		value, _ := act["value"].(string)

		var stepTasks chromedp.Tasks
		var screenshotBuf []byte
		var resultVar interface{}

		switch action {
		case "click":
			logger.Action("🖱️ [%d] Tıklanıyor: %s", i+1, selector)
			js := fmt.Sprintf(`(function(){ let el = document.querySelector("%s"); if(el) el.click(); })()`, selector)
			stepTasks = append(stepTasks, chromedp.Evaluate(js, nil))

		case "type":
			logger.Action("⌨️ [%d] Yazılıyor (%s): %s", i+1, selector, value)
			js := fmt.Sprintf(`(function(){ 
				let el = document.querySelector("%s"); 
				if(!el) return; 
				el.focus(); el.value = "%s"; 
				el.dispatchEvent(new Event('input', {bubbles: true})); 
				el.dispatchEvent(new Event('change', {bubbles: true})); 
			})()`, selector, value)
			stepTasks = append(stepTasks, chromedp.Evaluate(js, nil))

		case "enter":
			logger.Action("⌨️ [%d] Enter basılıyor: %s", i+1, selector)
			js := fmt.Sprintf(`(function(){ 
				let el = document.querySelector("%s"); 
				if(!el) return; 
				if(el.form) { el.form.submit(); return; }
				let ev = new KeyboardEvent('keydown', {key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true});
				el.dispatchEvent(ev);
			})()`, selector)
			stepTasks = append(stepTasks, chromedp.Evaluate(js, nil))

		case "hover":
			logger.Action("👆 [%d] Hover: %s", i+1, selector)
			js := fmt.Sprintf(`(function(){ let el = document.querySelector("%s"); if(el) { el.dispatchEvent(new MouseEvent('mouseover', {bubbles: true})); el.dispatchEvent(new MouseEvent('mouseenter', {bubbles: true})); }})()`, selector)
			stepTasks = append(stepTasks, chromedp.Evaluate(js, nil))

		case "scroll":
			logger.Action("📜 [%d] Kaydırılıyor: %s", i+1, selector)
			stepTasks = append(stepTasks, chromedp.ScrollIntoView(selector))

		case "wait":
			waitSec := 2
			fmt.Sscanf(value, "%d", &waitSec)
			logger.Action("⏳ [%d] Bekleniyor: %d sn", i+1, waitSec)
			stepTasks = append(stepTasks, chromedp.Sleep(time.Duration(waitSec)*time.Second))

		case "wait_vanish":
			logger.Action("👻 [%d] Kaybolması bekleniyor: %s", i+1, selector)
			stepTasks = append(stepTasks, chromedp.WaitNotVisible(selector))

		case "select":
			logger.Action("🔽 [%d] Seçiliyor: %s -> %s", i+1, selector, value)
			js := fmt.Sprintf(`(function(){ let el = document.querySelector("%s"); if(el){ el.value="%s"; el.dispatchEvent(new Event("change", {bubbles: true})); }})()`, selector, value)
			stepTasks = append(stepTasks, chromedp.Evaluate(js, nil))

		case "upload":
			absPath, err := filepath.Abs(value)
			if err == nil {
				logger.Action("📤 [%d] Yükleniyor: %s", i+1, absPath)
				stepTasks = append(stepTasks, chromedp.WaitVisible(selector), chromedp.SetUploadFiles(selector, []string{absPath}))
			}

		case "js_eval":
			logger.Action("⚙️ [%d] JS Koşturuluyor: %s", i+1, value)
			stepTasks = append(stepTasks, chromedp.Evaluate(value, &resultVar))

		case "get_text":
			logger.Action("📖 [%d] Metin okunuyor: %s", i+1, selector)
			var textVal string
			stepTasks = append(stepTasks, chromedp.WaitVisible(selector), chromedp.Text(selector, &textVal))
			resultVar = &textVal

		case "multi_text":
			logger.Action("📚 [%d] Toplu metin çekiliyor: %s", i+1, selector)
			js := fmt.Sprintf(`Array.from(document.querySelectorAll("%s")).map(el => el.innerText.trim())`, selector)
			var listVal []string
			stepTasks = append(stepTasks, chromedp.Evaluate(js, &listVal))
			resultVar = &listVal

		case "get_attr":
			logger.Action("🔍 [%d] Öznitelik (%s): %s", i+1, value, selector)
			var attrVal string
			var okAttr bool
			stepTasks = append(stepTasks, chromedp.WaitVisible(selector), chromedp.AttributeValue(selector, value, &attrVal, &okAttr))
			resultVar = &attrVal

		case "screenshot":
			if value == "" { value = fmt.Sprintf("web_snap_%d.png", time.Now().Unix()) }
			logger.Action("📸 [%d] SS Alınıyor: %s", i+1, value)
			stepTasks = append(stepTasks, chromedp.CaptureScreenshot(&screenshotBuf))
		}

		// 🛡️ HER ADIM İÇİN 15 SANİYE ZAMAN AŞIMI
		stepCtx, stepCancel := context.WithTimeout(cCtx, 15*time.Second)
		err := chromedp.Run(stepCtx, stepTasks)
		stepCancel() 

		if err != nil {
			errStr := fmt.Errorf("Adım %d (%s) Başarısız: %v", i+1, action, err)
			report.WriteString(fmt.Sprintf("❌ %s\n", errStr.Error()))
			return report.String(), errStr
		}

		report.WriteString(fmt.Sprintf("✅ Adım %d [%s] Tamamlandı.\n", i+1, action))

		// Çıktıları İşle
		if action == "screenshot" && len(screenshotBuf) > 0 {
			savePath := filepath.Join("logs", value)
			os.MkdirAll("logs", 0755)
			os.WriteFile(savePath, screenshotBuf, 0644)
			report.WriteString(fmt.Sprintf("   📸 SS Kaydedildi: %s\n", savePath))
		}
		if resultVar != nil {
			switch v := resultVar.(type) {
			case *string: report.WriteString(fmt.Sprintf("   📄 Çıktı: %s\n", *v))
			case *[]string:
				report.WriteString(fmt.Sprintf("   📚 Çıktı (%d öğe):\n", len(*v)))
				for idx, item := range *v { report.WriteString(fmt.Sprintf("      %d. %s\n", idx+1, item)) }
			default: report.WriteString(fmt.Sprintf("   📊 Çıktı: %v\n", v))
			}
		}
	}

	var finalURL, finalTitle string
	chromedp.Run(cCtx, chromedp.Location(&finalURL), chromedp.Title(&finalTitle))
	report.WriteString(strings.Repeat("-", 30) + "\n")
	report.WriteString(fmt.Sprintf("🏁 Son Durak: %s\n📜 Başlık: %s", finalURL, finalTitle))

	return report.String(), nil
}
package browser

import (
	"context"
	"os"
	"runtime"
	"sync"

	"github.com/aydndglr/pars-agent-v3/internal/core/logger"
	"github.com/chromedp/chromedp"
)

var (
	allocCtx    context.Context
	allocCancel context.CancelFunc
	browserMu   sync.Mutex
)

// GetChromeTab: Pars için V6 Turbo motorunda yeni ve izole bir sekme (Tab) açar.
func GetChromeTab(parentCtx context.Context) (context.Context, context.CancelFunc) {
	browserMu.Lock()
	
	// Eğer ana motor henüz çalışmıyorsa veya çöktüyse sıfırdan başlat
	if allocCtx == nil || allocCtx.Err() != nil {
		logger.Action("🌐 V6 Motoru: Ana Tarayıcı Başlatılıyor...")
		opts := append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", true), // Görünmez mod
			chromedp.Flag("disable-gpu", true),
			chromedp.Flag("no-sandbox", true),
			chromedp.Flag("disable-dev-shm-usage", true), // RAM şişmesini önler
			chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
			chromedp.WindowSize(1920, 1080), // Siteler mobil moda geçmesin diye
		)

		// 🚀 CHROME / EDGE AVCISI (Windows PATH Sorununu Çözer)
		if browserPath := findBrowserPath(); browserPath != "" {
			logger.Debug("🎯 Tarayıcı Bulundu: %s", browserPath)
			opts = append(opts, chromedp.ExecPath(browserPath))
		}

		allocCtx, allocCancel = chromedp.NewExecAllocator(context.Background(), opts...)
	}
	browserMu.Unlock()

	// 🚀 SİHİR BURADA: Ana motoru yeniden başlatmak yerine, sadece yeni bir sekme (context) döndürüyoruz!
	// Bu işlem milisaniyeler sürer ve inanılmaz bir hız kazandırır.
	return chromedp.NewContext(allocCtx)
}

// findBrowserPath: Sisteme göre yaygın tarayıcı yollarını otomatik bulur.
func findBrowserPath() string {
	if runtime.GOOS != "windows" {
		return "" // Linux/Mac'te varsayılanlar genellikle tıkır tıkır çalışır
	}

	// Windows için olası Chrome ve Edge kurulum rotaları
	paths := []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path // İlk bulduğunu kullanır
		}
	}
	
	return ""
}

// CloseBrowser: Ajan tamamen kapatılırken arka plandaki ana motoru temizler (Zombie Process Avcısı)
func CloseBrowser() {
	browserMu.Lock()
	defer browserMu.Unlock()
	
	if allocCancel != nil {
		allocCancel()
		allocCtx = nil
		allocCancel = nil
		logger.Action("🧹 V6 Motoru: Tarayıcı motoru kapatıldı, Zombie süreçler temizlendi.")
	}
}
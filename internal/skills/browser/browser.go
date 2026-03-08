package browser

import (
	"context"
	"fmt"
)

// BrowserTool: Pars'ın internetteki TEK silahı. (V6 Turbo Motorlu)
type BrowserTool struct{}

func (t *BrowserTool) Name() string { return "browser" }

func (t *BrowserTool) Description() string {
	return "İnternete açılan TEK kapın. Arama yapmak için 'search', sayfa okumak için 'read', form doldurup tıklamak/SS almak/VERİ ÇEKMEK gibi makrolar için 'interact' modunu kullan."
}

func (t *BrowserTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"mode": map[string]interface{}{
				"type":        "string",
				"description": "Mod seçimi: 'search' (Arama Motoru), 'read' (Sadece metin oku), 'interact' (Tıklama, yazma, SS alma, VERİ ÇEKME gibi çoklu eylemler)",
				"enum":        []string{"search", "read", "interact"},
			},
			"query": map[string]interface{}{"type": "string", "description": "Sadece 'search' modunda aranacak kelime."},
			"url":   map[string]interface{}{"type": "string", "description": "Sadece 'read' veya 'interact' modunda gidilecek adres."},
			"actions": map[string]interface{}{
				"type":        "array",
				"description": "Sadece 'interact' modunda sırayla yapılacak eylemler. Veri çekmek için js_eval YERİNE get_text veya multi_text KULLAN.",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"action": map[string]interface{}{
							"type": "string",
							"enum": []string{
								"click", "type", "enter", "hover", "scroll", "wait",
								"wait_vanish", "select", "upload", "js_eval",
								"get_text", "multi_text", "get_attr", "screenshot",
							},
							"description": "Yapılacak eylem. Tek metin için 'get_text', liste/makale başlıkları için 'multi_text' kullan.",
						},
						"selector": map[string]interface{}{"type": "string", "description": "Hedef CSS seçici (Örn: '#searchBox', '.post-title')"},
						"value":    map[string]interface{}{"type": "string", "description": "İşlem değeri (metin, saniye veya dosya adı)"},
					},
					"required": []string{"action"},
				},
			},
		},
		"required": []string{"mode"},
	}
}

func (t *BrowserTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	mode, _ := args["mode"].(string)

	// Gelen moda göre ilgili alt fonksiyona yönlendir (Router)
	switch mode {
	case "search":
		return t.doSearch(ctx, args)
	case "read":
		return t.doRead(ctx, args)
	case "interact":
		return t.doInteract(ctx, args)
	default:
		return "", fmt.Errorf("HATA: Geçersiz mod. Lütfen 'search', 'read' veya 'interact' kullanın")
	}
}
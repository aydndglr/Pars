package rag //coding

import (
	"context"
	"fmt"
	"strings"

	"github.com/aydndglr/pars-agent-v3/internal/memory"
)

// AskOracleTool: Pars'ın resmi dil ve kütüphane dokümanlarını okumasını sağlayan RAG aracı.
type AskOracleTool struct {
	DocStore *memory.SQLiteStore // Sadece pars_docs.db'ye bağlanacak
}

func (t *AskOracleTool) Name() string { return "ask_oracle" }

func (t *AskOracleTool) Description() string {
	return "SİSTEM KURALI: Yeni bir kod yazmadan veya bilmediğin bir kütüphaneyi kullanmadan ÖNCE resmi dokümantasyonları okumak için bu aracı kullan. Kendi hafızana güvenme, daima en güncel syntax'ı buradan teyit et."
}

func (t *AskOracleTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"language": map[string]interface{}{
				"type":        "string",
				"description": "Dokümantasyonu aranacak dil veya kütüphane etiketi. (Örn: 'python_3.13', 'go_1.22', 'react')",
			},
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Aramak istediğin özellik, fonksiyon veya konsept (Örn: 'asyncio TaskGroup examples')",
			},
		},
		"required": []string{"language", "query"},
	}
}

func (t *AskOracleTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	language, ok := args["language"].(string)
	if !ok || language == "" {
		return "", fmt.Errorf("HATA: 'language' (Dil/Kütüphane adı) belirtilmedi")
	}

	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("HATA: 'query' (Arama sorgusu) belirtilmedi")
	}

	// Doküman veritabanında arama yap (Maksimum 5 en iyi sonuç)
	chunks, err := t.DocStore.SearchCode(ctx, language, query, 5)
	if err != nil {
		return "", fmt.Errorf("Kahinden yanıt alınamadı: %v", err)
	}

	if len(chunks) == 0 {
		return fmt.Sprintf("⚠️ '%s' kütüphanesinde '%s' sorgusu için hiçbir doküman bulunamadı. İnternet araması (browser) yapmayı deneyebilirsin.", language, query), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📚 [%s] DİL KÜTÜPHANESİ SONUÇLARI:\n", strings.ToUpper(language)))
	sb.WriteString(strings.Repeat("=", 50) + "\n")

	for i, chunk := range chunks {
		sb.WriteString(fmt.Sprintf("📄 KAYNAK %d: %s\n", i+1, chunk.FilePath))
		sb.WriteString(strings.Repeat("-", 30) + "\n")
		sb.WriteString(chunk.Content + "\n")
		sb.WriteString(strings.Repeat("=", 50) + "\n")
	}

	sb.WriteString("\n🧠 SİSTEM YÖNERGESİ: Yukarıdaki resmi doküman örneklerini incele ve yazacağın kodu kesinlikle bu güncel standartlara göre kurgula.")

	return sb.String(), nil
}
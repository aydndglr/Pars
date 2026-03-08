package rag //filesystem

import (
	"context"
	"fmt"
	"strings"

	"github.com/aydndglr/pars-agent-v3/internal/memory"
)

type AskCodebaseTool struct {
	Store *memory.SQLiteStore // RAG (FTS5) veritabanı bağlantısı
}

func (t *AskCodebaseTool) Name() string { return "ask_codebase" }

func (t *AskCodebaseTool) Description() string {
	return "PROJE HAFIZA SORGUSU: 'fs_index' ile beynine kazıdığın KENDİ PROJELERİNDE anlamsal arama yapar. Sana sadece aradığın konunun geçtiği en alakalı parçaları (proje adı, dosya adı ve satır numaralarıyla) getirir. Bütün projeyi baştan okumak yerine bu aracı kullanarak spesifik bilgilere ışık hızında ulaşabilirsin."
}

func (t *AskCodebaseTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string", "description": "Aranacak teknik kavram, kelime veya kod parçası (Örn: 'database connection', 'login handler')."},
			// 🚀 YENİ FİLTRE PARAMETRESİ BURADA
			"project_name": map[string]interface{}{"type": "string", "description": "Sadece belirli bir projede arama yapmak için proje adı (Örn: 'omni_erp', 'python_12_docs'). Boş bırakılırsa tüm indekslenmiş projelerde arama yapar."},
			"limit": map[string]interface{}{"type": "integer", "description": "Getirilecek maksimum kod bloğu sayısı (Varsayılan: 3)."},
		},
		"required": []string{"query"},
	}
}

func (t *AskCodebaseTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	query, _ := args["query"].(string)
	projectName, _ := args["project_name"].(string) // Opsiyonel filtre
	
	limit := 3
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	if query == "" {
		return "⚠️ HATA: Aranacak bir 'query' belirtmelisin.", nil
	}

	// 🚀 Hafızadaki RAG tablosuna (FTS5) filtreli/filtresiz sorgu atıyoruz
	chunks, err := t.Store.SearchCode(ctx, projectName, query, limit)
	if err != nil {
		return "", fmt.Errorf("RAG veritabanında arama yapılamadı: %v", err)
	}

	if len(chunks) == 0 {
		if projectName != "" {
			return fmt.Sprintf("📭 '%s' projesinde '%s' ile ilgili indekslenmiş hiçbir kayıt bulunamadı.", projectName, query), nil
		}
		return fmt.Sprintf("📭 Tüm hafızada '%s' ile ilgili hiçbir kayıt bulunamadı. Doğru projeyi 'fs_index' ile indekslediğinden emin ol.", query), nil
	}

	var sb strings.Builder
	
	if projectName != "" {
		sb.WriteString(fmt.Sprintf("🧠 **RAG HAFIZASI SORGUSU (Proje: %s): '%s'**\n", projectName, query))
	} else {
		sb.WriteString(fmt.Sprintf("🧠 **RAG HAFIZASI SORGUSU (Global - Tüm Projeler): '%s'**\n", query))
	}
	
	sb.WriteString(fmt.Sprintf("Bulunan en alakalı %d parça:\n%s\n\n", len(chunks), strings.Repeat("-", 40)))

	for i, chunk := range chunks {
		// Çıktıya hangi projeden bulduğunu da ekliyoruz
		sb.WriteString(fmt.Sprintf("📦 **Proje:** `%s` | 📂 **Dosya:** `%s` (Satır: %d - %d)\n", chunk.ProjectName, chunk.FilePath, chunk.StartLine, chunk.EndLine))
		sb.WriteString("```\n")
		sb.WriteString(strings.TrimSpace(chunk.Content))
		sb.WriteString("\n```\n")
		
		if i < len(chunks)-1 {
			sb.WriteString(strings.Repeat("-", 40) + "\n\n")
		}
	}

	return sb.String(), nil
}
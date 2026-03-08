package rag //filesystem

import (
	"context"
	"fmt"
	"strings"

	"github.com/aydndglr/pars-agent-v3/internal/memory"
)

type ListRAGProjectsTool struct {
	Store *memory.SQLiteStore
}

func (t *ListRAGProjectsTool) Name() string { return "list_rag_projects" }

// 🚀 İŞTE SİHİRLİ DOKUNUŞ BURADA: LLM'i agresif bir şekilde yönlendiriyoruz!
func (t *ListRAGProjectsTool) Description() string {
	return "RAG HAFIZA ANALİZİ: Zihnindeki (FTS5 SQLite) kayıtlı tüm projeleri, indekslenmiş dokümanları ve bunların dosya/parça istatistiklerini listeler. Kullanıcı hafızanda hangi projelerin olduğunu veya indekslenmiş projelerin durumunu sorduğunda bu aracı kullan."
}
func (t *ListRAGProjectsTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *ListRAGProjectsTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	stats, err := t.Store.GetRAGProjectsStats(ctx)
	if err != nil {
		return "", fmt.Errorf("RAG hafızası okunamadı: %v", err)
	}

	if len(stats) == 0 {
		return "📭 **RAG HAFIZAN ŞU AN TAMAMEN BOŞ.**\nHenüz hiçbir proje veya doküman indekslenmemiş. Bir projeyi incelemek için önce 'fs_index' aracını kullanarak zihnine veri yüklemelisin.", nil
	}

	var sb strings.Builder
	sb.WriteString("🧠 **RAG HAFIZASI (ZİHİNSEL ARŞİV) DURUM RAPORU** 🧠\n")
	sb.WriteString(strings.Repeat("=", 50) + "\n\n")

	totalFiles := 0
	totalChunks := 0

	for _, s := range stats {
		sb.WriteString(fmt.Sprintf("📦 **Proje Adı:** `%s`\n", s.ProjectName))
		sb.WriteString(fmt.Sprintf("   📄 İndekslenmiş Dosya Sayısı: %d\n", s.FileCount))
		sb.WriteString(fmt.Sprintf("   🧩 Toplam Bilgi Parçası (Chunk): %d\n", s.ChunkCount))
		sb.WriteString(strings.Repeat("-", 40) + "\n")
		
		totalFiles += s.FileCount
		totalChunks += s.ChunkCount
	}

	sb.WriteString(fmt.Sprintf("\n📊 **GENEL SİSTEM ÖZETİ:**\n"))
	sb.WriteString(fmt.Sprintf("🔹 Toplam Kayıtlı Proje/Doküman Seti: **%d**\n", len(stats)))
	sb.WriteString(fmt.Sprintf("🔹 Toplam Taranan Dosya: **%d**\n", totalFiles))
	sb.WriteString(fmt.Sprintf("🔹 Zihindeki Toplam Veri Bloğu: **%d**\n", totalChunks))
	sb.WriteString("\n💡 **İpucu:** Bu projelerin içinde arama yapmak için `ask_codebase` aracını kullan. Doğru projeyi hedeflemek için arama yaparken `project_name` parametresini kullanmayı unutma!")

	return sb.String(), nil
}
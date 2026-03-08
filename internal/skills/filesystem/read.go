package filesystem

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
)

type ReadTool struct{}

func (t *ReadTool) Name() string { return "fs_read" }
func (t *ReadTool) Description() string {
	return "Dosya içeriğini okur. Büyük dosyalar için beynini korumak adına 'start_line' ve 'max_lines' kullanarak parçalı okuma yapabilirsin."
}

func (t *ReadTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path":       map[string]interface{}{"type": "string", "description": "Okunacak dosya yolu."},
			"start_line": map[string]interface{}{"type": "integer", "description": "Okumaya başlanacak satır numarası (Opsiyonel, varsayılan: 1)."},
			"max_lines":  map[string]interface{}{"type": "integer", "description": "Okunacak maksimum satır sayısı (Opsiyonel, varsayılan: 500)."},
		},
		"required": []string{"path"},
	}
}

func (t *ReadTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	path := ResolvePath(args["path"].(string))

	// Parametreleri güvenli bir şekilde al (JSON'dan float64 olarak gelir)
	startLine := 1
	if val, ok := args["start_line"].(float64); ok {
		startLine = int(val)
	}

	maxLines := 500 // Varsayılan güvenlik sınırı
	if val, ok := args["max_lines"].(float64); ok {
		maxLines = int(val)
	}

	// Pars saçma değerler girerse düzeltelim (Guardrails)
	if startLine < 1 {
		startLine = 1
	}
	if maxLines < 1 || maxLines > 2000 {
		maxLines = 2000 // LLM context penceresi taşmasın diye hard-limit
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("dosya bulunamadı: %v", err)
	}

	// KÜÇÜK DOSYA OPTİMİZASYONU:
	// Eğer özel satır sınırı verilmediyse ve dosya < 1MB ise doğrudan hafızaya alıp dön.
	if info.Size() < 1024*1024 && args["start_line"] == nil && args["max_lines"] == nil {
		content, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(content), nil
	}

	// BÜYÜK DOSYA VE PARÇALI OKUMA MODU:
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	
	// ÇOK UZUN SATIR KORUMASI: Sıkıştırılmış JS veya tek satırlık dev loglar için scanner buffer'ını artırıyoruz.
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024) // Tek bir satır maksimum 2MB olabilir

	currentLine := 0
	var sb strings.Builder
	linesRead := 0

	for scanner.Scan() {
		currentLine++
		
		// Başlangıç satırına gelene kadar atla
		if currentLine < startLine {
			continue
		}

		sb.WriteString(scanner.Text() + "\n")
		linesRead++

		// Maksimum satıra ulaşıldıysa döngüyü kır
		if linesRead >= maxLines {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("dosya okunurken hata oluştu: %v", err)
	}

	// Pars nerede kaldığını bildiren akıllı sistem notu
	systemNote := ""
	if linesRead == maxLines {
		systemNote = fmt.Sprintf("\n\n[SİSTEM NOTU: Maksimum okuma sınırına (%d satır) ulaşıldı. Devamını okumak için aracı start_line: %d ile tekrar çağır.]", maxLines, currentLine+1)
	} else {
		systemNote = "\n\n[SİSTEM NOTU: Dosya sonuna (EOF) ulaşıldı.]"
	}

	return fmt.Sprintf("📄 DOSYA (%s)\nOkunan Satırlar: %d - %d\n\n%s%s", path, startLine, currentLine, sb.String(), systemNote), nil
}
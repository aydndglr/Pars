package network

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type GitHubTool struct{}

func (t *GitHubTool) Name() string { return "github_tool" }

func (t *GitHubTool) Description() string {
	return "GitHub API üzerinden repoları inceler. 'repo_info' ile projenin detaylarını, 'readme' ile ana dökümantasyonu çeker."
}

func (t *GitHubTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string", "enum": []string{"repo_info", "readme"}},
			"repo":   map[string]interface{}{"type": "string", "description": "GitHub depo adı (Örn: 'torvalds/linux' veya 'golang/go')"},
		},
		"required": []string{"action", "repo"},
	}
}

func (t *GitHubTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	action, _ := args["action"].(string)
	repo, _ := args["repo"].(string)

	if !strings.Contains(repo, "/") {
		return "❌ HATA: Repo adı 'kullanici/repo_adi' formatında olmalıdır!", nil
	}

	client := &http.Client{Timeout: 10 * time.Second}
	baseURL := "https://api.github.com/repos/" + repo

	switch action {
	case "repo_info":
		req, _ := http.NewRequestWithContext(ctx, "GET", baseURL, nil)
		
		resp, err := client.Do(req)
		if err != nil { return "", err }
		defer resp.Body.Close()

		if resp.StatusCode == 404 { return "❌ Repo bulunamadı veya gizli.", nil }

		var info struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Stars       int    `json:"stargazers_count"`
			Forks       int    `json:"forks_count"`
			Language    string `json:"language"`
		}
		json.NewDecoder(resp.Body).Decode(&info)

		return fmt.Sprintf("📦 GITHUB REPO: %s\n📝 Açıklama: %s\n⭐ Yıldız: %d | 🍴 Fork: %d\n💻 Ana Dil: %s", repo, info.Description, info.Stars, info.Forks, info.Language), nil

	case "readme":
		req, _ := http.NewRequestWithContext(ctx, "GET", baseURL+"/readme", nil)
		req.Header.Set("Accept", "application/vnd.github.v3.raw")
		
		resp, err := client.Do(req)
		if err != nil { return "", err }
		defer resp.Body.Close()

		if resp.StatusCode == 404 { return "📭 Bu reponun bir README dosyası yok.", nil }

		bodyBytes, _ := io.ReadAll(resp.Body)
		readme := string(bodyBytes)
		
		if len(readme) > 4000 {
			readme = readme[:4000] + "\n\n...[METİN ÇOK UZUN OLDUĞU İÇİN KESİLDİ]..."
		}
		return fmt.Sprintf("📖 README (%s):\n\n%s", repo, readme), nil
	}

	return "Bilinmeyen eylem.", nil
}
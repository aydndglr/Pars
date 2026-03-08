package config

import (
	"os"
	"gopkg.in/yaml.v3"
)

// ========================================================================
// 🐕 KANGAL CONFIG YAPILARI
// ========================================================================
type KangalConfig struct {
	Enabled          bool               `yaml:"enabled"`
	SensitivityLevel string             `yaml:"sensitivity_level"` // low, balanced, high
	WatchdogModel    string             `yaml:"watchdog_model"`    // ← BOŞ İSE KANGAL PASİF
	WatchdogBaseURL  string             `yaml:"watchdog_base_url"`
	Notifications    NotificationConfig `yaml:"notifications"`
	TrackedApps      []string           `yaml:"tracked_apps"`
	QuietHours       QuietHoursConfig   `yaml:"quiet_hours"`
	RateLimit        RateLimitConfig    `yaml:"rate_limit"`
}

type NotificationConfig struct {
	Toast              bool `yaml:"toast"`
	Terminal           bool `yaml:"terminal"`
	WhatsAppCritical   bool `yaml:"whatsapp_critical"`
	WhatsAppSuggestion bool `yaml:"whatsapp_suggestion"`
}

type QuietHoursConfig struct {
	Enabled bool   `yaml:"enabled"`
	Start   string `yaml:"start"` // "23:00"
	End     string `yaml:"end"`   // "07:00"
}

type RateLimitConfig struct {
	MaxSuggestionsPerHour int `yaml:"max_suggestions_per_hour"`
	MaxCriticalPerHour    int `yaml:"max_critical_per_hour"`
	CooldownSeconds       int `yaml:"cooldown_between_suggestions"`
}

// 🆕 YENİ: IsWatchdogEnabled - Watchdog modeli tanımlı mı kontrol et
func (k *KangalConfig) IsWatchdogEnabled() bool {
	return k.Enabled && k.WatchdogModel != ""
}


func (k *KangalConfig) GetWatchdogBaseURL(primaryBaseURL string) string {
    if k.WatchdogBaseURL != "" {
        return k.WatchdogBaseURL  // Explicit ayar varsa onu kullan
    }
    return primaryBaseURL  // Yoksa primary'ninkini kullan
}
// ========================================================================
// 🧠 ANA CONFIG YAPISI
// ========================================================================
type Config struct {
	App struct {
		Name             string `yaml:"name"`
		ActivePrompt     string `yaml:"active_prompt"`
		Version          string `yaml:"version"`
		TimeoutMinutes   int    `yaml:"timeout_minutes" mapstructure:"timeout_minutes"`
		MaxSteps         int    `yaml:"max_steps"`
		MaxContextTokens int    `yaml:"max_context_tokens"`
		Debug            bool   `yaml:"debug"`
		WorkDir          string `yaml:"work_dir"`
	} `yaml:"app"`

	Security struct {
		Level        string `yaml:"level"`         // god_mode, standard, restricted
		AutoPatching bool   `yaml:"auto_patching"` // Kendi kodunu tamir etme
	} `yaml:"security"`

	Brain struct {
		Primary struct {
			Provider    string  `yaml:"provider"`
			BaseURL     string  `yaml:"base_url"`
			ModelName   string  `yaml:"model_name"`
			Temperature float64 `yaml:"temperature"`
			NumCtx      int     `yaml:"num_ctx"`
		} `yaml:"primary"`

		Secondary struct {
			Enabled   bool   `yaml:"enabled"`
			Provider  string `yaml:"provider"`
			BaseURL   string `yaml:"base_url"`
			ModelName string `yaml:"model_name"`
		} `yaml:"secondary"`

		APIKeys struct {
			OpenAI    string `yaml:"openai"`
			Gemini    string `yaml:"gemini"`
			Anthropic string `yaml:"anthropic"`
			Ollama    string `yaml:"ollama"`
		} `yaml:"api_keys"`
	} `yaml:"brain"`

	Communication struct {
		Whatsapp struct {
			Enabled      bool   `yaml:"enabled"`
			AdminPhone   string `yaml:"admin_phone"`
			DatabasePath string `yaml:"database_path"`
		} `yaml:"whatsapp"`
	} `yaml:"communication"`

	// 🆕 YENİ: KANGAL PROAKTİF BEKÇİ SİSTEMİ
	Kangal KangalConfig `yaml:"kangal"`
}

// Load: Config dosyasını okur
func Load(path string) (*Config, error) {
	config := &Config{}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(data, config)
	if err != nil {
		return nil, err
	}
	return config, nil
}
package config

import (
	"os"
	"gopkg.in/yaml.v3"
)

type KangalConfig struct {
	Enabled          bool               `yaml:"enabled"`
	SensitivityLevel string             `yaml:"sensitivity_level"` 
	WatchdogModel    string             `yaml:"watchdog_model"`    
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
	Start   string `yaml:"start"` 
	End     string `yaml:"end"`  
}

type RateLimitConfig struct {
	MaxSuggestionsPerHour int `yaml:"max_suggestions_per_hour"`
	MaxCriticalPerHour    int `yaml:"max_critical_per_hour"`
	CooldownSeconds       int `yaml:"cooldown_between_suggestions"`
}

func (k *KangalConfig) IsWatchdogEnabled() bool {
	return k.Enabled && k.WatchdogModel != ""
}


func (k *KangalConfig) GetWatchdogBaseURL(primaryBaseURL string) string {
    if k.WatchdogBaseURL != "" {
        return k.WatchdogBaseURL  
    }
    return primaryBaseURL  
}

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
		Level        string `yaml:"level"`         
		AutoPatching bool   `yaml:"auto_patching"` 
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
			Qwen      string `yaml:"qwen"`
		} `yaml:"api_keys"`
	} `yaml:"brain"`

	Communication struct {
		Whatsapp struct {
			Enabled      bool   `yaml:"enabled"`
			AdminPhone   string `yaml:"admin_phone"`
			DatabasePath string `yaml:"database_path"`
		} `yaml:"whatsapp"`
	} `yaml:"communication"`


	Kangal KangalConfig `yaml:"kangal"`
}


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
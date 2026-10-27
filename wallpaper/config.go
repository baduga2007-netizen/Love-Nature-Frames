package wallpaper

import "fmt"

// Config — настройка обоев и переходов.
type Config struct {
	CacheDir          string           `json:"cache_dir,omitempty"`
	CacheMaxSize      int64            `json:"cache_max_size,omitempty"`
	TransitionDuration int64           `json:"transition_duration,omitempty"`
	Transitions       []TransitionConfig `json:"transitions,omitempty"`
}

// TransitionConfig — настройка перехода.
type TransitionConfig struct {
	Name        string            `json:"name,omitempty"`
	Type        string            `json:"type,omitempty"`
	Duration    int64             `json:"duration,omitempty"`
	Easing      string            `json:"easing,omitempty"`
	Direction   string            `json:"direction,omitempty"`
	Custom      bool              `json:"custom,omitempty"`
	CustomScript string           `json:"custom_script,omitempty"`
	CustomArgs  []string          `json:"custom_args,omitempty"`
	CustomEnv   map[string]string `json:"custom_env,omitempty"`
}

// Validate — проверка конфигурации на валидность.
func (c *Config) Validate() error {
	if c.CacheDir == "" {
		return fmt.Errorf("cache_dir is required")
	}
	if c.CacheMaxSize <= 0 {
		c.CacheMaxSize = 100 * 1024 * 1024
	}
	if c.TransitionDuration <= 0 {
		c.TransitionDuration = 300
	}
	for _, t := range c.Transitions {
		if t.Name == "" {
			t.Name = "fade"
		}
		if t.Type == "" {
			t.Type = "fade"
		}
		if t.Duration <= 0 {
			t.Duration = c.TransitionDuration
		}
	}
	return nil
}

// DefaultConfig — создание конфигурации по умолчанию.
func DefaultConfig() *Config {
	return &Config{
		CacheDir:          "./cache",
		CacheMaxSize:      100 * 1024 * 1024,
		TransitionDuration: 300,
		Transitions: []TransitionConfig{
			{
				Name:        "fade",
				Type:        "fade",
				Duration:    300,
				Easing:      "ease-in-out",
				Custom:      false,
			},
		},
	}
}

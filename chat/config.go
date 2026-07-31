package chat

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"stele/internal/appdir"
)

type ProviderConfig struct {
	APIKey      string  `json:"api_key"`
	APIBase     string  `json:"api_base"`
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens"`
	Thinking    string  `json:"thinking"`
}

type Config struct {
	ActiveProvider string                    `json:"active_provider"`
	Providers      map[string]ProviderConfig `json:"providers"`
}

// legacyConfigDir is where the LLM provider config lived before the project was
// renamed and its state consolidated into one data directory.
const legacyConfigDir = ".comet-ui"

// configPath returns the config file inside the data directory, moving the
// pre-rename file there once. The file holds API keys, so silently losing track
// of it would drop the user's whole provider setup.
func configPath() (string, error) {
	path := appdir.Path("config.json")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path, nil
	}
	legacy := filepath.Join(home, legacyConfigDir, "config.json")
	if _, err := os.Stat(legacy); err != nil {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		log.Printf("chat: cannot prepare %s (%v); reading the legacy config at %s", filepath.Dir(path), err, legacy)
		return legacy, nil
	}
	if err := os.Rename(legacy, path); err != nil {
		log.Printf("chat: could not migrate %s to %s (%v); continuing with the legacy file", legacy, path, err)
		return legacy, nil
	}
	log.Printf("chat: migrated provider config %s -> %s", legacy, path)
	return path, nil
}

var LoadConfig = func() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultConfig(), nil
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultConfig(), nil
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]ProviderConfig{}
	}
	return &cfg, nil
}

func SaveConfig(cfg *Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	os.MkdirAll(dir, 0700)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func defaultConfig() *Config {
	return &Config{
		ActiveProvider: "minimax",
		Providers: map[string]ProviderConfig{
			"minimax": {
				APIBase:     "https://api.minimaxi.com",
				Model:       "MiniMax-M3",
				Temperature: 1.0,
				MaxTokens:   4096,
				Thinking:    "auto",
			},
		},
	}
}

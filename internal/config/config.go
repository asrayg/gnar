// Package config resolves Gnar's home directory and loads/saves user config.
//
// Precedence for every setting: explicit env var > config.json > built-in default.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// EmbedConfig configures the embeddings provider.
type EmbedConfig struct {
	// Provider is "hash" (default, zero-config), "openai", or "ollama".
	Provider string `json:"provider"`
	// Model is the embedding model name (provider-specific).
	Model string `json:"model,omitempty"`
	// BaseURL overrides the provider endpoint (e.g. http://localhost:11434 for ollama,
	// or an OpenAI-compatible base like http://localhost:1234/v1).
	BaseURL string `json:"base_url,omitempty"`
	// APIKeyEnv names the environment variable holding the API key (default OPENAI_API_KEY).
	APIKeyEnv string `json:"api_key_env,omitempty"`
	// Dim is the embedding dimension. For "hash" it is configurable (default 256);
	// for remote providers it is informational/used to request reduced dimensions.
	Dim int `json:"dim,omitempty"`
}

// Config is the persisted user configuration.
type Config struct {
	// DefaultSource labels memories written via the CLI when no source is given.
	DefaultSource string `json:"default_source,omitempty"`
	// Embed configures embeddings.
	Embed EmbedConfig `json:"embed"`
	// CandidateCap bounds how many recent memories a single recall scans per project.
	CandidateCap int `json:"candidate_cap,omitempty"`

	// home is the resolved gnar home directory (not serialized).
	home string `json:"-"`
}

// Defaults returns a Config with sensible zero-config defaults.
func Defaults() Config {
	return Config{
		DefaultSource: "cli",
		Embed: EmbedConfig{
			Provider:  "hash",
			APIKeyEnv: "OPENAI_API_KEY",
			Dim:       256,
		},
		CandidateCap: 5000,
	}
}

// Home returns the gnar home directory, honoring GNAR_HOME, else ~/.gnar.
func Home() string {
	if h := os.Getenv("GNAR_HOME"); h != "" {
		return h
	}
	dir, err := os.UserHomeDir()
	if err != nil || dir == "" {
		return ".gnar"
	}
	return filepath.Join(dir, ".gnar")
}

// DBPath returns the database path, honoring GNAR_DB, else <home>/gnar.db.
func DBPath() string {
	if p := os.Getenv("GNAR_DB"); p != "" {
		return p
	}
	return filepath.Join(Home(), "gnar.db")
}

// ConfigPath returns the config file path (<home>/config.json).
func ConfigPath() string {
	return filepath.Join(Home(), "config.json")
}

// HomeDir returns the home this config was loaded from.
func (c *Config) HomeDir() string { return c.home }

// Load reads config.json (creating the home dir), applies defaults for any
// missing fields, then overlays environment variables.
func Load() (*Config, error) {
	return load(true)
}

// LoadRaw is like Load but does NOT overlay environment variables. Use it before
// mutating and saving config, so transient env overrides are not baked into the
// persisted file (which would violate the documented env > file precedence).
func LoadRaw() (*Config, error) {
	return load(false)
}

func load(withEnv bool) (*Config, error) {
	home := Home()
	if err := os.MkdirAll(home, 0o755); err != nil {
		return nil, err
	}
	cfg := Defaults()
	cfg.home = home

	data, err := os.ReadFile(ConfigPath())
	if err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	// Re-apply defaults for any fields the file left empty.
	d := Defaults()
	if cfg.DefaultSource == "" {
		cfg.DefaultSource = d.DefaultSource
	}
	if cfg.Embed.Provider == "" {
		cfg.Embed.Provider = d.Embed.Provider
	}
	if cfg.Embed.APIKeyEnv == "" {
		cfg.Embed.APIKeyEnv = d.Embed.APIKeyEnv
	}
	if cfg.Embed.Dim == 0 {
		cfg.Embed.Dim = d.Embed.Dim
	}
	if cfg.CandidateCap == 0 {
		cfg.CandidateCap = d.CandidateCap
	}
	cfg.home = home
	if withEnv {
		cfg.applyEnv()
	}
	return &cfg, nil
}

// applyEnv overlays GNAR_* environment overrides.
func (c *Config) applyEnv() {
	if v := os.Getenv("GNAR_SOURCE"); v != "" {
		c.DefaultSource = v
	}
	if v := os.Getenv("GNAR_EMBED_PROVIDER"); v != "" {
		c.Embed.Provider = strings.ToLower(v)
	}
	if v := os.Getenv("GNAR_EMBED_MODEL"); v != "" {
		c.Embed.Model = v
	}
	if v := os.Getenv("GNAR_EMBED_BASE_URL"); v != "" {
		c.Embed.BaseURL = v
	}
	if v := os.Getenv("GNAR_EMBED_DIM"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			c.Embed.Dim = n
		}
	}
}

// APIKey resolves the API key from the configured env var (GNAR_EMBED_API_KEY
// takes precedence as a direct override).
func (c *Config) APIKey() string {
	if v := os.Getenv("GNAR_EMBED_API_KEY"); v != "" {
		return v
	}
	if c.Embed.APIKeyEnv != "" {
		return os.Getenv(c.Embed.APIKeyEnv)
	}
	return ""
}

// Save writes the config back to disk as pretty JSON.
func (c *Config) Save() error {
	if err := os.MkdirAll(c.home, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(ConfigPath(), data, 0o644)
}

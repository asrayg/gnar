package config

import (
	"os"
	"testing"
)

// TestConfigSetDoesNotPersistEnvOverrides guards the precedence bug: an active
// GNAR_* override must not be baked into config.json when saving an unrelated key.
func TestConfigSetDoesNotPersistEnvOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GNAR_HOME", home)

	// Start from a clean saved config (provider=hash).
	cfg, err := LoadRaw()
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Simulate an env override active in the shell.
	t.Setenv("GNAR_EMBED_PROVIDER", "ollama")

	// LoadRaw must ignore the env override.
	raw, err := LoadRaw()
	if err != nil {
		t.Fatal(err)
	}
	if raw.Embed.Provider == "ollama" {
		t.Fatal("LoadRaw must not apply env overrides")
	}

	// Load (with env) should reflect the override at runtime.
	live, _ := Load()
	if live.Embed.Provider != "ollama" {
		t.Fatalf("Load should apply env override, got %q", live.Embed.Provider)
	}

	// Mutate an unrelated key via the raw config and save.
	raw.DefaultSource = "me"
	if err := raw.Save(); err != nil {
		t.Fatal(err)
	}

	// The persisted file must NOT contain the env-injected provider.
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" {
		t.Fatal("empty config file")
	}
	// Re-read raw (still with env set) — provider must remain the saved value, not ollama.
	reread, _ := LoadRaw()
	if reread.Embed.Provider == "ollama" {
		t.Fatalf("env override leaked into persisted config: %s", string(data))
	}
}

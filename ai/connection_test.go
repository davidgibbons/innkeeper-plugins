package main

import (
	"slices"
	"strings"
	"testing"
)

func TestConnect(t *testing.T) {
	cases := []struct {
		config  map[string]any
		key     string
		wantErr string
	}{
		{map[string]any{"provider": "openai", "model": "gpt-5"}, "", "API key"},
		{map[string]any{"provider": "openai", "model": "gpt-5"}, "sk-x", ""},
		{map[string]any{"provider": "anthropic", "model": "claude"}, "k", ""},
		{map[string]any{"provider": "openai-compatible", "model": "m", "base_url": "http://localhost:8000/v1"}, "", ""},
		{map[string]any{"provider": "openai-compatible", "model": "m"}, "", "base_url"},
		{map[string]any{"provider": "ollama", "model": "llama3"}, "", ""},
		{map[string]any{"provider": "llamacpp", "model": "m"}, "", ""},
		{map[string]any{"provider": "llamafile", "model": "m"}, "", ""},
		{map[string]any{"provider": "gemini", "model": "m"}, "k", ""},
		{map[string]any{"provider": "mistral", "model": "m"}, "k", ""},
		{map[string]any{"provider": "groq", "model": "m"}, "k", ""},
		{map[string]any{"provider": "deepseek", "model": "m"}, "k", ""},
		{map[string]any{"provider": "skynet", "model": "m"}, "", "provider"},
		{map[string]any{"provider": "ollama"}, "", "model"},
		{map[string]any{"provider": "ollama", "model": "m", "kinds": []any{"draw"}}, "", "kind"},
		{map[string]any{"provider": "openai", "model": "e", "kinds": []any{"embed"}}, "k", ""},
		{map[string]any{"provider": "ollama", "model": "e", "kinds": []any{"embed"}}, "", ""},
		{map[string]any{"provider": "anthropic", "model": "c", "kinds": []any{"embed"}}, "k", "embeddings"},
		{map[string]any{"provider": "groq", "model": "m", "kinds": []any{"chat", "embed"}}, "k", "embeddings"},
	}
	for _, c := range cases {
		_, err := connect(c.config, c.key)
		if c.wantErr == "" && err != nil {
			t.Errorf("%v: %v", c.config, err)
		}
		if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%v: err = %v, want one mentioning %q", c.config, err, c.wantErr)
		}
	}
}

func TestParseSettingsDefaultsKindsToChat(t *testing.T) {
	for name, cfg := range map[string]map[string]any{
		"absent": {"provider": "ollama", "model": "m"},
		"empty":  {"provider": "ollama", "model": "m", "kinds": []any{}},
	} {
		s, err := parseSettings(cfg)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !slices.Equal(s.Kinds, []string{"chat"}) {
			t.Errorf("%s: kinds = %v, want [chat]", name, s.Kinds)
		}
	}
}

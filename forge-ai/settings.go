package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	anyllm "github.com/mozilla-ai/any-llm-go"
	"github.com/mozilla-ai/any-llm-go/config"
	"github.com/mozilla-ai/any-llm-go/providers/anthropic"
	"github.com/mozilla-ai/any-llm-go/providers/deepseek"
	"github.com/mozilla-ai/any-llm-go/providers/gemini"
	"github.com/mozilla-ai/any-llm-go/providers/groq"
	"github.com/mozilla-ai/any-llm-go/providers/llamacpp"
	"github.com/mozilla-ai/any-llm-go/providers/llamafile"
	"github.com/mozilla-ai/any-llm-go/providers/mistral"
	"github.com/mozilla-ai/any-llm-go/providers/ollama"
	"github.com/mozilla-ai/any-llm-go/providers/openai"
)

// settings are the instance's config.
type settings struct {
	Provider    string            `json:"provider"`
	BaseURL     string            `json:"base_url"`
	Model       string            `json:"model"`
	Temperature *float64          `json:"temperature"`
	MaxTokens   *int              `json:"max_tokens"`
	HouseStyle  string            `json:"house_style"`
	Additions   map[string]string `json:"additions"`
}

func parseSettings(cfg map[string]any) (settings, error) {
	var s settings
	b, err := json.Marshal(cfg)
	if err == nil {
		err = json.Unmarshal(b, &s)
	}
	if err != nil {
		return s, fmt.Errorf("settings: %w", err)
	}
	if _, ok := providers[s.Provider]; !ok {
		return s, fmt.Errorf("settings: provider %q isn't one of %s", s.Provider, strings.Join(slices.Sorted(maps.Keys(providers)), ", "))
	}
	if s.Model == "" {
		return s, errors.New("settings: model is required")
	}
	return s, nil
}

type newProvider func(...config.Option) (anyllm.Provider, error)

// wrap turns a provider package's New into a newProvider.
func wrap[P anyllm.Provider](f func(...config.Option) (P, error)) newProvider {
	return func(opts ...config.Option) (anyllm.Provider, error) {
		p, err := f(opts...)
		if err != nil {
			return nil, err
		}
		return p, nil
	}
}

var providers = map[string]newProvider{
	// For vLLM, llama.cpp servers, LM Studio, and OpenRouter, with or without a key.
	"openai-compatible": wrap(func(opts ...config.Option) (*openai.CompatibleProvider, error) {
		return openai.NewCompatible(openai.CompatibleConfig{Name: "openai-compatible"}, opts...)
	}),
	"openai":    wrap(openai.New),
	"anthropic": wrap(anthropic.New),
	"gemini":    wrap(gemini.New),
	"mistral":   wrap(mistral.New),
	"groq":      wrap(groq.New),
	"deepseek":  wrap(deepseek.New),
	"ollama":    wrap(ollama.New),
	"llamacpp":  wrap(llamacpp.New),
	"llamafile": wrap(llamafile.New),
}

// provider builds the configured any-llm-go provider. Providers that need
// a key fail here without one.
func (s settings) provider(apiKey string) (anyllm.Provider, error) {
	if s.Provider == "openai-compatible" && s.BaseURL == "" {
		return nil, errors.New("settings: openai-compatible needs a base_url")
	}
	// A long generation on a local model can stream for many minutes; the run's
	// context is what bounds it.
	opts := []config.Option{config.WithTimeout(time.Hour)}
	if apiKey != "" {
		opts = append(opts, config.WithAPIKey(apiKey))
	}
	if s.BaseURL != "" {
		opts = append(opts, config.WithBaseURL(s.BaseURL))
	}
	p, err := providers[s.Provider](opts...)
	if err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	return p, nil
}

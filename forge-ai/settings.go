package main

import (
	"encoding/json"
	"errors"
	"fmt"
)

// settings are the instance's config.
type settings struct {
	// Connection is the ai instance every model call goes to.
	Connection  int64             `json:"connection"`
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
	if s.Connection == 0 {
		return s, errors.New("settings: pick an AI connection")
	}
	return s, nil
}

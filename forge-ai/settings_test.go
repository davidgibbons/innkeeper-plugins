package main

import "testing"

func TestParseSettings(t *testing.T) {
	s, err := parseSettings(map[string]any{"connection": 9.0, "temperature": 0.5})
	if err != nil || s.Connection != 9 || *s.Temperature != 0.5 {
		t.Fatalf("settings %+v: %v", s, err)
	}
	for _, cfg := range []map[string]any{{}, {"connection": "nine"}, {"connection": 9.0, "temperature": "hot"}} {
		if _, err := parseSettings(cfg); err == nil {
			t.Errorf("%v: no error", cfg)
		}
	}
}

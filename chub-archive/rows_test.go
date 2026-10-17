package main

import (
	"errors"
	"testing"
	"time"
)

func TestPgTime(t *testing.T) {
	for in, want := range map[string]time.Time{
		"2024-05-06 20:00:28.368094+00": time.Date(2024, 5, 6, 20, 0, 28, 368094000, time.UTC),
		"2024-06-01 00:00:00+00":        time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		"2024-06-01 05:30:00+05:30":     time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
	} {
		got, err := pgTime(in)
		if err != nil || !got.Equal(want) {
			t.Errorf("pgTime(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := pgTime("May 1"); err == nil {
		t.Error("pgTime accepted May 1")
	}
}

func TestReadVersionRejectsBadRows(t *testing.T) {
	def := `{"data": {"name": "Aria"}, "spec": "chara_card_v2", "spec_version": "2.0"}`
	for name, bad := range map[string]map[string]string{
		"definition": {"id": "1", "increment": "1", "added": "2024-01-01 00:00:00+00", "definition": `{"data": "no"}`},
		"increment":  {"id": "1", "increment": "x", "added": "2024-01-01 00:00:00+00", "definition": def},
		"added":      {"id": "1", "increment": "1", "added": "soon", "definition": def},
		"no date":    {"id": "1", "increment": "1", "definition": def},
		"no id":      {"increment": "1", "added": "2024-01-01 00:00:00+00", "definition": def},
	} {
		if _, err := readVersion("chub_character_def", bad, nil); !errors.Is(err, errBadRow) {
			t.Errorf("%s: err = %v, want errBadRow", name, err)
		}
	}
}

// booru rows date a version by created when added is missing.
func TestReadVersionFallsBackToCreated(t *testing.T) {
	row := map[string]string{"id": "1", "created": "2024-01-02 00:00:00+00",
		"definition": `{"data": {"name": "Aria"}, "spec": "chara_card_v2", "spec_version": "2.0"}`}
	v, err := readVersion("booru_character_def", row, nil)
	if err != nil || !v.created.Equal(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("created = %v, err %v; want the created column", v.created, err)
	}
}

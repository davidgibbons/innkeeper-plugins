package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/davidgibbons/innkeeper-plugins/catalogkit"
	"github.com/davidgibbons/innkeeper-plugins/internal/pgtest"
	"github.com/davidgibbons/innkeeper/protocol"
)

func openStore(t *testing.T) *catalogkit.Store {
	t.Helper()
	s, err := catalogkit.Open(context.Background(), pgtest.URL(t), "test", catalogConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

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

func TestEntry(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	def := map[string]string{"author": "ann", "name": "Aria", "id": "100", "hidden": "f", "increment": "9",
		"added": "2024-10-02 09:50:16+00", "image_hash": "0",
		"definition": `{"data": {"name": "Aria", "description": "An elf.", "creator": "ann", "tags": ["ranger"]}, "spec": "chara_card_v2", "spec_version": "2.0"}`}

	e, err := entry(ctx, s, "chub_character_def", def)
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "chub_character_def:100" || e.Kind != protocol.KindCard || e.Version != 9 || e.Author != "ann" ||
		e.Tags != nil || e.Attrs["site"][0] != "chub" || e.Attrs["hidden"][0] != "false" {
		t.Fatalf("entry = %+v", e)
	}

	// With its parent row, the entry takes chub's tags, rating, date, and hidden flag.
	parent := map[string]string{"id": "100", "data": `{"rating": 4.5, "topics": ["Fantasy"]}`,
		"added": "2024-05-06 17:36:35+00", "hidden": "t"}
	if err := putParent(ctx, s, "chub_character", parent); err != nil {
		t.Fatal(err)
	}
	e, err = entry(ctx, s, "chub_character_def", def)
	if err != nil {
		t.Fatal(err)
	}
	if e.Tags[0] != "Fantasy" || e.Numbers["rating"] != 4.5 || e.Added.Year() != 2024 || e.Added.Month() != 5 ||
		e.Attrs["hidden"][0] != "true" {
		t.Fatalf("entry with parent = %+v", e)
	}

	// booru rows have no increment, so they are version 1.
	booru := map[string]string{"author": "cat", "name": "Cora", "id": "+p1", "hidden": "f", "added": "2024-05-08 05:10:30+00",
		"definition": def["definition"]}
	if e, err = entry(ctx, s, "booru_character_def", booru); err != nil || e.Version != 1 || e.ID != "booru_character_def:+p1" {
		t.Fatalf("booru entry = %+v, %v", e, err)
	}

	for name, bad := range map[string]map[string]string{
		"definition": {"id": "1", "increment": "1", "added": "2024-01-01 00:00:00+00", "definition": `{"data": "no"}`},
		"increment":  {"id": "1", "increment": "x", "added": "2024-01-01 00:00:00+00", "definition": def["definition"]},
		"added":      {"id": "1", "increment": "1", "added": "soon", "definition": def["definition"]},
		"no id":      {"increment": "1", "added": "2024-01-01 00:00:00+00", "definition": def["definition"]},
	} {
		if _, err := entry(ctx, s, "chub_character_def", bad); !errors.Is(err, errBadRow) {
			t.Errorf("%s: err = %v, want errBadRow", name, err)
		}
	}
}

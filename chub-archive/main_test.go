package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/inkandale/innkeeper-plugins/internal/pgtest"
	"github.com/jackc/pgx/v5"
)

// tables02 is the schema chub-archive 0.2 left, cut to what matters for the drop.
const tables02 = `
CREATE TABLE migration (name text PRIMARY KEY, applied timestamptz NOT NULL DEFAULT now());
CREATE FUNCTION tags_text(text[]) RETURNS text LANGUAGE sql IMMUTABLE AS $$ SELECT array_to_string($1, ' ') $$;
CREATE TABLE item (instance text, id text, PRIMARY KEY (instance, id));
CREATE TABLE item_version (instance text, item_id text, version text,
	FOREIGN KEY (instance, item_id) REFERENCES item (instance, id));
CREATE TABLE parent (instance text, item_id text);
CREATE TABLE source_row (instance text, item_id text, version text);
CREATE TABLE unrelated (id int);`

func TestDropOldTables(t *testing.T) {
	ctx := context.Background()
	url := pgtest.URL(t)
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, tables02); err != nil {
		t.Fatal(err)
	}
	// Twice: the second run finds nothing to drop.
	for range 2 {
		if err := dropOldTables(ctx, url); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := conn.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema()
		UNION ALL SELECT routine_name FROM information_schema.routines WHERE routine_schema = current_schema()`)
	if err != nil {
		t.Fatal(err)
	}
	left, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(left, []string{"unrelated"}) {
		t.Fatalf("left %s, want only unrelated", strings.Join(left, ", "))
	}
}

func TestConfigImages(t *testing.T) {
	if got, err := configImages(map[string]any{"images": []any{"/a", "", "/b"}}); err != nil || !slices.Equal(got, []string{"/a", "/b"}) {
		t.Fatalf("images = %v, %v", got, err)
	}
	if got, err := configImages(map[string]any{}); err != nil || got != nil {
		t.Fatalf("no images = %v, %v", got, err)
	}
	for _, bad := range []any{"/a", []any{"/a", 3}} {
		if _, err := configImages(map[string]any{"images": bad}); err == nil || !strings.Contains(err.Error(), "images") {
			t.Errorf("images %v: err = %v, want one naming images", bad, err)
		}
	}
}

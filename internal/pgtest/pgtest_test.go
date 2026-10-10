package pgtest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestURLGivesItsOwnSchema(t *testing.T) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, URL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var schema string
	if err := conn.QueryRow(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if len(schema) < 6 || schema[:5] != "test_" {
		t.Fatalf("current_schema() = %q, want test_…", schema)
	}
}

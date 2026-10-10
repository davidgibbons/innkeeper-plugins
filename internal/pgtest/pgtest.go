// Package pgtest gives each test a PostgreSQL schema of its own.
package pgtest

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

// URL returns INNKEEPER_TEST_DATABASE_URL with search_path set to a new
// schema, dropped when the test ends. It skips the test without the variable.
func URL(t testing.TB) string {
	t.Helper()
	base := os.Getenv("INNKEEPER_TEST_DATABASE_URL")
	if base == "" {
		t.Skip("INNKEEPER_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("test_%x", rand.Uint64())
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := conn.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("dropping %s: %v", schema, err)
		}
		conn.Close(ctx)
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	// pgx sends unknown URL parameters as session settings.
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

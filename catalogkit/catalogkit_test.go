package catalogkit

import (
	"context"
	"strings"
	"testing"

	"github.com/davidgibbons/innkeeper-plugins/internal/pgtest"
	"github.com/davidgibbons/innkeeper/protocol"
)

var testConfig = Config{
	Kinds: []string{protocol.KindCard, protocol.KindLorebook},
	Filters: []Filter{
		{CatalogFilter: protocol.CatalogFilter{Name: "tag", Label: "Tag", Type: protocol.FilterText}, Field: "tag"},
		{CatalogFilter: protocol.CatalogFilter{Name: "dir", Label: "Folder", Type: protocol.FilterText}, Field: "attrs.dir"},
		{CatalogFilter: protocol.CatalogFilter{Name: "changed", Label: "Changed", Type: protocol.FilterDateRange}, Field: "updated"},
	},
	Ingest: true,
}

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), pgtest.URL(t), testConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestDescribeIsValid(t *testing.T) {
	if err := Describe(testConfig).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRefusesUnsupportedFilter(t *testing.T) {
	cfg := testConfig
	cfg.Filters = []Filter{{CatalogFilter: protocol.CatalogFilter{Name: "rating", Label: "Rating",
		Type: protocol.FilterNumberRange}, Field: "attrs.rating"}}
	_, err := Open(context.Background(), "postgres://unused", cfg)
	if err == nil || !strings.Contains(err.Error(), `filter "rating"`) {
		t.Fatalf("err = %v, want the rating filter refused", err)
	}
}

// Opening twice applies each migration once, and runs the plugin's own.
func TestOpenMigratesOnce(t *testing.T) {
	ctx := context.Background()
	url := pgtest.URL(t)
	cfg := testConfig
	cfg.Migrations = []Migration{{Name: "test/1", SQL: "CREATE TABLE extra (x int)"}}
	for range 2 {
		s, err := Open(ctx, url, cfg)
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
	}
	s, err := Open(ctx, url, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var n int
	if err := s.Pool().QueryRow(ctx, "SELECT count(*) FROM migration").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if want := len(migrations) + 1; n != want {
		t.Fatalf("%d migrations recorded, want %d", n, want)
	}
}

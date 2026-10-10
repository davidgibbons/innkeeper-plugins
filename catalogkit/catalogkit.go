// Package catalogkit is the index a catalog plugin keeps in its own schema:
// items and their versions, full-text search, declared filters, and near
// matches by hash.
package catalogkit

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/davidgibbons/innkeeper/protocol"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Config describes one catalog.
type Config struct {
	Kinds   []string
	Filters []Filter
	Ingest  bool
	// Migrations are the plugin's own, applied after catalogkit's, each once
	// by name.
	Migrations []Migration
}

// Migration is one schema change.
type Migration struct {
	Name string
	SQL  string
}

// Filter is a declared filter and what it reads. Field is "tag" or
// "attrs.<key>" for a text filter, and "added" or "updated" for a date
// range.
type Filter struct {
	protocol.CatalogFilter
	Field string
}

// ponytail: only the filters folder declares; chub-archive (37d) adds number ranges.
func (f Filter) check() error {
	ok := false
	switch f.Type {
	case protocol.FilterText:
		ok = f.Field == "tag" || strings.HasPrefix(f.Field, "attrs.") && len(f.Field) > len("attrs.")
	case protocol.FilterDateRange:
		ok = f.Field == "added" || f.Field == "updated"
	}
	if !ok {
		return fmt.Errorf("filter %q: catalogkit can't serve a %s filter on %q", f.Name, f.Type, f.Field)
	}
	return nil
}

// Sorts are the orders every catalogkit catalog offers. Relevance falls back
// to newest when the query is empty.
var sorts = []protocol.CatalogOption{
	{Value: "relevance", Label: "Best match"},
	{Value: "name", Label: "Name"},
	{Value: "newest", Label: "Recently changed"},
}

// Describe returns the catalog.describe reply for cfg.
func Describe(cfg Config) protocol.CatalogInfo {
	info := protocol.CatalogInfo{Kinds: cfg.Kinds, Filters: []protocol.CatalogFilter{}, Sorts: sorts,
		Ingest: cfg.Ingest, Similar: true}
	for _, f := range cfg.Filters {
		info.Filters = append(info.Filters, f.CatalogFilter)
	}
	return info
}

// Store is an open catalog index.
type Store struct {
	pool     *pgxpool.Pool
	cfg      Config
	instance string
}

// Open checks cfg, connects to the plugin's database, and migrates it. The
// plugin's instances share its schema, so every row belongs to an instance.
func Open(ctx context.Context, dsn, instance string, cfg Config) (*Store, error) {
	errs := []error{Describe(cfg).Validate()}
	if instance == "" {
		errs = append(errs, errors.New("catalogkit: instance is empty"))
	}
	for _, f := range cfg.Filters {
		errs = append(errs, f.check())
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := migrate(ctx, pool, append(slices.Clone(migrations), cfg.Migrations...)); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool, cfg: cfg, instance: instance}, nil
}

// Close closes the database connections.
func (s *Store) Close() { s.pool.Close() }

// Instance is the plugin instance this store's rows belong to, for the
// plugin's own tables.
func (s *Store) Instance() string { return s.instance }

// Pool is the database, for the plugin's own tables.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// migrate applies each migration not yet recorded. The lock keeps the API
// and worker, which start plugin instances together, from racing.
func migrate(ctx context.Context, pool *pgxpool.Pool, ms []Migration) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('catalogkit:' || current_schema()))"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS migration (name text PRIMARY KEY, applied timestamptz NOT NULL DEFAULT now())"); err != nil {
		return err
	}
	for _, m := range ms {
		tag, err := tx.Exec(ctx, "INSERT INTO migration (name) VALUES ($1) ON CONFLICT DO NOTHING", m.Name)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		if _, err := tx.Exec(ctx, m.SQL); err != nil {
			return fmt.Errorf("migration %s: %w", m.Name, err)
		}
	}
	return tx.Commit(ctx)
}

var migrations = []Migration{{Name: "catalogkit/1-items", SQL: `
-- array_to_string isn't immutable, so a generated column can't call it directly.
CREATE FUNCTION tags_text(text[]) RETURNS text
	LANGUAGE sql IMMUTABLE PARALLEL SAFE RETURN array_to_string($1, ' ');

CREATE TABLE item (
	instance text NOT NULL,
	id text NOT NULL,
	kind text NOT NULL,
	name text NOT NULL,
	author text NOT NULL DEFAULT '',
	tags text[] NOT NULL DEFAULT '{}',
	summary text NOT NULL DEFAULT '',
	description text NOT NULL DEFAULT '',
	attrs jsonb NOT NULL DEFAULT '{}',
	added timestamptz NOT NULL DEFAULT now(),
	updated timestamptz NOT NULL,
	removed boolean NOT NULL DEFAULT false,
	latest integer NOT NULL,
	latest_version text NOT NULL,
	content_hash text NOT NULL,
	data_hash text NOT NULL,
	text_simhash bigint,
	image_phash bigint,
	lsh integer[] NOT NULL DEFAULT '{}',
	thumbnail bytea,
	search tsvector GENERATED ALWAYS AS (
		setweight(to_tsvector('english', name), 'A') ||
		setweight(to_tsvector('english', author || ' ' || tags_text(tags)), 'B') ||
		setweight(to_tsvector('english', summary || ' ' || description), 'C')) STORED,
	PRIMARY KEY (instance, id)
);
CREATE INDEX item_search ON item USING gin (search);
CREATE INDEX item_tags ON item USING gin (tags);
CREATE INDEX item_attrs ON item USING gin (attrs jsonb_path_ops);
CREATE INDEX item_lsh ON item USING gin (lsh);
CREATE INDEX item_content_hash ON item (instance, content_hash);
CREATE INDEX item_name ON item (instance, lower(name), id);
CREATE INDEX item_updated ON item (instance, updated, id);

-- data is json, not jsonb, so get returns the bytes content_hash was taken from.
CREATE TABLE item_version (
	instance text NOT NULL,
	item_id text NOT NULL,
	seq integer NOT NULL,
	version text NOT NULL,
	created timestamptz,
	data json NOT NULL,
	PRIMARY KEY (instance, item_id, seq),
	UNIQUE (instance, item_id, version),
	FOREIGN KEY (instance, item_id) REFERENCES item (instance, id) ON DELETE CASCADE
);
`}}

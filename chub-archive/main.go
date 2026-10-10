// Command chub-archive is a catalog plugin over the chub.ai archive's SQL
// dump.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync/atomic"

	"github.com/davidgibbons/innkeeper-plugins/catalogkit"
	"github.com/davidgibbons/innkeeper-plugins/internal/pluginio"
	"github.com/davidgibbons/innkeeper/protocol"
	"github.com/jackc/pgx/v5"
)

const version = "0.1.0"

var catalogConfig = catalogkit.Config{
	Kinds: []string{protocol.KindCard, protocol.KindLorebook},
	Filters: []catalogkit.Filter{
		{CatalogFilter: protocol.CatalogFilter{Name: "site", Label: "Site", Type: protocol.FilterEnum, Options: sites}, Field: "attrs.site"},
		{CatalogFilter: protocol.CatalogFilter{Name: "tag", Label: "Tag", Type: protocol.FilterText}, Field: "tag"},
		{CatalogFilter: protocol.CatalogFilter{Name: "author", Label: "Author", Type: protocol.FilterText}, Field: "author"},
		{CatalogFilter: protocol.CatalogFilter{Name: "added", Label: "Date added", Type: protocol.FilterDateRange}, Field: "added"},
		{CatalogFilter: protocol.CatalogFilter{Name: "rating", Label: "Rating", Type: protocol.FilterNumberRange}, Field: "nums.rating"},
		{CatalogFilter: protocol.CatalogFilter{Name: "hidden", Label: "Hidden on its site", Type: protocol.FilterEnum,
			Options: []protocol.CatalogOption{{Value: "false", Label: "Shown"}, {Value: "true", Label: "Hidden"}}}, Field: "attrs.hidden"},
	},
	Ingest: true,
	Migrations: []catalogkit.Migration{{Name: "chub-archive/1-tables", SQL: `
CREATE TABLE parent (instance text NOT NULL, item_id text NOT NULL, tags text[] NOT NULL, rating float8,
	added timestamptz NOT NULL, hidden boolean NOT NULL, PRIMARY KEY (instance, item_id));
CREATE TABLE source_row (instance text NOT NULL, item_id text NOT NULL, version text NOT NULL,
	image_hash text NOT NULL, PRIMARY KEY (instance, item_id, version));
`}},
}

// env is what initialize set up.
type env struct {
	store   *catalogkit.Store // nil without a database_url
	dump    string            // config.dump
	images  []string          // config.images
	blobTmp string
}

func main() {
	// Set by initialize; requests run on other goroutines.
	var cur atomic.Pointer[env]
	var conn *protocol.Conn
	handler := func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		switch method {
		case protocol.MethodInitialize:
			var p protocol.InitializeParams
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, invalid(err)
			}
			e := &env{blobTmp: p.BlobTmp}
			e.dump, _ = p.Config["dump"].(string)
			images, _ := p.Config["images"].([]any)
			for _, dir := range images {
				if s, ok := dir.(string); ok && s != "" {
					e.images = append(e.images, s)
				}
			}
			if p.DatabaseURL != "" {
				var err error
				if e.store, err = catalogkit.Open(ctx, p.DatabaseURL, p.InstanceID, catalogConfig); err != nil {
					return nil, err
				}
			}
			cur.Store(e)
			return protocol.InitializeResult{Protocol: protocol.Version, Name: "chub-archive", Version: version,
				Capabilities: []string{"catalog"}}, nil
		case protocol.MethodShutdown:
			if e := cur.Load(); e != nil && e.store != nil {
				e.store.Close()
			}
			return nil, nil
		case protocol.MethodCatalogDescribe:
			return catalogkit.Describe(catalogConfig), nil
		case protocol.MethodCatalogSearch, protocol.MethodCatalogGet, protocol.MethodCatalogSimilar, protocol.MethodCatalogIngest:
		default:
			return nil, protocol.MethodNotFound(method)
		}
		e := cur.Load()
		if e == nil {
			return nil, fmt.Errorf("%s before initialize", method)
		}
		// Search checks its params first, so plugin check's filter check
		// passes without a database too.
		if method == protocol.MethodCatalogSearch {
			var p protocol.CatalogSearchParams
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, invalid(err)
			}
			if err := catalogkit.Describe(catalogConfig).CheckSearch(p); err != nil {
				return nil, invalid(err)
			}
			if e.store == nil {
				return nil, errNoDatabase
			}
			return e.store.Search(ctx, p)
		}
		if e.store == nil {
			return nil, errNoDatabase
		}
		switch method {
		case protocol.MethodCatalogGet:
			return get(ctx, e, params)
		case protocol.MethodCatalogSimilar:
			var p protocol.CatalogSimilarParams
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, invalid(err)
			}
			return e.store.Similar(ctx, p)
		}
		if e.dump == "" {
			return nil, invalid(errors.New("set dump in this instance's config before ingesting"))
		}
		return ingest(ctx, e, conn.Notify, params)
	}
	conn = protocol.NewConn(os.Stdin, os.Stdout, handler)
	if err := conn.Run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var errNoDatabase = errors.New("no database: the core sends database_url to a plugin whose manifest sets database = true")

// get answers catalog.get, with the version's image from the archive as
// its avatar.
func get(ctx context.Context, e *env, raw json.RawMessage) (protocol.CatalogGetResult, error) {
	var p protocol.CatalogGetParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return protocol.CatalogGetResult{}, invalid(err)
	}
	res, err := e.store.Get(ctx, p.ID, p.Version)
	if err != nil || !p.Avatar || res.Item.Kind != protocol.KindCard {
		return res, err
	}
	var hash string
	err = e.store.Pool().QueryRow(ctx, `SELECT image_hash FROM source_row WHERE instance = $1 AND item_id = $2 AND version = $3`,
		e.store.Instance(), p.ID, res.Version).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, nil
	}
	if err != nil {
		return res, err
	}
	b, err := readImage(e.images, hash)
	if err != nil {
		// The item is still worth showing without its avatar.
		log.Printf("get %s version %s: image %s: %v", p.ID, res.Version, hash, err)
		return res, nil
	}
	if b != nil {
		res.Avatar, err = pluginio.WriteTmp(e.blobTmp, "avatar-*.png", b)
	}
	return res, err
}

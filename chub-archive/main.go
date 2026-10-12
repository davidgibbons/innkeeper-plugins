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
	store      *catalogkit.Store // nil without a database_url
	dump       string            // config.dump
	images     []string          // config.images
	imageStore string            // config.image_store
	blobTmp    string
}

func main() {
	err := catalogkit.Serve(catalogkit.Plugin[*env]{Name: "chub-archive", Version: version, Config: catalogConfig,
		Open: func(p protocol.InitializeParams, store *catalogkit.Store) (*env, error) {
			e := &env{store: store, blobTmp: p.BlobTmp}
			e.dump, _ = p.Config["dump"].(string)
			e.imageStore, _ = p.Config["image_store"].(string)
			var err error
			if e.images, err = configImages(p.Config); err != nil {
				return nil, invalid(err)
			}
			return e, nil
		},
		Get: get, Ingest: ingest})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// configImages reads config.images, a list of folders. Empty entries are skipped.
func configImages(cfg map[string]any) ([]string, error) {
	v, ok := cfg["images"]
	if !ok || v == nil {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, errors.New("config images must be a list of folder paths")
	}
	var dirs []string
	for _, dir := range list {
		s, ok := dir.(string)
		if !ok {
			return nil, errors.New("config images must be a list of folder paths")
		}
		if s != "" {
			dirs = append(dirs, s)
		}
	}
	return dirs, nil
}

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

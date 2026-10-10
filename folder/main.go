// Command folder is a catalog plugin over a folder of character cards and
// lorebooks.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"sync/atomic"
	"time"

	"github.com/davidgibbons/innkeeper-plugins/card"
	"github.com/davidgibbons/innkeeper-plugins/catalogkit"
	"github.com/davidgibbons/innkeeper-plugins/internal/pluginio"
	"github.com/davidgibbons/innkeeper/protocol"
)

const version = "0.1.0"

var catalogConfig = catalogkit.Config{
	Kinds: []string{protocol.KindCard, protocol.KindLorebook},
	Filters: []catalogkit.Filter{
		{CatalogFilter: protocol.CatalogFilter{Name: "subdirectory", Label: "Subfolder", Type: protocol.FilterText}, Field: "attrs.dir"},
		{CatalogFilter: protocol.CatalogFilter{Name: "tag", Label: "Tag", Type: protocol.FilterText}, Field: "tag"},
		{CatalogFilter: protocol.CatalogFilter{Name: "modified", Label: "Date modified", Type: protocol.FilterDateRange}, Field: "updated"},
	},
	Ingest: true,
	Migrations: []catalogkit.Migration{{Name: "folder/1-file",
		SQL: `CREATE TABLE file (instance text NOT NULL, path text NOT NULL, mtime timestamptz NOT NULL,
			size bigint NOT NULL, PRIMARY KEY (instance, path))`}},
}

// env is what initialize set up.
type env struct {
	store   *catalogkit.Store // nil without a database_url
	folder  string            // config.folder
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
			e.folder, _ = p.Config["folder"].(string)
			if p.DatabaseURL != "" {
				var err error
				if e.store, err = catalogkit.Open(ctx, p.DatabaseURL, p.InstanceID, catalogConfig); err != nil {
					return nil, err
				}
			}
			cur.Store(e)
			return protocol.InitializeResult{Protocol: protocol.Version, Name: "folder", Version: version,
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
		if e.folder == "" {
			return nil, invalid(errors.New("set folder in this instance's config before ingesting"))
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

// get answers catalog.get. The avatar comes from the file, so only the
// latest version of an unchanged file has one.
func get(ctx context.Context, e *env, raw json.RawMessage) (protocol.CatalogGetResult, error) {
	var p protocol.CatalogGetParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return protocol.CatalogGetResult{}, invalid(err)
	}
	res, err := e.store.Get(ctx, p.ID, p.Version)
	if err != nil || !p.Avatar || res.Item.Kind != protocol.KindCard || res.Version != res.Item.LatestVersion || e.folder == "" {
		return res, err
	}
	img, err := currentAvatar(ctx, e, p.ID)
	if err != nil {
		log.Printf("%s: avatar: %v", p.ID, err)
		return res, nil
	}
	if img != nil {
		res.Avatar, err = pluginio.WriteTmp(e.blobTmp, "avatar-*.png", img)
	}
	return res, err
}

// currentAvatar reads id's avatar if the file is unchanged since ingest, or
// returns nil.
func currentAvatar(ctx context.Context, e *env, id string) ([]byte, error) {
	if !fs.ValidPath(id) {
		return nil, nil
	}
	root, err := os.OpenRoot(e.folder)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Stat(id)
	if err != nil {
		return nil, err
	}
	var mtime time.Time
	var size int64
	if err := e.store.Pool().QueryRow(ctx, `SELECT mtime, size FROM file WHERE instance = $1 AND path = $2`,
		e.store.Instance(), id).Scan(&mtime, &size); err != nil {
		return nil, err
	}
	if !mtime.Equal(modTime(info)) || size != info.Size() {
		return nil, nil
	}
	b, err := pluginio.ReadIn(root, id)
	if err != nil {
		return nil, err
	}
	_, img, err := card.Decode(b)
	return img, err
}

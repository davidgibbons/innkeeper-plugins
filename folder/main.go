// Command folder is a catalog plugin over a folder of character cards and
// lorebooks.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"time"

	"github.com/inkandale/innkeeper-plugins/card"
	"github.com/inkandale/innkeeper-plugins/catalogkit"
	"github.com/inkandale/innkeeper-plugins/internal/pluginio"
	"github.com/inkandale/innkeeper/protocol"
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
	err := catalogkit.Serve(catalogkit.Plugin[*env]{Name: "folder", Version: version, Config: catalogConfig,
		Open: func(p protocol.InitializeParams, store *catalogkit.Store) (*env, error) {
			e := &env{store: store, blobTmp: p.BlobTmp}
			e.folder, _ = p.Config["folder"].(string)
			return e, nil
		},
		Get: get, Ingest: ingest})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

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

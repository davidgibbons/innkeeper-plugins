package catalogkit

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/davidgibbons/innkeeper/protocol"
	"github.com/jackc/pgx/v5"
)

// itemColumns are the columns itemDest scans, in order.
const itemColumns = `id, kind, name, author, tags, summary, latest_version, content_hash, text_simhash, image_phash, thumbnail`

func itemDest(it *protocol.CatalogItem) []any {
	return []any{&it.ID, &it.Kind, &it.Name, &it.Author, &it.Tags, &it.Summary, &it.LatestVersion,
		&it.Hashes.ContentHash, &it.Hashes.TextSimhash, &it.Hashes.ImagePhash, &it.Thumbnail}
}

func invalid(err error) error {
	return protocol.NewError(protocol.CodeInvalidParams, err.Error(), false)
}

func notFound(format string, args ...any) error {
	return protocol.NewError(protocol.CodeRemoteNotFound, fmt.Sprintf(format, args...), false)
}

// Get returns version of item id, or its latest when version is empty.
// Removed items still answer.
func (s *Store) Get(ctx context.Context, id, version string) (protocol.CatalogGetResult, error) {
	var res protocol.CatalogGetResult
	err := s.pool.QueryRow(ctx, `SELECT `+itemColumns+` FROM item WHERE id = $1`, id).Scan(itemDest(&res.Item)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, notFound("no item %q", id)
	}
	if err != nil {
		return res, err
	}
	rows, _ := s.pool.Query(ctx, `SELECT version, created FROM item_version WHERE item_id = $1 ORDER BY seq DESC`, id)
	res.Versions, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (protocol.CatalogVersion, error) {
		var v protocol.CatalogVersion
		var created *time.Time
		err := r.Scan(&v.Version, &created)
		if created != nil {
			v.Created = created.UTC().Format(time.RFC3339)
		}
		return v, err
	})
	if err != nil {
		return res, err
	}
	res.Version = cmp.Or(version, res.Item.LatestVersion)
	var data string
	err = s.pool.QueryRow(ctx, `SELECT data FROM item_version WHERE item_id = $1 AND version = $2`, id, res.Version).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, notFound("item %q has no version %q", id, res.Version)
	}
	res.Data = json.RawMessage(data)
	return res, err
}

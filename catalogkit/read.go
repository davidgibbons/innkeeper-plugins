package catalogkit

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
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
	err := s.pool.QueryRow(ctx, `SELECT `+itemColumns+` FROM item WHERE instance = $1 AND id = $2`, s.instance, id).Scan(itemDest(&res.Item)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, notFound("no item %q", id)
	}
	if err != nil {
		return res, err
	}
	rows, _ := s.pool.Query(ctx, `SELECT version, created FROM item_version WHERE instance = $1 AND item_id = $2 ORDER BY seq DESC`, s.instance, id)
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
	err = s.pool.QueryRow(ctx, `SELECT data FROM item_version WHERE instance = $1 AND item_id = $2 AND version = $3`, s.instance, id, res.Version).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, notFound("item %q has no version %q", id, res.Version)
	}
	res.Data = json.RawMessage(data)
	return res, err
}

const defaultPage = 50

// cursor is the last row of a page: its sort key as Postgres prints it,
// and its ID.
type cursor struct {
	Sort string `json:"s"`
	Key  string `json:"k"`
	ID   string `json:"id"`
}

// query collects a WHERE clause and its arguments.
type query struct {
	conds []string
	args  []any
}

func (b *query) arg(v any) string {
	b.args = append(b.args, v)
	return fmt.Sprintf("$%d", len(b.args))
}

func (b *query) where(cond string) { b.conds = append(b.conds, cond) }

// Search answers catalog.search over items not removed.
func (s *Store) Search(ctx context.Context, p protocol.CatalogSearchParams) (protocol.CatalogSearchResult, error) {
	res := protocol.CatalogSearchResult{Items: []protocol.CatalogItem{}}
	if err := Describe(s.cfg).CheckSearch(p); err != nil {
		return res, invalid(err)
	}
	b := &query{}
	b.where("instance = " + b.arg(s.instance))
	b.where("NOT removed")
	text := strings.TrimSpace(p.Q)
	var tsq string
	if text != "" {
		tsq = "websearch_to_tsquery('english', " + b.arg(text) + ")"
		b.where("search @@ " + tsq)
	}
	if p.Kind != "" {
		b.where("kind = " + b.arg(p.Kind))
	}
	for name, raw := range p.Filters {
		s.filter(b, name, raw)
	}

	sort := cmp.Or(p.Sort, sorts[0].Value)
	if sort == "relevance" && text == "" {
		sort = "newest"
	}
	// Each sort orders by its key, then id in the same direction, so one
	// row comparison continues from the cursor.
	var key, cast, dir, after string
	switch sort {
	case "relevance":
		key, cast, dir, after = "ts_rank(search, "+tsq+")::float8", "float8", "DESC", "<"
	case "name":
		key, cast, dir, after = "lower(name)", "text", "ASC", ">"
	default:
		key, cast, dir, after = "updated", "timestamptz", "DESC", "<"
	}
	if p.Page.Cursor != "" {
		var c cursor
		raw, err := base64.RawURLEncoding.DecodeString(p.Page.Cursor)
		if err == nil {
			err = json.Unmarshal(raw, &c)
		}
		if err != nil || c.Sort != sort {
			return res, invalid(errors.New("page.cursor didn't come from this search"))
		}
		b.where(fmt.Sprintf("(%s, id) %s (%s::%s, %s)", key, after, b.arg(c.Key), cast, b.arg(c.ID)))
	}
	size := cmp.Or(p.Page.Size, defaultPage)
	sql := fmt.Sprintf(`SELECT %s, (%s)::text FROM item WHERE %s ORDER BY %s %s, id %s LIMIT %d`,
		itemColumns, key, strings.Join(b.conds, " AND "), key, dir, dir, size+1)
	rows, err := s.pool.Query(ctx, sql, b.args...)
	if err != nil {
		return res, err
	}
	defer rows.Close()
	var lastKey string
	for rows.Next() {
		if len(res.Items) == size {
			next, _ := json.Marshal(cursor{Sort: sort, Key: lastKey, ID: res.Items[size-1].ID})
			res.Next = base64.RawURLEncoding.EncodeToString(next)
			break
		}
		var it protocol.CatalogItem
		if err := rows.Scan(append(itemDest(&it), &lastKey)...); err != nil {
			return res, err
		}
		res.Items = append(res.Items, it)
	}
	return res, rows.Err()
}

// filter adds one declared filter. CheckSearch has already checked the
// value's shape, and Open the filter's field.
func (s *Store) filter(b *query, name string, raw json.RawMessage) {
	f := s.cfg.Filters[slices.IndexFunc(s.cfg.Filters, func(f Filter) bool { return f.Name == name })]
	switch f.Type {
	case protocol.FilterText:
		var v string
		_ = json.Unmarshal(raw, &v)
		if f.Field == "tag" {
			b.where("tags @> ARRAY[" + b.arg(strings.ToLower(v)) + "::text]")
			return
		}
		match, _ := json.Marshal(map[string][]string{strings.TrimPrefix(f.Field, "attrs."): {v}})
		b.where("attrs @> " + b.arg(string(match)) + "::jsonb")
	case protocol.FilterDateRange:
		var r protocol.DateRange
		_ = json.Unmarshal(raw, &r)
		if r.From != "" {
			b.where(f.Field + " >= " + b.arg(r.From) + "::date")
		}
		if r.To != "" {
			b.where(f.Field + " < " + b.arg(r.To) + "::date + 1")
		}
	}
}

// Similar answers catalog.similar: items of the kind with the same
// content_hash, or within MaxDistance bits by simhash or phash, closest first.
func (s *Store) Similar(ctx context.Context, p protocol.CatalogSimilarParams) (protocol.CatalogSimilarResult, error) {
	res := protocol.CatalogSimilarResult{Items: []protocol.CatalogItem{}}
	switch {
	case !slices.Contains(s.cfg.Kinds, p.Kind):
		return res, invalid(fmt.Errorf("kind %q is not declared", p.Kind))
	case p.Limit < 1 || p.Limit > protocol.MaxCatalogPage:
		return res, invalid(fmt.Errorf("limit %d must be 1 to %d", p.Limit, protocol.MaxCatalogPage))
	case p.MaxDistance < 0 || p.MaxDistance > MaxDistance:
		return res, invalid(fmt.Errorf("max_distance %d must be 0 to %d", p.MaxDistance, MaxDistance))
	}
	// least() skips the NULL a missing hash gives.
	rows, _ := s.pool.Query(ctx, `
		SELECT `+itemColumns+` FROM (
			SELECT *, least(
				CASE WHEN content_hash = $2 THEN 0 END,
				bit_count((text_simhash # $3)::bit(64)),
				bit_count((image_phash # $4)::bit(64))) AS distance
			FROM item
			WHERE instance = $8 AND kind = $1 AND NOT removed AND (content_hash = $2 OR lsh && $5::int4[])) m
		WHERE distance <= $6
		ORDER BY distance, id
		LIMIT $7`,
		p.Kind, p.Hashes.ContentHash, p.Hashes.TextSimhash, p.Hashes.ImagePhash,
		lsh(p.Hashes.TextSimhash, p.Hashes.ImagePhash), p.MaxDistance, p.Limit, s.instance)
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (protocol.CatalogItem, error) {
		var it protocol.CatalogItem
		err := r.Scan(itemDest(&it)...)
		return it, err
	})
	if err != nil {
		return res, err
	}
	res.Items = append(res.Items, items...)
	return res, nil
}

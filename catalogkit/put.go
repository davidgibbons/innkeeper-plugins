package catalogkit

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"strconv"
	"strings"
	"time"

	"github.com/davidgibbons/innkeeper/cardhash"
	"github.com/davidgibbons/innkeeper/protocol"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/image/draw"
)

// ErrBadData wraps Put's errors about the entry itself, which an ingest skips.
var ErrBadData = errors.New("catalogkit: bad data")

// Entry is one item as a plugin read it from its source.
type Entry struct {
	ID   string
	Kind string // protocol.KindCard or protocol.KindLorebook
	// Data is a CCv3 card, or a CCv3 character_book for a lorebook.
	Data json.RawMessage
	// Image is the card's avatar, or nil. It gives the thumbnail and phash.
	Image image.Image
	// Attrs are the values attrs.<key> filters match.
	Attrs map[string][]string
	// Updated is when the source last changed. It dates a new version.
	Updated time.Time
}

// Change is what Put did.
type Change int

const (
	Unchanged Change = iota
	Added
	Updated
)

// Put stores e. Data that hashes differently from the latest version adds a
// version; the same data only refreshes the item's other fields.
func (s *Store) Put(ctx context.Context, e Entry) (Change, error) {
	d, err := derive(e)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrBadData, e.ID, err)
	}
	if e.Attrs == nil {
		e.Attrs = map[string][]string{}
	}
	attrs, err := json.Marshal(e.Attrs)
	if err != nil {
		return 0, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var hash string
	var latest int
	var removed bool
	err = tx.QueryRow(ctx, `SELECT data_hash, latest, removed FROM item WHERE id = $1 FOR UPDATE`, e.ID).
		Scan(&hash, &latest, &removed)
	change, newVersion := Unchanged, true
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		change, latest = Added, 1
	case err != nil:
		return 0, err
	case hash != d.dataHash:
		change, latest = Updated, latest+1
	default:
		newVersion = false
		if removed {
			change = Updated
		}
	}
	version := strconv.Itoa(latest)
	_, err = tx.Exec(ctx, `
		INSERT INTO item (id, kind, name, author, tags, summary, description, attrs, updated, latest,
			latest_version, content_hash, data_hash, text_simhash, image_phash, lsh, thumbnail)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (id) DO UPDATE SET kind = excluded.kind, name = excluded.name, author = excluded.author,
			tags = excluded.tags, summary = excluded.summary, description = excluded.description,
			attrs = excluded.attrs, updated = excluded.updated, latest = excluded.latest,
			latest_version = excluded.latest_version, content_hash = excluded.content_hash, data_hash = excluded.data_hash,
			text_simhash = excluded.text_simhash, image_phash = excluded.image_phash, lsh = excluded.lsh,
			thumbnail = excluded.thumbnail, removed = false`,
		e.ID, e.Kind, d.name, d.author, d.tags, d.summary, d.description, string(attrs), e.Updated, latest,
		version, d.contentHash, d.dataHash, d.simhash, d.phash, lsh(d.simhash, d.phash), d.thumbnail)
	if err != nil {
		return 0, badData(e.ID, err)
	}
	if newVersion {
		_, err = tx.Exec(ctx, `INSERT INTO item_version (item_id, seq, version, created, data) VALUES ($1, $2, $3, $4, $5)`,
			e.ID, latest, version, e.Updated, string(e.Data))
		if err != nil {
			return 0, badData(e.ID, err)
		}
	}
	return change, tx.Commit(ctx)
}

// badData wraps a data exception (SQLSTATE class 22), such as a NUL in text or
// invalid UTF-8 in json, in ErrBadData so an ingest skips the entry.
func badData(id string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && strings.HasPrefix(pgErr.Code, "22") {
		return fmt.Errorf("%w: %s: %w", ErrBadData, id, err)
	}
	return err
}

// MarkRemovedExcept marks every item not in keep removed and returns how
// many it marked. Removed items leave search and similar, but get still
// answers for them, so adopted cards keep a working source.
func (s *Store) MarkRemovedExcept(ctx context.Context, keep []string) (int64, error) {
	if keep == nil {
		keep = []string{} // NULL would match nothing
	}
	tag, err := s.pool.Exec(ctx, `UPDATE item SET removed = true WHERE NOT removed AND id <> ALL($1)`, keep)
	return tag.RowsAffected(), err
}

// derived is what Put stores beside the entry's own fields.
type derived struct {
	name, author, summary, description string
	tags                               []string
	contentHash, dataHash              string
	simhash, phash                     *int64
	thumbnail                          []byte
}

// derive hashes e as the library does: a card over cardhash.CardData, a
// lorebook whole.
func derive(e Entry) (derived, error) {
	d := derived{tags: []string{}}
	hashed := []byte(e.Data)
	var sim int64
	var ok bool
	var err error
	switch e.Kind {
	case protocol.KindCard:
		if hashed, _, err = cardhash.CardData(e.Data); err == nil && hashed == nil {
			err = errors.New("the card has no data")
		}
		if err == nil {
			sim, ok, err = cardhash.CardSimhash(hashed)
		}
	case protocol.KindLorebook:
		sim, ok, err = cardhash.LorebookSimhash(hashed)
	default:
		err = fmt.Errorf("kind %q: catalogkit stores cards and lorebooks", e.Kind)
	}
	if err != nil {
		return d, err
	}
	if ok {
		d.simhash = &sim
	}
	if d.contentHash, err = cardhash.ContentHash(hashed); err != nil {
		return d, err
	}
	// The whole entry, so edits content_hash ignores (an embedded lorebook) still version.
	if d.dataHash, err = cardhash.ContentHash(e.Data); err != nil {
		return d, err
	}
	var m map[string]any
	if err := json.Unmarshal(hashed, &m); err != nil {
		return d, err
	}
	str := func(k string) string { s, _ := m[k].(string); return strings.TrimSpace(s) }
	d.name, d.author, d.description = cmp.Or(str("name"), e.ID), str("creator"), str("description")
	d.summary = truncate(cmp.Or(str("creator_notes"), d.description), 300)
	tags, _ := m["tags"].([]any)
	for _, t := range tags {
		if s, _ := t.(string); strings.TrimSpace(s) != "" {
			d.tags = append(d.tags, strings.ToLower(strings.TrimSpace(s)))
		}
	}
	if e.Image != nil {
		p := cardhash.Phash(e.Image)
		d.phash = &p
		d.thumbnail = thumbnail(e.Image)
	}
	return d, nil
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return strings.TrimSpace(string(r[:n])) + "…"
	}
	return s
}

// thumbnail scales img to 160 pixels wide on white, as a JPEG of at most
// protocol.MaxThumbnail bytes, or nil if no quality fits.
func thumbnail(img image.Image) []byte {
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return nil
	}
	w := min(160, b.Dx())
	h := max(1, b.Dy()*w/b.Dx())
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	for _, q := range []int{80, 60, 40} {
		var buf bytes.Buffer
		if jpeg.Encode(&buf, dst, &jpeg.Options{Quality: q}) == nil && buf.Len() <= protocol.MaxThumbnail {
			return buf.Bytes()
		}
	}
	return nil
}

// bands splits a 64-bit hash into 9 bands of 7 bits, ignoring the top bit.
// Hashes within MaxDistance bits differ in at most 8 bands, so they share
// at least one.
const bands = 9

// MaxDistance is the largest max_distance Similar serves without missing
// matches.
const MaxDistance = bands - 1

// lsh returns the bands of each hash, tagged with the hash and band number
// so a simhash band never equals a phash band.
func lsh(simhash, phash *int64) []int32 {
	out := []int32{}
	for which, h := range []*int64{simhash, phash} {
		if h == nil {
			continue
		}
		for b := range bands {
			bucket := int32(uint64(*h)>>(7*b)) & 0x7f
			out = append(out, int32(which)<<12|int32(b)<<8|bucket)
		}
	}
	return out
}

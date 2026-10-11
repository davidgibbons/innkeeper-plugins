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
	"math"
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
	// Numbers are the values nums.<key> filters match.
	Numbers map[string]float64
	// Author, when not empty, replaces the creator in Data. Tags replace the
	// data's tags when it has any entries.
	Author string
	Tags   []string
	// Added is when the source first listed the item, or zero for the time
	// of the first Put. The item keeps the earliest Added it is given.
	Added time.Time
	// Version numbers this version from 1, and versions sort by it. Zero
	// lets Put number them 1, 2, 3 as the data changes.
	Version int
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
// version, or with e.Version set, a version number not seen before does.
// A version number already stored is left as it is. Otherwise Put only
// refreshes the item's other fields.
func (s *Store) Put(ctx context.Context, e Entry) (Change, error) {
	if e.Version < 0 || e.Version > math.MaxInt32 {
		return 0, fmt.Errorf("%w: %s: version %d", ErrBadData, e.ID, e.Version)
	}
	d, err := derive(e)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrBadData, e.ID, err)
	}
	if e.Attrs == nil {
		e.Attrs = map[string][]string{}
	}
	if e.Numbers == nil {
		e.Numbers = map[string]float64{}
	}
	attrs, err := json.Marshal(e.Attrs)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrBadData, e.ID, err)
	}
	nums, err := json.Marshal(e.Numbers)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrBadData, e.ID, err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// FOR UPDATE locks nothing when the row is missing, so two new puts of one id would both insert version 1.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('catalogkit-item:' || $1 || '/' || $2))`, s.instance, e.ID); err != nil {
		return 0, err
	}

	var hash string
	var latest int
	var removed bool
	err = tx.QueryRow(ctx, `SELECT data_hash, latest, removed FROM item WHERE instance = $1 AND id = $2 FOR UPDATE`, s.instance, e.ID).
		Scan(&hash, &latest, &removed)
	// seq numbers e's version. current means e is the latest version, so
	// the item's fields come from it.
	change, seq, current, newVersion := Unchanged, e.Version, true, true
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		change, seq = Added, cmp.Or(e.Version, 1)
	case err != nil:
		return 0, err
	case e.Version > 0:
		err = tx.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM item_version WHERE instance = $1 AND item_id = $2 AND seq = $3)`,
			s.instance, e.ID, e.Version).Scan(&newVersion)
		if err != nil {
			return 0, err
		}
		current = newVersion && e.Version >= latest
	case hash != d.dataHash:
		seq = latest + 1
	default:
		seq, newVersion = latest, false
	}
	if change == Unchanged && (newVersion || removed) {
		change = Updated
	}
	version := strconv.Itoa(seq)
	var added *time.Time // NULL keeps the stored date
	if !e.Added.IsZero() {
		added = &e.Added
	}
	if current {
		_, err = tx.Exec(ctx, `
			INSERT INTO item (instance, id, kind, name, author, tags, summary, description, attrs, updated, latest,
				latest_version, content_hash, data_hash, text_simhash, image_phash, lsh, thumbnail, nums, added)
			VALUES ($18, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $19,
				COALESCE($20::timestamptz, now()))
			ON CONFLICT (instance, id) DO UPDATE SET kind = excluded.kind, name = excluded.name, author = excluded.author,
				tags = excluded.tags, summary = excluded.summary, description = excluded.description,
				attrs = excluded.attrs, updated = excluded.updated, latest = excluded.latest,
				latest_version = excluded.latest_version, content_hash = excluded.content_hash, data_hash = excluded.data_hash,
				text_simhash = excluded.text_simhash, image_phash = excluded.image_phash, lsh = excluded.lsh,
				thumbnail = excluded.thumbnail, nums = excluded.nums, added = LEAST(item.added, $20), removed = false`,
			e.ID, e.Kind, d.name, d.author, d.tags, d.summary, d.description, string(attrs), e.Updated, seq,
			version, d.contentHash, d.dataHash, d.simhash, d.phash, lsh(d.simhash, d.phash), d.thumbnail, s.instance,
			string(nums), added)
	} else {
		// An older or repeated version only moves the date added back, and the
		// source still lists the item.
		_, err = tx.Exec(ctx, `UPDATE item SET added = LEAST(added, $3), removed = false WHERE instance = $1 AND id = $2`, s.instance, e.ID, added)
	}
	if err != nil {
		return 0, BadData(e.ID, err)
	}
	if newVersion {
		_, err = tx.Exec(ctx, `INSERT INTO item_version (instance, item_id, seq, version, created, data) VALUES ($1, $2, $3, $4, $5, $6)`,
			s.instance, e.ID, seq, version, e.Updated, string(e.Data))
		if err != nil {
			return 0, BadData(e.ID, err)
		}
	}
	return change, tx.Commit(ctx)
}

// BadData wraps a data exception (SQLSTATE class 22), such as a NUL in text
// or invalid UTF-8 in json, or a program limit exceeded (54000), such as text
// too long for a tsvector, in ErrBadData, so an ingest skips the entry
// instead of failing.
func BadData(id string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (strings.HasPrefix(pgErr.Code, "22") || pgErr.Code == "54000") {
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
	tag, err := s.pool.Exec(ctx, `UPDATE item SET removed = true WHERE instance = $1 AND NOT removed AND id <> ALL($2)`, s.instance, keep)
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
	var tags []string
	raw, _ := m["tags"].([]any)
	for _, t := range raw {
		if s, ok := t.(string); ok {
			tags = append(tags, s)
		}
	}
	if len(e.Tags) > 0 {
		tags = e.Tags
	}
	for _, t := range tags {
		if t = strings.TrimSpace(t); t != "" {
			d.tags = append(d.tags, strings.ToLower(t))
		}
	}
	if a := strings.TrimSpace(e.Author); a != "" {
		d.author = a
	}
	// 500 runes fit the btree row limit of the name index, even at 4 bytes each.
	d.name, d.author = truncate(d.name, 500), truncate(d.author, 500)
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

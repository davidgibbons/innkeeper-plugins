package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/davidgibbons/innkeeper-plugins/card"
	"github.com/davidgibbons/innkeeper-plugins/catalogkit"
	"github.com/davidgibbons/innkeeper/protocol"
	"github.com/jackc/pgx/v5"
)

// source is a table whose rows are card or lorebook versions.
type source struct {
	kind  string
	idCol string // the column naming the item
	site  string
}

var sources = map[string]source{
	"chub_character_def":   {protocol.KindCard, "id", "chub"},
	"chub_lorebook_def":    {protocol.KindLorebook, "id", "chub"},
	"booru_character_def":  {protocol.KindCard, "id", "booru"},
	"nyaime_character_def": {protocol.KindCard, "id", "nyaime"},
	"risuai_character_def": {protocol.KindCard, "id", "risuai"},
	// These have no source ID. The hash covers the card's image and definition.
	"webring_character_def": {protocol.KindCard, "card_data_hash", "webring"},
	"generic_character_def": {protocol.KindCard, "card_data_hash", "generic"},
}

// parents are chub's per-item tables, with the table of versions each
// describes. pg_dump writes each before its versions.
var parents = map[string]string{"chub_character": "chub_character_def", "chub_lorebook": "chub_lorebook_def"}

// read reports whether ingest reads table's rows.
func read(table string) bool {
	_, ok := sources[table]
	_, parent := parents[table]
	return ok || parent
}

var sites = []protocol.CatalogOption{
	{Value: "chub", Label: "Chub"},
	{Value: "booru", Label: "Pygmalion Booru"},
	{Value: "nyaime", Label: "Nyai.me"},
	{Value: "risuai", Label: "RisuRealm"},
	{Value: "webring", Label: "Webring"},
	{Value: "generic", Label: "Other (JanitorAI, catbox)"},
}

// pgTime parses a timestamptz as pg_dump writes it, such as
// 2024-05-06 20:00:28.368094+00.
func pgTime(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02 15:04:05.999999-07", "2006-01-02 15:04:05.999999-07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%w: timestamp %q", errBadRow, s)
}

// putParent stores a chub per-item row for its versions to read.
func putParent(ctx context.Context, s *catalogkit.Store, table string, row map[string]string) error {
	var data struct {
		Rating *float64 `json:"rating"`
		Topics []string `json:"topics"`
	}
	if err := json.Unmarshal([]byte(row["data"]), &data); err != nil {
		return fmt.Errorf("%w: data: %w", errBadRow, err)
	}
	added, err := pgTime(row["added"])
	if err != nil {
		return err
	}
	if data.Topics == nil {
		data.Topics = []string{}
	}
	id := parents[table] + ":" + row["id"]
	_, err = s.Pool().Exec(ctx, `
		INSERT INTO parent (instance, item_id, tags, rating, added, hidden) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (instance, item_id) DO UPDATE SET tags = excluded.tags, rating = excluded.rating,
			added = excluded.added, hidden = excluded.hidden`,
		s.Instance(), id, data.Topics, data.Rating, added, row["hidden"] == "t")
	return catalogkit.BadData(id, err)
}

// entry turns a row of a source table into one version of its item.
func entry(ctx context.Context, s *catalogkit.Store, table string, row map[string]string) (catalogkit.Entry, error) {
	src := sources[table]
	e := catalogkit.Entry{ID: table + ":" + row[src.idCol], Kind: src.kind, Author: row["author"], Version: 1,
		Attrs: map[string][]string{"site": {src.site}, "hidden": {strconv.FormatBool(row["hidden"] == "t")}}}
	if row[src.idCol] == "" {
		return e, fmt.Errorf("%w: no %s", errBadRow, src.idCol)
	}
	var err error
	if inc, ok := row["increment"]; ok {
		if e.Version, err = strconv.Atoi(inc); err != nil || e.Version < 1 {
			return e, fmt.Errorf("%w: increment %q", errBadRow, inc)
		}
	}
	if e.Updated, err = pgTime(row["added"]); err != nil {
		return e, err
	}
	e.Added = e.Updated
	if src.kind == protocol.KindLorebook {
		e.Data, err = card.DecodeLorebook([]byte(row["definition"]), row["name"])
	} else {
		e.Data, _, err = card.Decode([]byte(row["definition"]))
	}
	if err != nil {
		return e, fmt.Errorf("%w: %w", errBadRow, err)
	}

	var tags []string
	var rating *float64
	var added time.Time
	var hidden bool
	err = s.Pool().QueryRow(ctx, `SELECT tags, rating, added, hidden FROM parent WHERE instance = $1 AND item_id = $2`,
		s.Instance(), e.ID).Scan(&tags, &rating, &added, &hidden)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return e, nil
	case err != nil:
		return e, err
	}
	e.Tags, e.Added = tags, added
	if rating != nil {
		e.Numbers = map[string]float64{"rating": *rating}
	}
	if hidden {
		e.Attrs["hidden"] = []string{"true"}
	}
	return e, nil
}

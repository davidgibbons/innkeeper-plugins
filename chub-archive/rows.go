package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/inkandale/innkeeper-plugins/card"
	"github.com/inkandale/innkeeper/protocol"
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

// parentTable is chub's per-item card table, which pg_dump writes before
// parentVersions, the versions it describes.
const (
	parentTable    = "chub_character"
	parentVersions = "chub_character_def"
)

// read reports whether the import reads table's rows.
func read(table string) bool {
	_, ok := sources[table]
	return ok || table == parentTable
}

// parent is what a chub per-item row adds to its versions.
type parent struct {
	rating *float64
	topics []string
	hidden bool
}

// readParent reads a chub per-item row, keyed by its versions' item ID.
func readParent(row map[string]string) (string, parent, error) {
	var data struct {
		Rating *float64 `json:"rating"`
		Topics []string `json:"topics"`
	}
	if err := json.Unmarshal([]byte(row["data"]), &data); err != nil {
		return "", parent{}, fmt.Errorf("%w: data: %w", errBadRow, err)
	}
	return parentVersions + ":" + row["id"], parent{rating: data.Rating, topics: data.Topics, hidden: row["hidden"] == "t"}, nil
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

// entry is one row of a source table, as the import sends it.
type entry struct {
	item    string
	kind    string
	label   string
	created time.Time
	data    json.RawMessage // a CCv3 card, or a character_book
	values  map[string]json.RawMessage
}

// readVersion turns a row of a source table into one version of its item.
// parents holds chub's per-item rows read so far.
func readVersion(table string, row map[string]string, parents map[string]parent) (entry, error) {
	src := sources[table]
	v := entry{item: table + ":" + row[src.idCol], kind: src.kind, label: "Version 1"}
	if row[src.idCol] == "" {
		return v, fmt.Errorf("%w: no %s", errBadRow, src.idCol)
	}
	if inc, ok := row["increment"]; ok {
		if n, err := strconv.Atoi(inc); err != nil || n < 1 {
			return v, fmt.Errorf("%w: increment %q", errBadRow, inc)
		}
		v.label = "Version " + inc
	}
	// The core orders an item's versions by created, so a version without a date can't be placed.
	date := cmp.Or(row["added"], row["created"])
	if date == "" {
		return v, fmt.Errorf("%w: no added or created date", errBadRow)
	}
	var err error
	if v.created, err = pgTime(date); err != nil {
		return v, err
	}
	if src.kind == protocol.KindLorebook {
		v.data, err = card.DecodeLorebook([]byte(row["definition"]), row["name"])
	} else {
		v.data, _, err = card.Decode([]byte(row["definition"]))
	}
	if err != nil {
		return v, fmt.Errorf("%w: %w", errBadRow, err)
	}
	if src.kind == protocol.KindCard {
		v.values = cardValues(src.site, row["hidden"] == "t", parents[v.item])
	}
	return v, nil
}

// cardValues are a card version's values for the manifest's [enrichments].
func cardValues(site string, hidden bool, p parent) map[string]json.RawMessage {
	values := map[string]json.RawMessage{"site": jsonValue(site)}
	if hidden || p.hidden {
		values["hidden"] = jsonValue("yes")
	}
	if p.rating != nil {
		values["rating"] = jsonValue(*p.rating)
	}
	if len(p.topics) > 0 {
		values["topics"] = jsonValue(p.topics)
	}
	return values
}

// jsonValue marshals a string, a float read from JSON, or a string slice,
// none of which can fail.
func jsonValue(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

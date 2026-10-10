package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"

	"github.com/davidgibbons/innkeeper-plugins/catalogkit"
	"github.com/davidgibbons/innkeeper/cardhash"
	"github.com/davidgibbons/innkeeper/protocol"
)

// checkpointEvery is how many rows ingest reads between checkpoints. Tests
// set it to 1.
var checkpointEvery = 1000

func invalid(err error) error {
	return protocol.NewError(protocol.CodeInvalidParams, err.Error(), false)
}

// ingest answers catalog.ingest: it reads the dump from the checkpoint to
// the end and stores each card and lorebook version.
// ponytail: one row at a time; decode images in parallel if a full ingest is too slow.
func ingest(ctx context.Context, e *env, notify func(string, any) error, raw json.RawMessage) (protocol.CatalogIngestResult, error) {
	var res protocol.CatalogIngestResult
	var p protocol.CatalogIngestParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return res, invalid(err)
	}
	var at checkpoint
	if len(p.Checkpoint) > 0 && string(p.Checkpoint) != "null" {
		if err := json.Unmarshal(p.Checkpoint, &at); err != nil {
			return res, invalid(fmt.Errorf("checkpoint: %w", err))
		}
	}
	d, err := openDump(e.dump, at)
	if err != nil {
		return res, invalid(fmt.Errorf("config dump: %w", err))
	}
	defer d.Close()
	report := func() error {
		if err := notify(protocol.NotifyProgress, protocol.Progress{Run: p.Run, Done: d.at.Offset, Total: d.size}); err != nil {
			return err
		}
		cp, _ := json.Marshal(d.at)
		return notify(protocol.NotifyCheckpoint, protocol.Checkpoint{Run: p.Run, Checkpoint: cp})
	}
	for n := 1; ; n++ {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		table, row, err := d.next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err == nil {
			err = ingestRow(ctx, e, table, row, &res)
		}
		if errors.Is(err, errBadRow) || errors.Is(err, catalogkit.ErrBadData) {
			res.Skipped++
			log.Printf("skipped %s %s increment %s: %v", table, row[idColumn(table)], row["increment"], err)
		} else if err != nil {
			return res, err
		}
		if n%checkpointEvery == 0 {
			if err := report(); err != nil {
				return res, err
			}
		}
	}
	return res, report()
}

// idColumn names the column holding a row's item ID.
func idColumn(table string) string {
	if src, ok := sources[table]; ok {
		return src.idCol
	}
	return "id"
}

// ingestRow stores one row: a chub per-item row for later versions to read,
// or a version.
func ingestRow(ctx context.Context, e *env, table string, row map[string]string, res *protocol.CatalogIngestResult) error {
	if _, ok := parents[table]; ok {
		return putParent(ctx, e.store, table, row)
	}
	en, err := entry(ctx, e.store, table, row)
	if err != nil {
		return err
	}
	version := strconv.Itoa(en.Version)
	pool, instance := e.store.Pool(), e.store.Instance()
	// ponytail: a version already ingested is skipped whole, so a newer dump's rating and tag changes don't reach it; drop this check to refresh them, at the cost of reading every image again.
	var seen bool
	err = pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM source_row WHERE instance = $1 AND item_id = $2 AND version = $3)`,
		instance, en.ID, version).Scan(&seen)
	if err != nil || seen {
		return err
	}
	hash := row["image_hash"]
	// A read error fails the run rather than store the version without its image for good.
	b, err := readImage(e.images, hash)
	if err != nil {
		return err
	}
	if b != nil {
		if en.Image, err = cardhash.DecodeImageBytes(b); err != nil {
			log.Printf("%s version %s: image %s: %v", en.ID, version, hash, err)
		}
	}
	change, err := e.store.Put(ctx, en)
	if err != nil {
		return err
	}
	switch change {
	case catalogkit.Added:
		res.Added++
	case catalogkit.Updated:
		res.Updated++
	}
	_, err = pool.Exec(ctx, `INSERT INTO source_row (instance, item_id, version, image_hash) VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING`, instance, en.ID, version, hash)
	return err
}

// env is what initialize set up.
type env struct {
	store   *catalogkit.Store // nil without a database_url
	dump    string            // config.dump
	images  []string          // config.images
	blobTmp string
}

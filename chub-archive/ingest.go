package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"io/fs"
	"log"
	"os"
	"strconv"
	"sync"
	"syscall"

	"github.com/davidgibbons/innkeeper-plugins/catalogkit"
	"github.com/davidgibbons/innkeeper-plugins/internal/pluginio"
	"github.com/davidgibbons/innkeeper/cardhash"
	"github.com/davidgibbons/innkeeper/protocol"
)

// checkpointEvery is how many rows ingest reads between checkpoints. Tests
// set it to 1.
var checkpointEvery = 1000

// imageReaders is how many versions' images ingest reads and decodes ahead
// of the row it is storing, since each read waits on the archive's share.
const imageReaders = 16

// pending is a row read from the dump, with its image once done is closed.
type pending struct {
	table  string
	row    map[string]string
	at     checkpoint // where the dump is after this row
	err    error      // from reading the row
	img    image.Image
	imgErr error // fails the run
	done   chan struct{}
}

func invalid(err error) error {
	return protocol.NewError(protocol.CodeInvalidParams, err.Error(), false)
}

// ingest answers catalog.ingest: it reads the dump from the checkpoint to
// the end and stores each card and lorebook version.
func ingest(ctx context.Context, e *env, notify func(string, any) error, raw json.RawMessage) (protocol.CatalogIngestResult, error) {
	var res protocol.CatalogIngestResult
	if e.dump == "" {
		return res, invalid(errors.New("set dump in this instance's config before ingesting"))
	}
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
	for _, dir := range e.images {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return res, invalid(fmt.Errorf("config images: %s is not a folder; is the archive mounted?", dir))
		}
	}
	d, err := openDump(e.dump, at)
	if errors.Is(err, errDumpChanged) {
		return res, invalid(err)
	}
	if err != nil {
		return res, invalid(fmt.Errorf("config dump: %w", err))
	}
	defer d.Close()
	report := func(at checkpoint) error {
		if err := notify(protocol.NotifyProgress, protocol.Progress{Run: p.Run, Done: at.Offset, Total: at.Size}); err != nil {
			return err
		}
		cp, _ := json.Marshal(at)
		return notify(protocol.NotifyCheckpoint, protocol.Checkpoint{Run: p.Run, Checkpoint: cp})
	}
	// One goroutine reads the dump and starts each version's image read, and
	// this one stores rows in dump order, so a checkpoint never passes a row
	// still in flight.
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer func() { cancel(); wg.Wait() }()
	rows := make(chan *pending, imageReaders)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(rows)
		for {
			table, row, err := d.next(ctx)
			if errors.Is(err, io.EOF) {
				return
			}
			r := &pending{table: table, row: row, at: d.at, err: err, done: make(chan struct{})}
			if _, parent := parents[table]; err == nil && !parent {
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer close(r.done)
					r.img, r.imgErr = loadImage(ctx, e, table, row)
				}()
			} else {
				close(r.done)
			}
			select {
			case rows <- r:
			case <-ctx.Done():
				return
			}
			if err != nil && !errors.Is(err, errBadRow) {
				return
			}
		}
	}()
	n := 0
	for r := range rows {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		<-r.done
		n++
		err := r.err
		if err == nil {
			err = ingestRow(ctx, e, r, &res)
		}
		var rpc *protocol.Error
		switch {
		case errors.Is(err, errBadRow) || errors.Is(err, catalogkit.ErrBadData):
			res.Skipped++
			log.Printf("skipped %s %s increment %s, ending at byte %d: %v", r.table, r.row[idColumn(r.table)], r.row["increment"], r.at.Offset, err)
		case err == nil:
		case ctx.Err() != nil || errors.As(err, &rpc):
			return res, err
		default:
			// The database or the archive's disk failed; the core retries from the last checkpoint.
			return res, protocol.NewError(protocol.CodeInternalError, err.Error(), true)
		}
		if n%checkpointEvery == 0 {
			if err := report(r.at); err != nil {
				return res, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return res, err
	}
	// The reader is done, so d.at is where the dump ended.
	return res, report(d.at)
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
func ingestRow(ctx context.Context, e *env, r *pending, res *protocol.CatalogIngestResult) error {
	if _, ok := parents[r.table]; ok {
		return putParent(ctx, e.store, r.table, r.row)
	}
	en, err := entry(ctx, e.store, r.table, r.row)
	if err != nil {
		return err
	}
	version := strconv.Itoa(en.Version)
	// Checked again here, in dump order, in case the dump repeats a version.
	if seen, err := stored(ctx, e.store, en.ID, version); err != nil || seen {
		return err
	}
	if r.imgErr != nil {
		return r.imgErr
	}
	en.Image = r.img
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
	_, err = e.store.Pool().Exec(ctx, `INSERT INTO source_row (instance, item_id, version, image_hash) VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING`, e.store.Instance(), en.ID, version, r.row["image_hash"])
	return err
}

// stored reports whether a version is already ingested.
// ponytail: a version already ingested is skipped whole, so a newer dump's rating and tag changes don't reach it; drop this check to refresh them, at the cost of reading every image again.
func stored(ctx context.Context, s *catalogkit.Store, id, version string) (bool, error) {
	var seen bool
	err := s.Pool().QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM source_row WHERE instance = $1 AND item_id = $2 AND version = $3)`,
		s.Instance(), id, version).Scan(&seen)
	return seen, err
}

// loadImage reads and decodes a version row's image, or returns nil for a
// version already stored. An image that can never be read is logged and left
// out; any other read error is returned, to fail the run for the core to retry.
func loadImage(ctx context.Context, e *env, table string, row map[string]string) (image.Image, error) {
	id, inc := table+":"+row[sources[table].idCol], row["increment"]
	if inc == "" {
		inc = "1"
	}
	v, err := strconv.Atoi(inc)
	if err != nil {
		return nil, nil
	}
	version := strconv.Itoa(v)
	if seen, err := stored(ctx, e.store, id, version); err != nil || seen {
		return nil, err
	}
	hash := row["image_hash"]
	b, err := readImage(e.images, hash)
	if err != nil && !errors.Is(err, fs.ErrPermission) && !errors.Is(err, syscall.EISDIR) && !errors.Is(err, pluginio.ErrTooLarge) {
		return nil, err
	}
	var img image.Image
	if err == nil && b != nil {
		img, err = cardhash.DecodeImageBytes(b)
	}
	if err != nil {
		log.Printf("%s version %s: image %s: %v", id, version, hash, err)
	}
	return img, nil
}

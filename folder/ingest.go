package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/davidgibbons/innkeeper-plugins/card"
	"github.com/davidgibbons/innkeeper-plugins/catalogkit"
	"github.com/davidgibbons/innkeeper-plugins/internal/pluginio"
	"github.com/davidgibbons/innkeeper/cardhash"
	"github.com/davidgibbons/innkeeper/protocol"
)

// checkpointEvery is how many files ingest reads between checkpoints.
const checkpointEvery = 100

type checkpoint struct {
	Last string `json:"last"`
}

func invalid(err error) error {
	return protocol.NewError(protocol.CodeInvalidParams, err.Error(), false)
}

// ingest answers catalog.ingest: it indexes every changed card and lorebook
// file, then marks items whose file is gone removed.
func ingest(ctx context.Context, e *env, notify func(string, any) error, raw json.RawMessage) (protocol.CatalogIngestResult, error) {
	var res protocol.CatalogIngestResult
	var p protocol.CatalogIngestParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return res, invalid(err)
	}
	var cp checkpoint
	if len(p.Checkpoint) > 0 && string(p.Checkpoint) != "null" {
		if err := json.Unmarshal(p.Checkpoint, &cp); err != nil {
			return res, invalid(fmt.Errorf("checkpoint: %w", err))
		}
	}
	root, err := os.OpenRoot(e.folder)
	if err != nil {
		return res, invalid(fmt.Errorf("config folder: %w", err))
	}
	defer root.Close()
	files, err := walk(root)
	if err != nil {
		return res, invalid(err)
	}
	// ponytail: if last is gone, the ingest starts over; unchanged files cost a stat each.
	start := slices.Index(files, cp.Last) + 1
	for i := start; i < len(files); i++ {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		change, skip, err := ingestFile(ctx, e.store, root, files[i])
		if err != nil {
			return res, err
		}
		switch {
		case skip != nil:
			res.Skipped++
			log.Printf("skipped %s: %v", files[i], skip)
		case change == catalogkit.Added:
			res.Added++
		case change == catalogkit.Updated:
			res.Updated++
		}
		if n := i + 1; n%checkpointEvery == 0 || n == len(files) {
			if err := notify(protocol.NotifyProgress, protocol.Progress{Run: p.Run, Done: int64(n), Total: int64(len(files))}); err != nil {
				return res, err
			}
			next, _ := json.Marshal(checkpoint{Last: files[i]})
			if err := notify(protocol.NotifyCheckpoint, protocol.Checkpoint{Run: p.Run, Checkpoint: next}); err != nil {
				return res, err
			}
		}
	}
	if res.Removed, err = e.store.MarkRemovedExcept(ctx, files); err != nil {
		return res, err
	}
	// A file that comes back unchanged must be read again to restore its item.
	_, err = e.store.Pool().Exec(ctx, `DELETE FROM file WHERE instance = $1 AND path <> ALL($2)`, e.store.Instance(), files)
	return res, err
}

// walk lists the card and lorebook files under root in lexical order,
// skipping hidden folders and logging unreadable ones.
func walk(root *os.Root) ([]string, error) {
	files := []string{}
	err := fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			if name == "." {
				return err
			}
			log.Printf("skipped %s: %v", name, err)
			return nil
		}
		if d.IsDir() && name != "." && strings.HasPrefix(d.Name(), ".") {
			return fs.SkipDir
		}
		ext := strings.ToLower(path.Ext(name))
		if d.Type().IsRegular() && (ext == ".png" || ext == ".json" || ext == ".charx") {
			files = append(files, name)
		}
		return nil
	})
	return files, err
}

// ingestFile indexes one file unless its mtime and size match the last
// ingest. skip is why a file that isn't a card or lorebook was passed over;
// err is a database error, which fails the ingest.
func ingestFile(ctx context.Context, store *catalogkit.Store, root *os.Root, name string) (change catalogkit.Change, skip, err error) {
	info, err := root.Stat(name)
	if err != nil {
		return 0, err, nil
	}
	mtime := modTime(info)
	var oldTime time.Time
	var oldSize int64
	err = store.Pool().QueryRow(ctx, `SELECT mtime, size FROM file WHERE instance = $1 AND path = $2`,
		store.Instance(), name).Scan(&oldTime, &oldSize)
	if err == nil && oldTime.Equal(mtime) && oldSize == info.Size() {
		return catalogkit.Unchanged, nil, nil
	}
	b, err := pluginio.ReadIn(root, name)
	if err != nil {
		return 0, err, nil
	}
	e := catalogkit.Entry{ID: name, Updated: mtime, Attrs: map[string][]string{"dir": dirs(name)}}
	stem := strings.TrimSuffix(path.Base(name), path.Ext(name))
	// Lorebooks first: one with a name and description would pass as a V1 card.
	if book, berr := card.DecodeLorebook(b, stem); berr == nil {
		e.Kind, e.Data = protocol.KindLorebook, book
	} else if c, img, cerr := card.Decode(b); cerr != nil {
		skip = cerr
	} else {
		e.Kind, e.Data = protocol.KindCard, c
		if img != nil {
			if e.Image, err = cardhash.DecodeImageBytes(img); err != nil {
				log.Printf("%s: avatar: %v", name, err)
			}
		}
	}
	if skip == nil {
		change, err = store.Put(ctx, e)
		if errors.Is(err, catalogkit.ErrBadData) {
			skip = err
		} else if err != nil {
			return 0, nil, err
		}
	}
	_, err = store.Pool().Exec(ctx, `INSERT INTO file (instance, path, mtime, size) VALUES ($1, $2, $3, $4)
		ON CONFLICT (instance, path) DO UPDATE SET mtime = excluded.mtime, size = excluded.size`,
		store.Instance(), name, mtime, info.Size())
	return change, skip, err
}

// modTime is info's mtime as Postgres stores it, to the microsecond.
func modTime(info fs.FileInfo) time.Time {
	return info.ModTime().UTC().Truncate(time.Microsecond)
}

// dirs lists the folders above name, outermost first: a/b/c.png gives a and a/b.
func dirs(name string) []string {
	out := []string{}
	for d := path.Dir(name); d != "."; d = path.Dir(d) {
		out = append([]string{d}, out...)
	}
	return out
}

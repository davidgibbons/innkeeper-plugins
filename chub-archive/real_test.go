package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/inkandale/innkeeper-plugins/catalogkit"
	"github.com/inkandale/innkeeper/protocol"
)

// TestRealDump ingests a real dump: CHUB_DUMP is its path, CHUB_IMAGES the
// image folders separated by colons, CHUB_IMAGE_STORE the image store folder,
// and CHUB_DATABASE_URL the database.
func TestRealDump(t *testing.T) {
	dump := os.Getenv("CHUB_DUMP")
	if dump == "" {
		t.Skip("CHUB_DUMP is not set")
	}
	ctx := context.Background()
	store, err := catalogkit.Open(ctx, os.Getenv("CHUB_DATABASE_URL"), "real", catalogConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e := &env{store: store, dump: dump, images: strings.Split(os.Getenv("CHUB_IMAGES"), ":"),
		imageStore: os.Getenv("CHUB_IMAGE_STORE"), blobTmp: t.TempDir()}
	n := 0
	var cp json.RawMessage
	notify := func(method string, params any) error {
		switch p := params.(type) {
		case protocol.Progress:
			if n++; n%20 == 0 {
				log.Printf("progress %d / %d bytes", p.Done, p.Total)
			}
		case protocol.Checkpoint:
			cp = p.Checkpoint
		}
		return nil
	}
	// Retry a retryable error from the last checkpoint, as the core does.
	for try := 1; ; try++ {
		params, _ := json.Marshal(protocol.CatalogIngestParams{Run: "real", Checkpoint: cp})
		res, err := ingest(ctx, e, notify, params)
		log.Printf("result: %+v", res)
		var rpc *protocol.Error
		if errors.As(err, &rpc) && rpc.Retryable() && try < 10 {
			log.Printf("retrying from checkpoint: %v", err)
			time.Sleep(time.Minute)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		return
	}
}

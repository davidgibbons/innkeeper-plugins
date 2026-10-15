package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/davidgibbons/innkeeper/protocol"
)

// qualityKey holds the evaluation's overall score; each dimension's score
// goes under the dimension's key.
const qualityKey = "quality"

// evaluatePrompt names the evaluate prompt's version in each value's meta. It
// hashes the template so it changes whenever the prompt does.
var evaluatePrompt = func() string {
	b, err := promptFiles.ReadFile("prompts/evaluate.tmpl")
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(b)
	return "evaluate-" + hex.EncodeToString(sum[:4])
}()

// enrich scores each revision with the evaluate action. One revision's
// failure is its own error; the rest of the batch goes on.
func (srv *server) enrich(ctx context.Context, in *instance, params json.RawMessage) (*protocol.EnrichRunResult, error) {
	var p protocol.EnrichRunParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, invalid("%v", err)
	}
	if len(p.Revisions) == 0 || len(p.Revisions) > protocol.MaxEnrichBatch {
		return nil, invalid("revisions must hold 1 to %d", protocol.MaxEnrichBatch)
	}
	if in.err != nil {
		return nil, in.err
	}
	meta, err := json.Marshal(map[string]string{"model": in.settings.Model, "prompt": evaluatePrompt})
	if err != nil {
		return nil, err
	}
	res := &protocol.EnrichRunResult{Results: []protocol.EnrichResult{}}
	// ponytail: one revision at a time; run a few concurrently if batches are slow.
	for i, rev := range p.Revisions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		_ = srv.conn.Notify(protocol.NotifyProgress, protocol.Progress{Run: p.Run, Done: int64(i), Total: int64(len(p.Revisions)),
			Message: fmt.Sprintf("Evaluating %s (%d of %d)", revisionName(rev), i+1, len(p.Revisions))})
		values, err := srv.scores(ctx, in, p.Run, rev)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		out := protocol.EnrichResult{RevisionID: rev.RevisionID, Values: values, Meta: meta}
		if err != nil {
			out = protocol.EnrichResult{RevisionID: rev.RevisionID, Error: err.Error()}
		}
		res.Results = append(res.Results, out)
	}
	return res, nil
}

// scores runs evaluate on one revision and returns its scores by enrichment
// key, each clamped to 0 to 100.
func (srv *server) scores(ctx context.Context, in *instance, run string, rev protocol.EnrichRevision) (map[string]json.RawMessage, error) {
	r, err := parseRun(protocol.ActionRunParams{Run: run, Action: "evaluate",
		Entity: protocol.ActionEntity{Kind: "card", Data: rev.Data}})
	if err != nil {
		return nil, err
	}
	res, err := srv.execute(ctx, in, r, func() func(string) { return func(string) {} })
	if err != nil {
		return nil, err
	}
	values := map[string]json.RawMessage{}
	add := func(key string, score float64) {
		values[key], _ = json.Marshal(min(max(score, 0), 100))
	}
	if res.Report.Score != nil {
		add(qualityKey, *res.Report.Score)
	}
	for _, d := range res.Report.Dimensions {
		add(d.Key, d.Score)
	}
	if len(values) == 0 {
		return nil, errors.New("the model's evaluation has no scores")
	}
	return values, nil
}

// revisionName is the card's name for progress messages.
func revisionName(rev protocol.EnrichRevision) string {
	var card struct{ Name string }
	if json.Unmarshal(rev.Data, &card) == nil && card.Name != "" {
		return card.Name
	}
	return fmt.Sprintf("revision %d", rev.RevisionID)
}

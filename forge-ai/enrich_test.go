package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/davidgibbons/innkeeper/protocol"
)

const evaluated = `{"summary": "Solid.", "overall_score": 82, "card_type": "character", "dimensions": {
"consistency": {"score": 90, "comment": "ok"}, "voice": {"score": 120, "comment": "ok"},
"first_impression": {"score": -5, "comment": "ok"}, "roleplayability": {"score": 70, "comment": "ok"},
"token_efficiency": {"score": 60.5, "comment": "ok"}, "field_governance": {"score": 50, "comment": "ok"}},
"contradictions": [], "misplaced_content": [], "suggestions": []}`

func enrichParams(names ...string) protocol.EnrichRunParams {
	p := protocol.EnrichRunParams{Run: "R1"}
	for i, name := range names {
		data, _ := json.Marshal(map[string]string{"name": name, "description": "A towering orc."})
		p.Revisions = append(p.Revisions, protocol.EnrichRevision{CardID: int64(i + 1), RevisionID: int64(100 + i), Data: data})
	}
	return p
}

func (h *host) enrich(t *testing.T, p protocol.EnrichRunParams) (protocol.EnrichRunResult, error) {
	t.Helper()
	var res protocol.EnrichRunResult
	err := h.conn.Call(context.Background(), protocol.MethodEnrichRun, p, &res)
	return res, err
}

func TestEnrichMapsEvaluationToDeclaredKeys(t *testing.T) {
	_, cfg := withLLM(t, streamed(evaluated))
	h := startPlugin(t, cfg, nil)
	res, err := h.enrich(t, enrichParams("Brakka"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 1 || res.Results[0].RevisionID != 100 || res.Results[0].Error != "" {
		t.Fatalf("results %+v", res.Results)
	}
	got := map[string]float64{}
	for k, v := range res.Results[0].Values {
		var n float64
		if err := json.Unmarshal(v, &n); err != nil {
			t.Fatalf("%s = %s: %v", k, v, err)
		}
		got[k] = n
	}
	want := map[string]float64{"quality": 82, "consistency": 90, "voice": 100, "first_impression": 0,
		"roleplayability": 70, "token_efficiency": 60.5, "field_governance": 50}
	if !maps.Equal(got, want) {
		t.Fatalf("values %v, want %v (out-of-range scores clamped)", got, want)
	}
	m, err := protocol.LoadManifest(".")
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range res.Results[0].Values {
		if err := m.Enrichments[k].Check(v); err != nil {
			t.Errorf("%s: %v", k, err)
		}
	}
	var meta map[string]string
	if err := json.Unmarshal(res.Results[0].Meta, &meta); err != nil || meta["model"] != "m" || !strings.HasPrefix(meta["prompt"], "evaluate-") {
		t.Fatalf("meta %s: %v", res.Results[0].Meta, err)
	}
	if len(h.progress) != 1 || h.progress[0].Run != "R1" || h.progress[0].Message != "Evaluating Brakka (1 of 1)" || h.progress[0].Total != 1 {
		t.Fatalf("progress %+v", h.progress)
	}
}

func TestEnrichFailedRevisionKeepsTheBatch(t *testing.T) {
	llm, cfg := withLLM(t, fakeReply{status: 400}, streamed("I can't score this."), streamed(evaluated))
	h := startPlugin(t, cfg, nil)
	res, err := h.enrich(t, enrichParams("Brakka", "Mira", "Ash"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 3 || len(llm.requests) != 3 {
		t.Fatalf("results %+v after %d requests", res.Results, len(llm.requests))
	}
	for i, r := range res.Results[:2] {
		if r.Error == "" || r.Values != nil || r.Meta != nil {
			t.Errorf("revision %d: %+v, want only an error", r.RevisionID, res.Results[i])
		}
	}
	if r := res.Results[2]; r.RevisionID != 102 || r.Error != "" || len(r.Values) != 7 {
		t.Fatalf("third revision %+v", r)
	}
	var messages []string
	for _, p := range h.progress {
		messages = append(messages, p.Message)
	}
	if !slices.Equal(messages, []string{"Evaluating Brakka (1 of 3)", "Evaluating Mira (2 of 3)", "Evaluating Ash (3 of 3)"}) {
		t.Fatalf("progress %q", messages)
	}
}

func TestEnrichWithoutSettingsFailsTheCall(t *testing.T) {
	h := startPlugin(t, map[string]any{}, nil)
	_, err := h.enrich(t, enrichParams("Brakka"))
	var rpc *protocol.Error
	if !errors.As(err, &rpc) || rpc.Code == -32601 || !strings.Contains(rpc.Message, "provider") {
		t.Fatalf("err = %v, want a settings error", err)
	}
}

func TestEnrichCancelStopsFurtherCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &fakeLLM{replies: []fakeReply{streamed(evaluated), streamed(evaluated)}}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel() // The owner pressed Stop during the first call.
		f.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	s, err := parseSettings(map[string]any{"provider": "openai-compatible", "base_url": ts.URL, "model": "m"})
	if err != nil {
		t.Fatal(err)
	}
	in := &instance{settings: s}
	if in.llm, err = s.provider(""); err != nil {
		t.Fatal(err)
	}
	srv := &server{conn: protocol.NewConn(strings.NewReader(""), io.Discard, nil)}
	params, _ := json.Marshal(enrichParams("Brakka", "Mira"))
	if _, err := srv.enrich(ctx, in, params); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) > 1 {
		t.Fatalf("%d requests after cancel, want at most 1", len(f.requests))
	}
}

// The manifest declares quality and one key per evaluate dimension, labeled
// as the report labels it, all scored 0 to 100 as the clamp assumes.
func TestEnrichmentsMatchDimensions(t *testing.T) {
	m, err := protocol.LoadManifest(".")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{qualityKey: "Quality"}
	for _, d := range dimensions {
		want[d.key] = d.label
	}
	got := map[string]string{}
	for k, e := range m.Enrichments {
		got[k] = e.Label
		if e.Type != "score" || !slices.Equal(e.Range, []float64{0, 100}) || e.Grid != (k == qualityKey) {
			t.Errorf("enrichment %s = %+v", k, e)
		}
	}
	if !maps.Equal(got, want) {
		t.Fatalf("enrichments %v, want %v", got, want)
	}
}

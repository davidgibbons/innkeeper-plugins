package main

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/davidgibbons/innkeeper/protocol"
)

func TestActionsValidate(t *testing.T) {
	seen := map[string]bool{}
	for _, a := range actions {
		if err := a.Validate(); err != nil {
			t.Error(err)
		}
		if seen[a.ID] {
			t.Errorf("action %s repeats", a.ID)
		}
		seen[a.ID] = true
	}
}

func result(t *testing.T, p protocol.ActionRunParams, output string) protocol.ActionRunResult {
	t.Helper()
	in, err := parseRun(p)
	if err != nil {
		t.Fatal(err)
	}
	res, err := in.result(output)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func proposalOf(t *testing.T, res protocol.ActionRunResult) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(res.Proposal, &m); err != nil {
		t.Fatalf("proposal %s: %v", res.Proposal, err)
	}
	return m
}

func TestFieldActionProposalChangesOnlyItsField(t *testing.T) {
	res := result(t, sampleRun(t, "tighten"), `{"description": "A towering orc.", "name": "Grok"}`)
	m := proposalOf(t, res)
	if m["description"] != "A towering orc." || m["name"] != "Brakka" || m["extensions"] == nil {
		t.Fatalf("proposal %v", m)
	}
	if res.Report != nil {
		t.Fatal("field action made a report")
	}
}

func TestWholeCardProposalKeepsOtherKeys(t *testing.T) {
	// The model echoes empty fields the draft never had; they stay absent.
	res := result(t, sampleRun(t, "revise"), `{"name": "Brakka", "description": "Billy's brother.", "scenario": "", "tags": ["orc", "brother"]}`)
	m := proposalOf(t, res)
	if m["description"] != "Billy's brother." || m["personality"] != "Gruff." || m["extensions"] == nil {
		t.Fatalf("proposal %v", m)
	}
	if _, ok := m["scenario"]; ok {
		t.Fatalf("proposal gained an empty scenario: %v", m)
	}
	if tags, _ := m["tags"].([]any); len(tags) != 2 {
		t.Fatalf("tags = %v", m["tags"])
	}
}

func TestGenerateProposalIsTheNewCard(t *testing.T) {
	m := proposalOf(t, result(t, sampleRun(t, "generate"), `{"name": "Ash", "description": "A courier.", "tags": ["sci-fi"]}`))
	if m["name"] != "Ash" || len(m) != 3 {
		t.Fatalf("proposal %v", m)
	}
}

func TestProposalRefusesWrongTypes(t *testing.T) {
	in, err := parseRun(sampleRun(t, "revise"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.result(`{"description": ["not", "text"]}`); err == nil {
		t.Fatal("a list description was accepted")
	}
}

func TestEvaluateReport(t *testing.T) {
	out := `{"summary": "Solid **orc**.", "overall_score": 78, "card_type": "character",
		"dimensions": {"voice": {"score": 88, "comment": "Distinct."}, "consistency": {"score": 70, "comment": "Mostly."}},
		"contradictions": [{"fields": ["personality", "first_mes"], "issue": "Gruff, yet chatty."}],
		"misplaced_content": [{"excerpt": "Always reply in 3 lines", "current_field": "description", "suggested_field": "post_history_instructions", "reason": "Per-turn rule."}],
		"suggestions": ["Add a system prompt."]}`
	res := result(t, sampleRun(t, "evaluate"), out)
	r := res.Report
	if r == nil || res.Proposal != nil {
		t.Fatalf("result %+v", res)
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.Summary != "Solid **orc**." || r.Score == nil || *r.Score != 78 {
		t.Fatalf("summary %q, score %v", r.Summary, r.Score)
	}
	// Dimensions keep the prompt's order, not the model's.
	if len(r.Dimensions) != 2 || r.Dimensions[0].Key != "consistency" || r.Dimensions[1].Label != "Voice" {
		t.Fatalf("dimensions %+v", r.Dimensions)
	}
	if len(r.Findings) != 2 || r.Findings[0].Severity != "high" || r.Findings[1].Move == nil ||
		r.Findings[1].Move.To != "post_history_instructions" || r.Findings[1].Fields[0] != "description" {
		t.Fatalf("findings %+v", r.Findings)
	}
	if len(r.Suggestions) != 1 {
		t.Fatalf("suggestions %v", r.Suggestions)
	}
}

func TestRunInputsChecked(t *testing.T) {
	bad := map[string]func(*protocol.ActionRunParams){
		"unknown action":    func(p *protocol.ActionRunParams) { p.Action = "nope" },
		"field not offered": func(p *protocol.ActionRunParams) { p.Field = "name" },
		"no instruction":    func(p *protocol.ActionRunParams) { p.Instruction = " " },
		"no point of view": func(p *protocol.ActionRunParams) {
			p.Action, p.Field, p.Inputs = "change-pov", "", json.RawMessage(`{}`)
		},
		"scenario is not a pov": func(p *protocol.ActionRunParams) {
			p.Action, p.Field, p.Inputs = "change-pov", "", json.RawMessage(`{"point_of_view": "scenario"}`)
		},
		"examples on a new card": func(p *protocol.ActionRunParams) { p.Action, p.Field, p.Entity.Data = "examples", "mes_example", nil },
	}
	for name, change := range bad {
		p := sampleRun(t, "rewrite")
		change(&p)
		_, err := parseRun(p)
		var rpc *protocol.Error
		if !errors.As(err, &rpc) || rpc.Code != -32602 {
			t.Errorf("%s: err = %v, want -32602", name, err)
		}
	}
}

func TestNullMeansUnchanged(t *testing.T) {
	m := proposalOf(t, result(t, sampleRun(t, "revise"), `{"description": null, "personality": "Calm.", "scenario": null}`))
	if m["description"] != "A very big, very tall orc who guards {{user}}." || m["personality"] != "Calm." {
		t.Fatalf("proposal %v", m)
	}
	if _, ok := m["scenario"]; ok {
		t.Fatalf("null scenario added a key: %v", m)
	}
}

func TestTagsAsCommaSeparatedText(t *testing.T) {
	m := proposalOf(t, result(t, sampleRun(t, "revise"), `{"tags": "orc, guard ,, brother"}`))
	tags, _ := m["tags"].([]any)
	if len(tags) != 3 || tags[1] != "guard" || tags[2] != "brother" {
		t.Fatalf("tags = %v", m["tags"])
	}
}

func TestAlternateGreetingsAppendToExisting(t *testing.T) {
	p := sampleRun(t, "alternate-greetings")
	p.Entity.Data = json.RawMessage(`{"name": "Brakka", "first_mes": "Hi.", "alternate_greetings": ["Old."], "extensions": {"x": 1}}`)
	m := proposalOf(t, result(t, p, `{"alternate_greetings": ["*Rain.* Who goes there?", " ", "Old.", "*Dawn.*"]}`))
	got, _ := json.Marshal(m["alternate_greetings"])
	if string(got) != `["Old.","*Rain.* Who goes there?","*Dawn.*"]` || m["first_mes"] != "Hi." || m["extensions"] == nil {
		t.Fatalf("proposal %v", m)
	}
}

func TestAlternateGreetingsOnACardWithoutAny(t *testing.T) {
	m := proposalOf(t, result(t, sampleRun(t, "alternate-greetings"), `{"alternate_greetings": ["*Dawn.*"]}`))
	if got, _ := json.Marshal(m["alternate_greetings"]); string(got) != `["*Dawn.*"]` || m["name"] != "Brakka" {
		t.Fatalf("proposal %v", m)
	}
}

func TestLoreEntriesKeepExistingAndAppendNew(t *testing.T) {
	out := `{"entries": [
		{"comment": "The pass", "keys": ["BRAKKA", "Iron Pass", "iron pass"], "content": "A narrow road through the peaks."},
		{"comment": "Repeat", "keys": ["the orc"], "content": "Only taken keys."},
		{"comment": "Empty", "keys": ["Snow"], "content": " "},
		{"keys": ["Billy"], "content": "Brakka's brother."}]}`
	m := proposalOf(t, result(t, sampleRun(t, "lore-entries"), out))
	if m["name"] != "Northern Reach" || m["description"] != "The cold north." {
		t.Fatalf("proposal %v", m)
	}
	entries, _ := m["entries"].([]any)
	if len(entries) != 3 {
		t.Fatalf("entries %v", entries)
	}
	var before struct{ Entries []any }
	_ = json.Unmarshal([]byte(sampleLorebook), &before)
	if !reflect.DeepEqual(entries[0], before.Entries[0]) {
		t.Fatalf("existing entry changed: %v", entries[0])
	}
	added, _ := json.Marshal(entries[1:])
	if string(added) != `[{"comment":"The pass","content":"A narrow road through the peaks.","enabled":true,"extensions":{},`+
		`"insertion_order":100,"keys":["Iron Pass"],"position":"before_char","use_regex":false},`+
		`{"content":"Brakka's brother.","enabled":true,"extensions":{},"insertion_order":100,"keys":["Billy"],"position":"before_char","use_regex":false}]` {
		t.Fatalf("added entries %s", added)
	}
}

func TestCountDefaultsAndLimits(t *testing.T) {
	cases := []struct {
		action, inputs string
		want           int
	}{
		{"alternate-greetings", `{}`, 3},
		{"alternate-greetings", `{"count": 50}`, 10},
		{"lore-entries", `{}`, 5},
		{"lore-entries", `{"count": 50}`, 20},
		{"lore-entries", `{"count": -2}`, 1},
	}
	for _, c := range cases {
		p := sampleRun(t, c.action)
		p.Inputs = json.RawMessage(c.inputs)
		r, err := parseRun(p)
		if err != nil {
			t.Fatal(err)
		}
		if r.count != c.want {
			t.Errorf("%s %s: count %d, want %d", c.action, c.inputs, r.count, c.want)
		}
	}
}

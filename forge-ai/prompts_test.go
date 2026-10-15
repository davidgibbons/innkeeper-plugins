package main

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/davidgibbons/innkeeper/protocol"
)

const sampleCard = `{"name": "Brakka", "description": "A very big, very tall orc who guards {{user}}.", "personality": "Gruff.",
	"first_mes": "*Brakka grunts.*", "mes_example": "<START>\n{{char}}: Hm.", "tags": ["orc"], "extensions": {"x": 1}}`

// sampleLorebook has one entry, with an id the proposal must keep.
const sampleLorebook = `{"name": "Northern Reach", "description": "The cold north.", "entries": [{"id": 1, "keys": ["Brakka", "the orc"],
	"content": "Brakka guards the pass & the gate.", "enabled": true, "insertion_order": 50, "use_regex": false, "extensions": {"depth": 4}}]}`

// sampleRun is a valid run of the action id on sampleCard, or for a lorebook
// action on sampleLorebook with sampleCard attached.
func sampleRun(t *testing.T, id string) protocol.ActionRunParams {
	t.Helper()
	a, ok := findAction(id)
	if !ok {
		t.Fatalf("no action %s", id)
	}
	p := protocol.ActionRunParams{Run: "1.1", Action: id, Entity: protocol.ActionEntity{Kind: "card", Data: json.RawMessage(sampleCard)},
		Inputs: json.RawMessage(`{}`)}
	switch a.Applies {
	case protocol.ApplyNew:
		p.Entity.Data = nil
	case protocol.ApplyLorebook:
		p.Entity = protocol.ActionEntity{Kind: "lorebook", Data: json.RawMessage(sampleLorebook)}
		p.Context.Cards = []protocol.ContextItem{{ID: 7, Name: "Brakka", Data: json.RawMessage(sampleCard)}}
	}
	if len(a.Fields) > 0 {
		p.Field = a.Fields[0]
	}
	if a.Instruction {
		p.Instruction = "make him the brother of @cards/Billy"
	}
	if id == "change-pov" {
		p.Inputs = json.RawMessage(`{"point_of_view": "first"}`)
	}
	return p
}

func render(t *testing.T, p protocol.ActionRunParams, s settings) (system, user string, keys []string) {
	t.Helper()
	in, err := parseRun(p)
	if err != nil {
		t.Fatalf("%s: %v", p.Action, err)
	}
	system, user, err = in.prompts(s)
	if err != nil {
		t.Fatalf("%s: %v", p.Action, err)
	}
	return system, user, in.writes()
}

func TestEveryActionRenders(t *testing.T) {
	for _, a := range actions {
		system, user, keys := render(t, sampleRun(t, a.ID), settings{})
		if strings.TrimSpace(system) == "" || strings.TrimSpace(user) == "" {
			t.Errorf("%s: empty prompt", a.ID)
		}
		if strings.Contains(system+user, "[[") || strings.Contains(system+user, "<no value>") {
			t.Errorf("%s: unrendered template text:\n%s\n%s", a.ID, system, user)
		}
		if a.Applies == protocol.ApplyNew && !strings.Contains(system, "{{char}}") {
			t.Errorf("%s: system prompt lost the roleplay macros", a.ID)
		}
		if a.Applies != protocol.ApplyNew && !strings.Contains(user, "guards {{user}}") {
			t.Errorf("%s: user prompt lost the card's macros", a.ID)
		}
		if a.Applies != protocol.ApplyNew && !strings.Contains(user, "<START>") {
			t.Errorf("%s: user prompt escaped the card's <START>", a.ID)
		}
		if len(keys) == 0 {
			t.Errorf("%s: writes no keys", a.ID)
		}
		for _, k := range keys {
			if !strings.Contains(system, `"`+k+`"`) {
				t.Errorf("%s: system prompt never names key %q", a.ID, k)
			}
		}
		if a.Applies != protocol.ApplyNew && !strings.Contains(user, "very tall orc") {
			t.Errorf("%s: user prompt lacks the card", a.ID)
		}
	}
}

func TestPromptsCarryRunInputs(t *testing.T) {
	_, user, _ := render(t, sampleRun(t, "revise"), settings{})
	if !strings.Contains(user, "@cards/Billy") {
		t.Errorf("revise: instruction missing from %q", user)
	}
	_, user, keys := render(t, sampleRun(t, "tighten"), settings{})
	if !strings.Contains(user, `"description"`) || !slices.Equal(keys, []string{"description"}) {
		t.Errorf("tighten: field missing; keys %v, user %q", keys, user)
	}
	p := sampleRun(t, "examples")
	p.Inputs = json.RawMessage(`{"count": 5}`)
	if system, _, _ := render(t, p, settings{}); !strings.Contains(system, "Write 5 example") {
		t.Errorf("examples: count missing from %q", system)
	}
	p = sampleRun(t, "alternate-greetings")
	p.Entity.Data = json.RawMessage(`{"name": "Brakka", "alternate_greetings": ["*The gate creaks.*"]}`)
	p.Inputs = json.RawMessage(`{"count": 4}`)
	if system, user, _ := render(t, p, settings{}); !strings.Contains(system, "Write 4 new greetings") || !strings.Contains(user, "The gate creaks.") {
		t.Errorf("alternate-greetings: count or existing greetings missing:\n%s\n%s", system, user)
	}
	p = sampleRun(t, "lore-entries")
	p.Instruction = "the guards of the pass"
	system, user, _ := render(t, p, settings{})
	for _, want := range []string{"Write 5 new entries", "the guards of the pass", "Northern Reach", "Brakka guards the pass & the gate.", "Card: Brakka"} {
		if !strings.Contains(system+user, want) {
			t.Errorf("lore-entries: prompt lacks %q:\n%s\n%s", want, system, user)
		}
	}
	if strings.Contains(user, "insertion_order") {
		t.Errorf("lore-entries: the model sees entry settings:\n%s", user)
	}
	for pov, want := range map[string]string{"first": "first person", "third": "third person", "scenario": "scenario card"} {
		p := sampleRun(t, "generate")
		p.Inputs = json.RawMessage(`{"point_of_view": "` + pov + `"}`)
		if system, _, _ := render(t, p, settings{}); !strings.Contains(system, want) {
			t.Errorf("generate %s: system prompt lacks %q", pov, want)
		}
	}
}

func TestHouseStyleAndAdditionInSystemPrompt(t *testing.T) {
	s := settings{HouseStyle: "Use British spelling.", Additions: map[string]string{"evaluate": "Be harsh about clichés."}}
	for _, a := range actions {
		system, user, _ := render(t, sampleRun(t, a.ID), s)
		if !strings.Contains(system, "Use British spelling.") {
			t.Errorf("%s: no house style", a.ID)
		}
		if got := strings.Contains(system, "Be harsh about clichés."); got != (a.ID == "evaluate") {
			t.Errorf("%s: addition in system prompt = %v", a.ID, got)
		}
		if strings.Contains(user, "British") {
			t.Errorf("%s: house style leaked into the user prompt", a.ID)
		}
	}
}

func TestContextInUserPrompt(t *testing.T) {
	p := sampleRun(t, "revise")
	p.Context = protocol.ActionContext{
		Cards:     []protocol.ContextItem{{ID: 7, Name: "Billy", Data: json.RawMessage(`{"name": "Billy", "description": "A small goblin."}`)}},
		Lorebooks: []protocol.ContextItem{{ID: 3, Name: "Northern Reach", Data: json.RawMessage(`{"entries": []}`)}},
	}
	_, user, _ := render(t, p, settings{})
	for _, want := range []string{"Card: Billy", "A small goblin.", "Lorebook: Northern Reach"} {
		if !strings.Contains(user, want) {
			t.Errorf("user prompt lacks %q:\n%s", want, user)
		}
	}
	if _, user, _ := render(t, sampleRun(t, "revise"), settings{}); strings.Contains(user, "Attached by the owner") {
		t.Errorf("empty context still rendered:\n%s", user)
	}
}

func TestAdditionsMatchActions(t *testing.T) {
	var m struct {
		Config struct {
			Properties struct {
				Additions struct {
					Properties map[string]any `toml:"properties"`
				} `toml:"additions"`
			} `toml:"properties"`
		} `toml:"config"`
	}
	if _, err := toml.DecodeFile("plugin.toml", &m); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, a := range actions {
		ids = append(ids, a.ID)
	}
	slices.Sort(ids)
	if got := slices.Sorted(maps.Keys(m.Config.Properties.Additions.Properties)); !slices.Equal(got, ids) {
		t.Fatalf("additions properties %v, want the action IDs %v", got, ids)
	}
}

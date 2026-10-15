package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davidgibbons/innkeeper-plugins/internal/pgtest"
	"github.com/davidgibbons/innkeeper/protocol"
	"github.com/jackc/pgx/v5"
)

// startWithDB starts the plugin on a fresh schema as instance.
func startWithDB(t *testing.T, dsn, instance string, config map[string]any) *host {
	t.Helper()
	h := startWith(t, protocol.InitializeParams{Protocol: 1, InstanceID: instance, Config: config, DatabaseURL: dsn})
	t.Cleanup(func() { h.conn.Call(context.Background(), protocol.MethodShutdown, nil, nil) })
	return h
}

func (h *host) call(method string, params, res any) error {
	return h.conn.Call(context.Background(), method, params, res)
}

func (h *host) put(t *testing.T, id string, sk skill) string {
	t.Helper()
	var res protocol.RecordsPutResult
	if err := h.call(protocol.MethodRecordsPut, map[string]any{"type": "skill", "id": id, "record": sk}, &res); err != nil {
		t.Fatal(err)
	}
	return res.ID
}

func (h *host) list(t *testing.T) []protocol.RecordSummary {
	t.Helper()
	var res protocol.RecordsListResult
	if err := h.call(protocol.MethodRecordsList, protocol.RecordsListParams{Type: "skill"}, &res); err != nil {
		t.Fatal(err)
	}
	return res.Records
}

func wantCode(t *testing.T, err error, code int) {
	t.Helper()
	var rpc *protocol.Error
	if !errors.As(err, &rpc) || rpc.Code != code {
		t.Fatalf("err = %v, want code %d", err, code)
	}
}

var college = skill{Name: "college", Description: "Campus life.", Body: "# College", Files: map[string]string{"references/campus.md": "The quad."}, Enabled: true}

func TestRecordsRoundTrip(t *testing.T) {
	dsn := pgtest.URL(t)
	h := startWithDB(t, dsn, "i1", map[string]any{})
	if got := h.list(t); len(got) != 0 {
		t.Fatalf("new store lists %v", got)
	}
	if id := h.put(t, "", college); id != "college" {
		t.Fatalf("id %q", id)
	}
	var got protocol.RecordsGetResult
	if err := h.call(protocol.MethodRecordsGet, protocol.RecordRef{Type: "skill", ID: "college"}, &got); err != nil {
		t.Fatal(err)
	}
	var back skill
	if err := json.Unmarshal(got.Record, &back); err != nil || back.Body != "# College" || back.Files["references/campus.md"] != "The quad." {
		t.Fatalf("record %s: %v", got.Record, err)
	}
	// Creating a second skill with the same name never overwrites the first.
	err := h.call(protocol.MethodRecordsPut, map[string]any{"type": "skill", "record": skill{Name: "college", Description: "x", Body: "x"}}, nil)
	wantCode(t, err, -32602)

	renamed := back
	renamed.Name = "university"
	if id := h.put(t, "college", renamed); id != "university" {
		t.Fatalf("rename id %q", id)
	}
	if l := h.list(t); len(l) != 1 || l[0] != (protocol.RecordSummary{ID: "university", Name: "university", Description: "Campus life."}) {
		t.Fatalf("list %+v", l)
	}
	h.put(t, "", college)
	err = h.call(protocol.MethodRecordsPut, map[string]any{"type": "skill", "id": "college", "record": renamed}, nil)
	wantCode(t, err, -32602) // renaming onto another skill
	err = h.call(protocol.MethodRecordsPut, map[string]any{"type": "skill", "id": "gone", "record": renamed}, nil)
	wantCode(t, err, -32004)
	err = h.call(protocol.MethodRecordsPut, map[string]any{"type": "skill", "record": skill{Name: "Bad Name", Description: "x", Body: "x"}}, nil)
	wantCode(t, err, -32602)
	err = h.call(protocol.MethodRecordsPut, map[string]any{"type": "skill", "record": skill{Name: "nul", Description: "x", Body: "a\x00b"}}, nil)
	wantInvalid(t, err, "without NUL")
	err = h.call(protocol.MethodRecordsPut, map[string]any{"type": "skill", "record": skill{Name: "nul", Description: "x\x00", Body: "b"}}, nil)
	wantInvalid(t, err, "description")

	if err := h.call(protocol.MethodRecordsDelete, protocol.RecordRef{Type: "skill", ID: "college"}, nil); err != nil {
		t.Fatal(err)
	}
	wantCode(t, h.call(protocol.MethodRecordsDelete, protocol.RecordRef{Type: "skill", ID: "college"}, nil), -32004)
	wantCode(t, h.call(protocol.MethodRecordsGet, protocol.RecordRef{Type: "skill", ID: "college"}, nil), -32004)
	wantCode(t, h.call(protocol.MethodRecordsList, protocol.RecordsListParams{Type: "skills"}, nil), -32602)

	// Instances share the schema but not their skills.
	other := startWithDB(t, dsn, "i2", map[string]any{})
	if l := other.list(t); len(l) != 0 {
		t.Fatalf("instance i2 sees %v", l)
	}
}

func TestRecordsImportUpload(t *testing.T) {
	h := startWithDB(t, pgtest.URL(t), "i1", map[string]any{})
	h.put(t, "", skill{Name: "college", Description: "Old.", Body: "old"})
	upload := filepath.Join(t.TempDir(), "upload-1")
	if err := os.WriteFile(upload, zipOf(t, map[string]string{"college/SKILL.md": collegeMD, "college/a.md": "A"}), 0o600); err != nil {
		t.Fatal(err)
	}
	var res protocol.RecordsImportResult
	// The name is the owner's and untrusted; the content decides the format.
	err := h.call(protocol.MethodRecordsImport, protocol.RecordsImportParams{Type: "skill", Path: upload, Name: "../SKILL.md"}, &res)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Records) != 1 || res.Records[0].ID != "college" || !strings.HasPrefix(res.Records[0].Description, "Campus life") {
		t.Fatalf("imported %+v", res.Records)
	}
	if l := h.list(t); len(l) != 1 || l[0].Description != res.Records[0].Description {
		t.Fatalf("import didn't replace: %+v", l)
	}
	if err := os.WriteFile(upload, []byte("not a skill"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = h.call(protocol.MethodRecordsImport, protocol.RecordsImportParams{Type: "skill", Path: upload, Name: "x.md"}, nil)
	wantInvalid(t, err, "starts with ---")
}

func TestRecordsWithoutDatabase(t *testing.T) {
	h := startPlugin(t, map[string]any{}, nil)
	err := h.call(protocol.MethodRecordsList, protocol.RecordsListParams{Type: "skill"}, nil)
	var rpc *protocol.Error
	if !errors.As(err, &rpc) || rpc.Code == -32601 || !strings.Contains(rpc.Message, "no database") {
		t.Fatalf("err = %v", err)
	}
}

func TestPutWithoutEnabledEnablesTheSkill(t *testing.T) {
	h := startWithDB(t, pgtest.URL(t), "i1", map[string]any{})
	err := h.call(protocol.MethodRecordsPut, map[string]any{"type": "skill",
		"record": map[string]any{"name": "college", "description": "x", "body": "y"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got protocol.RecordsGetResult
	if err := h.call(protocol.MethodRecordsGet, protocol.RecordRef{Type: "skill", ID: "college"}, &got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got.Record), `"enabled":true`) {
		t.Fatalf("record %s", got.Record)
	}
}

func TestActionListEnabledSkills(t *testing.T) {
	h := startWithDB(t, pgtest.URL(t), "i1", map[string]any{})
	h.put(t, "", college)
	h.put(t, "", skill{Name: "off", Description: "Off.", Body: "x"})
	// A rename keeps the skill disabled.
	h.put(t, "off", skill{Name: "disabled", Description: "Off.", Body: "x"})
	// Reimporting a disabled skill leaves it disabled.
	upload := filepath.Join(t.TempDir(), "upload")
	if err := os.WriteFile(upload, []byte("---\nname: disabled\ndescription: Still off.\n---\nx"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.call(protocol.MethodRecordsImport, protocol.RecordsImportParams{Type: "skill", Path: upload, Name: "SKILL.md"}, nil); err != nil {
		t.Fatal(err)
	}
	var res protocol.ActionListResult
	if err := h.call(protocol.MethodActionList, nil, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Skills) != 1 || res.Skills[0] != (protocol.Skill{Name: "college", Description: "Campus life."}) {
		t.Fatalf("skills %+v", res.Skills)
	}
}

// skillsHost starts a plugin with college stored and enabled, talking to llm.
func skillsHost(t *testing.T, replies ...fakeReply) (*fakeLLM, *host) {
	t.Helper()
	llm, cfg := withLLM(t, replies...)
	h := startWithDB(t, pgtest.URL(t), "i1", cfg)
	h.put(t, "", college)
	h.put(t, "", skill{Name: "disabled", Description: "Off.", Body: "Never sent."})
	return llm, h
}

func systemPrompt(req map[string]any) string {
	return req["messages"].([]any)[0].(map[string]any)["content"].(string)
}

func TestLoadSkillToolCall(t *testing.T) {
	llm, h := skillsHost(t,
		fakeReply{deltas: []string{toolCall("call_1", "load_skill", `{"name": "college"}`)}},
		fakeReply{deltas: []string{toolCall("call_2", "load_skill_file", `{"name": "college", "path": "references/campus.md"}`)}},
		fakeReply{deltas: []string{toolCall("call_3", "load_skill", `{"name": "disabled"}`)}},
		streamed(`{"description": "A towering orc."}`))
	if _, err := h.run(t, sampleRun(t, "tighten")); err != nil {
		t.Fatal(err)
	}
	tools := llm.requests[0]["tools"].([]any)
	desc := tools[0].(map[string]any)["function"].(map[string]any)["description"].(string)
	if len(tools) != 2 || !strings.Contains(desc, "- college: Campus life.") || strings.Contains(desc, "disabled") {
		t.Fatalf("tools %v", tools)
	}
	if strings.Contains(systemPrompt(llm.requests[0]), "# College") {
		t.Fatal("an unmentioned skill's body is in the prompt")
	}
	results := []string{}
	msgs := llm.requests[3]["messages"].([]any)
	for _, m := range msgs {
		if m := m.(map[string]any); m["role"] == "tool" {
			results = append(results, m["content"].(string))
		}
	}
	if len(results) != 3 || results[0] != "# College\n\nReference files: references/campus.md" || results[1] != "The quad." ||
		!strings.HasPrefix(results[2], "error: no skill") {
		t.Fatalf("tool results %q", results)
	}
	if len(h.calls) != 0 {
		t.Fatalf("skill calls reached the core: %+v", h.calls)
	}
}

func TestForcedSkillInPrompt(t *testing.T) {
	llm, h := skillsHost(t, streamed(`{"description": "x"}`))
	p := sampleRun(t, "tighten")
	p.Context.Skills = []string{"college", "college"}
	if _, err := h.run(t, p); err != nil {
		t.Fatal(err)
	}
	if system := systemPrompt(llm.requests[0]); strings.Count(system, "### college\n\n# College\n\nReference files: references/campus.md") != 1 {
		t.Fatalf("system prompt %q", system)
	}
}

func TestUnknownForcedSkillRefused(t *testing.T) {
	_, h := skillsHost(t)
	for _, name := range []string{"nope", "disabled"} {
		p := sampleRun(t, "tighten")
		p.Context.Skills = []string{name}
		_, err := h.run(t, p)
		wantInvalid(t, err, "unknown skill")
	}
}

func TestNoToolsKeepsForcedSkills(t *testing.T) {
	llm, h := skillsHost(t, fakeReply{status: 400}, streamed(`{"description": "x"}`))
	p := sampleRun(t, "tighten")
	p.Context.Skills = []string{"college"}
	if _, err := h.run(t, p); err != nil {
		t.Fatal(err)
	}
	if llm.requests[0]["tools"] == nil || llm.requests[1]["tools"] != nil {
		t.Fatalf("tools offered %v, then %v", llm.requests[0]["tools"], llm.requests[1]["tools"])
	}
	if !strings.Contains(systemPrompt(llm.requests[1]), "# College") {
		t.Fatal("the forced skill was dropped with the tools")
	}
}

func TestLibraryActionGetsBothToolSets(t *testing.T) {
	llm, h := skillsHost(t, streamed(`{"description": "x"}`))
	if _, err := h.run(t, sampleRun(t, "revise")); err != nil {
		t.Fatal(err)
	}
	if tools := llm.requests[0]["tools"].([]any); len(tools) != len(libraryTools)+2 {
		t.Fatalf("%d tools", len(tools))
	}
}

func TestForcedSkillsCapped(t *testing.T) {
	_, h := skillsHost(t)
	big := skill{Name: "big", Description: "Big.", Body: strings.Repeat("a", maxForced/2+1), Enabled: true}
	h.put(t, "", big)
	big.Name = "big-two"
	h.put(t, "", big)
	p := sampleRun(t, "tighten")
	p.Context.Skills = []string{"big", "big-two"}
	_, err := h.run(t, p)
	wantInvalid(t, err, "256 KiB")
}

func TestSkillsDatabaseErrorSparesActions(t *testing.T) {
	dsn := pgtest.URL(t)
	llm, cfg := withLLM(t, streamed(`{"description": "x"}`))
	h := startWithDB(t, dsn, "i1", cfg)
	conn, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(context.Background(), "DROP TABLE skill"); err != nil {
		t.Fatal(err)
	}
	var res protocol.ActionListResult
	if err := h.call(protocol.MethodActionList, nil, &res); err != nil || len(res.Actions) != len(actions) {
		t.Fatalf("action.list: %+v, %v", res, err)
	}
	if _, err := h.run(t, sampleRun(t, "tighten")); err != nil {
		t.Fatalf("a run that needs no skill failed: %v", err)
	}
	if _, ok := llm.requests[0]["tools"]; ok {
		t.Fatal("skill tools offered without a skill list")
	}
	p := sampleRun(t, "tighten")
	p.Context.Skills = []string{"college"}
	if _, err := h.run(t, p); err == nil {
		t.Fatal("a run with a forced skill succeeded without the database")
	}
}

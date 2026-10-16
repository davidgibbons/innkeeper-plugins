package main

import (
	"strings"
	"testing"

	"github.com/davidgibbons/innkeeper/protocol"
)

func TestModelOptionsFromTheProvider(t *testing.T) {
	cfg := withLLM(t, &fakeLLM{})
	delete(cfg, "model")
	h := started(t, map[string]any{})
	var res protocol.SettingsOptionsResult
	err := h.call(protocol.MethodSettingsOptions, protocol.SettingsOptionsParams{
		SettingsDraft: protocol.SettingsDraft{Config: cfg}, Field: "/model"}, &res)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Options) != 2 || res.Options[0].Value != "m1" || res.Options[1].Value != "m2" {
		t.Fatalf("options %+v", res.Options)
	}
}

func TestModelOptionsWithoutAListSayToType(t *testing.T) {
	h := started(t, map[string]any{})
	err := h.call(protocol.MethodSettingsOptions, protocol.SettingsOptionsParams{SettingsDraft: protocol.SettingsDraft{
		Config: map[string]any{"provider": "anthropic"}, Secrets: map[string]string{"api_key": "k"}}, Field: "/model"}, nil)
	if err == nil || !strings.Contains(err.Error(), "type the model") {
		t.Fatalf("err = %v", err)
	}
}

func TestModelOptionsErrorHidesTheKey(t *testing.T) {
	h := started(t, map[string]any{})
	err := h.call(protocol.MethodSettingsOptions, protocol.SettingsOptionsParams{SettingsDraft: protocol.SettingsDraft{
		Config: withLLM(t, &fakeLLM{status: 401}), Secrets: map[string]string{"api_key": "sk-secret"}}, Field: "/model"}, nil)
	if err == nil || !strings.Contains(err.Error(), "refused by the fake") || strings.Contains(err.Error(), "sk-secret") {
		t.Fatalf("err = %v", err)
	}
}

func TestOptionsForAnotherFieldIsInvalid(t *testing.T) {
	h := started(t, map[string]any{})
	err := h.call(protocol.MethodSettingsOptions, protocol.SettingsOptionsParams{
		SettingsDraft: protocol.SettingsDraft{Config: map[string]any{"provider": "ollama"}}, Field: "/base_url"}, nil)
	if rpcCode(err) != protocol.CodeInvalidParams {
		t.Fatalf("err = %v", err)
	}
}

func testConnection(t *testing.T, draft protocol.SettingsDraft) protocol.SettingsRunResult {
	t.Helper()
	h := started(t, map[string]any{})
	var res protocol.SettingsRunResult
	if err := h.call(protocol.MethodSettingsRun, protocol.SettingsRunParams{SettingsDraft: draft, Action: "test"}, &res); err != nil {
		t.Fatal(err)
	}
	return res
}

func TestConnectionTestChecksEachKind(t *testing.T) {
	llm := &fakeLLM{chat: []string{content("OK")}}
	res := testConnection(t, protocol.SettingsDraft{Config: withLLM(t, llm, "chat", "embed")})
	if !res.OK || len(res.Checks) != 2 || res.Checks[0].Name != "Chat" || res.Checks[1].Name != "Embeddings" ||
		!res.Checks[0].OK || !res.Checks[1].OK || res.Checks[0].LatencyMS <= 0 || res.Checks[1].LatencyMS <= 0 {
		t.Fatalf("result %+v", res)
	}
	if len(llm.paths) != 2 || !strings.HasSuffix(llm.paths[0], "/chat/completions") || !strings.HasSuffix(llm.paths[1], "/embeddings") {
		t.Fatalf("paths %v", llm.paths)
	}
}

func TestConnectionTestReportsAFailedKind(t *testing.T) {
	res := testConnection(t, protocol.SettingsDraft{Config: withLLM(t, &fakeLLM{status: 400}),
		Secrets: map[string]string{"api_key": "sk-secret"}})
	if res.OK || len(res.Checks) != 1 || res.Checks[0].OK || !strings.Contains(res.Checks[0].Message, "refused by the fake") ||
		strings.Contains(res.Checks[0].Message, "sk-secret") {
		t.Fatalf("result %+v", res)
	}
}

func TestConnectionTestReportsBadSettings(t *testing.T) {
	for _, cfg := range []map[string]any{{}, {"provider": "openai-compatible", "model": "m"}} {
		res := testConnection(t, protocol.SettingsDraft{Config: cfg})
		if res.OK || res.Message == "" || len(res.Checks) != 0 {
			t.Errorf("%v: result %+v", cfg, res)
		}
	}
}

func TestUnknownSettingsActionIsInvalid(t *testing.T) {
	h := started(t, map[string]any{})
	err := h.call(protocol.MethodSettingsRun, protocol.SettingsRunParams{Action: "reboot"}, nil)
	if rpcCode(err) != protocol.CodeInvalidParams {
		t.Fatalf("err = %v", err)
	}
}

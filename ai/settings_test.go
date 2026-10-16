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

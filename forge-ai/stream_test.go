package main

import (
	"slices"
	"strings"
	"testing"
)

func TestVisibleDropsThinkBlocks(t *testing.T) {
	cases := map[string]string{
		`<think>plan</think>{"x":1}`:                  `{"x":1}`,
		`{"x":1}<think>more`:                          `{"x":1}`,
		"<think>a</think>\n{\"x\":<think>b</think>1}": "\n{\"x\":1}",
		`no think`: `no think`,
	}
	for in, want := range cases {
		if got := visible(in); got != want {
			t.Errorf("visible(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStartedKeysOnPartialOutput(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"Sure! ```json\n{\"name\": \"Bo\", \"descr", []string{"name"}},
		{"```json\n{\"name\": \"Bo\", \"description\": \"A big", []string{"name", "description"}},
		{`{"name": "Bo", "tags": ["a", "b"], "first_mes": "Hi`, []string{"name", "tags", "first_mes"}},
		{`<think>{"wrong": 1}</think>{"name": "Bo"}`, []string{"name"}},
	}
	for _, c := range cases {
		if got := startedKeys(c.in); !slices.Equal(got, c.want) {
			t.Errorf("startedKeys(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseObjectAfterThinkAndFence(t *testing.T) {
	obj, err := parseObject("<think>hmm {not json}</think>\n```json\n{\"name\": \"Bo\", \"tags\": [\"x\"]}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if string(obj["name"]) != `"Bo"` || string(obj["tags"]) != `["x"]` {
		t.Fatalf("parsed %v", obj)
	}
	if _, err := parseObject("I can't help with that."); err == nil || !strings.Contains(err.Error(), "I can't help") {
		t.Fatalf("prose: err = %v, want one quoting the output", err)
	}
	if _, err := parseObject(`{"name": "Bo", "desc`); err == nil {
		t.Fatal("truncated object parsed")
	}
}

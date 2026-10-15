package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// visible is the model's output without <think> blocks. An unclosed block
// hides the rest, since the model is still reasoning.
func visible(s string) string {
	var b strings.Builder
	for {
		open := strings.Index(s, "<think>")
		if open < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:open])
		end := strings.Index(s[open:], "</think>")
		if end < 0 {
			return b.String()
		}
		s = s[open+end+len("</think>"):]
	}
}

// startedKeys lists the top-level keys of the JSON object the output has
// begun, in order, including one whose value is still streaming.
func startedKeys(output string) []string {
	v := visible(output)
	i := strings.IndexByte(v, '{')
	if i < 0 {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(v[i:]))
	if _, err := dec.Token(); err != nil {
		return nil
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		key, ok := tok.(string)
		if err != nil || !ok {
			break
		}
		keys = append(keys, key)
		var value json.RawMessage
		if dec.Decode(&value) != nil {
			break
		}
	}
	return keys
}

// parseObject reads the first JSON object in the output, after any <think>
// blocks and around any code fence or prose.
func parseObject(output string) (map[string]json.RawMessage, error) {
	v := strings.TrimSpace(visible(output))
	var obj map[string]json.RawMessage
	i := strings.IndexByte(v, '{')
	if i >= 0 && json.NewDecoder(strings.NewReader(v[i:])).Decode(&obj) == nil {
		return obj, nil
	}
	if len(v) > 150 {
		v = v[:150] + "…"
	}
	return nil, fmt.Errorf("the model didn't return a JSON object: %q", v)
}

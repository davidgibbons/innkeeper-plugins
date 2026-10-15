package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// visible is the model's output without reasoning. A <think> block is
// removed, an unclosed one hides the rest since the model is still reasoning,
// and a </think> with no opening tag (Qwen3 and R1 templates put that in the
// prompt) drops everything before it.
func visible(s string) string {
	var b strings.Builder
	for {
		open, end := strings.Index(s, "<think>"), strings.Index(s, "</think>")
		if end >= 0 && (open < 0 || end < open) {
			b.Reset()
			s = s[end+len("</think>"):]
			continue
		}
		if open < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:open])
		end = strings.Index(s[open:], "</think>")
		if end < 0 {
			return b.String()
		}
		s = s[open+end+len("</think>"):]
	}
}

// objectStarts yields the output from each '{' on, since prose before the
// JSON may hold braces of its own, such as {{char}}.
func objectStarts(v string) func(func(string) bool) {
	return func(yield func(string) bool) {
		for i := strings.IndexByte(v, '{'); i >= 0; {
			if !yield(v[i:]) {
				return
			}
			next := strings.IndexByte(v[i+1:], '{')
			if next < 0 {
				return
			}
			i += 1 + next
		}
	}
}

// startedKeys lists the top-level keys of the JSON object the output has
// begun, in order, including one whose value is still streaming.
func startedKeys(output string) []string {
	for start := range objectStarts(visible(output)) {
		if keys := keysFrom(start); len(keys) > 0 {
			return keys
		}
	}
	return nil
}

func keysFrom(s string) []string {
	dec := json.NewDecoder(strings.NewReader(s))
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

// parseObject reads the first JSON object in the output, after any reasoning
// and around any code fence or prose.
func parseObject(output string) (map[string]json.RawMessage, error) {
	v := strings.TrimSpace(visible(output))
	for start := range objectStarts(v) {
		var obj map[string]json.RawMessage
		if json.NewDecoder(strings.NewReader(start)).Decode(&obj) == nil {
			return obj, nil
		}
	}
	if len(v) > 150 {
		v = v[:150] + "…"
	}
	return nil, fmt.Errorf("the model didn't return a JSON object: %q", v)
}

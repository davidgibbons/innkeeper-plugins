package main

import (
	"embed"
	"encoding/json"
	"strings"
	"text/template"

	"github.com/davidgibbons/innkeeper/protocol"
)

//go:embed prompts/*.tmpl
var promptFiles embed.FS

// The prompts use [[ ]] so that {{char}} and {{user}} reach the model as written.
var templates = template.Must(template.New("").Delims("[[", "]]").ParseFS(promptFiles, "prompts/*.tmpl"))

// promptData is what the templates see.
type promptData struct {
	Card        string
	Field       string
	FieldText   string
	Instruction string
	PointOfView string
	Count       int
	Image       bool
	Context     protocol.ActionContext
}

// prompts renders the run's system and user prompts. The system prompt ends
// with the owner's house style and this action's addition.
func (r *run) prompts(s settings) (system, user string, err error) {
	d := promptData{Field: r.Field, Instruction: r.Instruction, PointOfView: r.pointOfView, Count: r.count,
		Image: r.Images.Reference != "" || r.Images.Avatar != "", Context: r.Context}
	if r.draft != nil {
		card, err := r.sent()
		if err != nil {
			return "", "", err
		}
		var b strings.Builder
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false) // Keep <START> as written.
		enc.SetIndent("", "  ")
		if err := enc.Encode(card); err != nil {
			return "", "", err
		}
		d.Card = strings.TrimSpace(b.String())
		if r.Field != "" {
			d.FieldText, _ = card[r.Field].(string)
		}
	}
	if system, err = execute(r.ID+".system", d); err != nil {
		return "", "", err
	}
	if user, err = execute(r.ID+".user", d); err != nil {
		return "", "", err
	}
	if s.HouseStyle != "" {
		system += "\n\n## House style\n\n" + s.HouseStyle
	}
	if a := s.Additions[r.ID]; a != "" {
		system += "\n\n## For this action\n\n" + a
	}
	return system, user, nil
}

func execute(name string, d promptData) (string, error) {
	var b strings.Builder
	if err := templates.ExecuteTemplate(&b, name, d); err != nil {
		return "", err
	}
	return strings.TrimSpace(b.String()), nil
}

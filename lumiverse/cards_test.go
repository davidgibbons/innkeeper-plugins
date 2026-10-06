package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/davidgibbons/innkeeper/protocol"
)

func card(fields map[string]any) json.RawMessage {
	data := map[string]any{"name": "Brakka", "description": "A dwarf smith.", "first_mes": "Hail.",
		"tags": []any{"dwarf"}, "character_version": "2",
		"extensions": map[string]any{"depth_prompt": map[string]any{"depth": 4.0, "prompt": "Grumble."}}}
	for k, v := range fields {
		data[k] = v
	}
	raw, _ := json.Marshal(map[string]any{"spec": "chara_card_v3", "spec_version": "3.0", "data": data})
	return raw
}

// avatarFile writes an avatar where a blob store would keep it, named by sha.
func avatarFile(t *testing.T, sha string) string {
	path := filepath.Join(t.TempDir(), sha)
	if err := os.WriteFile(path, []byte("\x89PNG fake "+sha), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func put(t *testing.T, p *plugin, in protocol.TargetPutParams) string {
	t.Helper()
	res, err := p.put(context.Background(), mustJSON(t, in))
	if err != nil {
		t.Fatal(err)
	}
	return res.(protocol.TargetPutResult).RemoteID
}

func TestCreateCharacter(t *testing.T) {
	f := newFake(t)
	p := newPlugin(t, f, "owner", fakePassword)
	id := put(t, p, protocol.TargetPutParams{Card: card(map[string]any{"character_book": map[string]any{"entries": []any{}}}),
		Key: "1:2:3", Avatar: avatarFile(t, "aaa"), LorebookRemoteIDs: []string{"wb9"}})
	c := f.characters[id]
	ext := object(c["extensions"])
	if c["name"] != "Brakka" || c["first_mes"] != "Hail." || ext["character_book"] != nil {
		t.Fatalf("character = %v", c)
	}
	if fmt.Sprint(ext["world_book_ids"]) != "[wb9]" || fmt.Sprint(ext[innkeeperKey]) != "map[avatar:aaa key:1:2:3]" ||
		ext["character_version"] != "2" || ext["depth_prompt"] == nil || f.avatars != 1 {
		t.Fatalf("extensions = %v, %d avatars", ext, f.avatars)
	}
}

// A re-run after a crash finds the character its key made.
func TestPutWithAKnownKeyReusesTheCharacter(t *testing.T) {
	f := newFake(t)
	p := newPlugin(t, f, "owner", fakePassword)
	first := put(t, p, protocol.TargetPutParams{Card: card(nil), Key: "k1"})
	put(t, p, protocol.TargetPutParams{Card: card(nil), Key: "k2"})
	if again := put(t, p, protocol.TargetPutParams{Card: card(nil), Key: "k1"}); again != first || len(f.characters) != 2 {
		t.Fatalf("re-run made %s; %d characters", again, len(f.characters))
	}
}

func TestUpdateCharacter(t *testing.T) {
	f := newFake(t)
	p := newPlugin(t, f, "owner", fakePassword)
	avatar := avatarFile(t, "aaa")
	id := put(t, p, protocol.TargetPutParams{Card: card(nil), Key: "k", Avatar: avatar, LorebookRemoteIDs: []string{"wb1"}})
	// Lumiverse's own settings, made in its UI.
	ext := object(f.characters[id]["extensions"])
	ext["ttsVoice"], ext["_lumiverse_fav"] = "alto", true

	put(t, p, protocol.TargetPutParams{Card: card(map[string]any{"first_mes": "Well met.", "extensions": map[string]any{}}),
		RemoteID: id, Avatar: avatar})
	c := f.characters[id]
	ext = object(c["extensions"])
	if c["first_mes"] != "Well met." || ext["depth_prompt"] != nil {
		t.Fatalf("character = %v", c)
	}
	if ext["ttsVoice"] != "alto" || ext["_lumiverse_fav"] != true || fmt.Sprint(ext["world_book_ids"]) != "[]" {
		t.Fatalf("extensions = %v", ext)
	}
	if f.avatars != 1 {
		t.Fatalf("uploaded the same avatar %d times", f.avatars)
	}
	object(f.characters[id]["extensions"])["avatar_crop_image_id"] = "crop-1"
	put(t, p, protocol.TargetPutParams{Card: card(nil), RemoteID: id, Avatar: avatarFile(t, "bbb")})
	if f.avatars != 2 {
		t.Fatal("a new avatar wasn't uploaded")
	}
	if _, ok := object(f.characters[id]["extensions"])["avatar_crop_image_id"]; ok {
		t.Fatal("the push restored the old avatar's crop")
	}
	if _, err := p.put(context.Background(), mustJSON(t, protocol.TargetPutParams{Card: card(nil), RemoteID: "gone"})); !notFound(err) {
		t.Fatalf("put to a missing character: %v", err)
	}
}

func TestGetCharacter(t *testing.T) {
	f := newFake(t)
	p := newPlugin(t, f, "owner", fakePassword)
	id := put(t, p, protocol.TargetPutParams{Card: card(nil), Key: "k", LorebookRemoteIDs: []string{"wb1"}})
	object(f.characters[id]["extensions"])["ttsVoice"] = "alto"
	res, err := p.get(context.Background(), mustJSON(t, protocol.TargetGetParams{RemoteID: id}))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(res.(protocol.TargetGetResult).Card, &got)
	if fmt.Sprint(got.Data["extensions"]) != "map[depth_prompt:map[depth:4 prompt:Grumble.]]" {
		t.Fatalf("extensions = %v", got.Data["extensions"])
	}
	if got.Data["character_version"] != "2" || got.Data["first_mes"] != "Hail." || got.Data["character_book"] != nil {
		t.Fatalf("data = %v", got.Data)
	}
	if _, err := p.get(context.Background(), mustJSON(t, protocol.TargetGetParams{RemoteID: "gone"})); !notFound(err) {
		t.Fatalf("get of a missing character: %v", err)
	}
}

// Lumiverse returns at most 1000 characters a page.
func TestListCharacters(t *testing.T) {
	f := newFake(t)
	p := newPlugin(t, f, "owner", fakePassword)
	for i := range 1001 {
		id := fmt.Sprintf("c%04d", i)
		f.characters[id] = map[string]any{"id": id, "name": fmt.Sprintf("Char %04d", i)}
	}
	res, err := p.list(context.Background(), "/characters/summary?sort=name&direction=asc")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, it := range res.Items {
		seen[it.RemoteID] = true
		if f.characters[it.RemoteID]["name"] != it.Name {
			t.Fatalf("item %v", it)
		}
	}
	if len(res.Items) != 1001 || len(seen) != 1001 {
		t.Fatalf("listed %d items, %d distinct", len(res.Items), len(seen))
	}
}

func TestGetAvatar(t *testing.T) {
	f := newFake(t)
	p := newPlugin(t, f, "owner", fakePassword)
	id := put(t, p, protocol.TargetPutParams{Card: card(nil), Key: "k", Avatar: avatarFile(t, "abc")})
	res, err := p.get(context.Background(), mustJSON(t, protocol.TargetGetParams{RemoteID: id, Avatar: true}))
	if err != nil {
		t.Fatal(err)
	}
	path := res.(protocol.TargetGetResult).Avatar
	img, err := os.ReadFile(path)
	if filepath.Dir(path) != p.blobTmp || err != nil || !bytes.Equal(img, f.images[id]) {
		t.Fatalf("avatar %q: %v", path, err)
	}
}

func TestGetNoAvatar(t *testing.T) {
	f := newFake(t)
	p := newPlugin(t, f, "owner", fakePassword)
	id := put(t, p, protocol.TargetPutParams{Card: card(nil), Key: "k"})
	res, err := p.get(context.Background(), mustJSON(t, protocol.TargetGetParams{RemoteID: id, Avatar: true}))
	if err != nil || res.(protocol.TargetGetResult).Avatar != "" {
		t.Fatalf("avatar %q: %v", res.(protocol.TargetGetResult).Avatar, err)
	}
}

func TestGetWithoutAvatarAsksForNone(t *testing.T) {
	f := newFake(t)
	p := newPlugin(t, f, "owner", fakePassword)
	id := put(t, p, protocol.TargetPutParams{Card: card(nil), Key: "k", Avatar: avatarFile(t, "abc")})
	if _, err := p.get(context.Background(), mustJSON(t, protocol.TargetGetParams{RemoteID: id})); err != nil || f.avatarGets != 0 {
		t.Fatalf("%d avatar requests: %v", f.avatarGets, err)
	}
}

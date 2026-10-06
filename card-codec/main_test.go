package main

import (
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davidgibbons/innkeeper/protocol"
)

func TestEncodeDecode(t *testing.T) {
	tmp := t.TempDir()
	avatar := filepath.Join(tmp, "avatar.png")
	f, err := os.Create(avatar)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewGray(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	f.Close()
	params, _ := json.Marshal(protocol.CodecEncodeParams{
		Card:   json.RawMessage(`{"spec": "chara_card_v3", "spec_version": "3.0", "data": {"name": "Brakka"}}`),
		Format: "png", Avatar: avatar,
	})
	res, err := encode(tmp, params)
	if err != nil {
		t.Fatal(err)
	}
	enc := res.(protocol.CodecEncodeResult)
	if filepath.Dir(enc.Path) != tmp || enc.Extension != "png" {
		t.Fatalf("encode = %+v", enc)
	}

	params, _ = json.Marshal(protocol.CodecDecodeParams{Path: enc.Path})
	res, err = decode(tmp, params)
	if err != nil {
		t.Fatal(err)
	}
	dec := res.(protocol.CodecDecodeResult)
	if !strings.Contains(string(dec.Card), `"Brakka"`) || filepath.Dir(dec.Avatar) != tmp {
		t.Fatalf("decode = %s %q", dec.Card, dec.Avatar)
	}
}

func TestDecodeNotCard(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "notes.txt")
	if err := os.WriteFile(path, []byte("not a card"), 0o644); err != nil {
		t.Fatal(err)
	}
	params, _ := json.Marshal(protocol.CodecDecodeParams{Path: path})
	_, err := decode(tmp, params)
	var rpcErr *protocol.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != protocol.CodeInvalidParams {
		t.Fatalf("err = %v, want -32602", err)
	}
}

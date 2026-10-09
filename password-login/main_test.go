package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/davidgibbons/innkeeper/protocol"
	"golang.org/x/crypto/bcrypt"
)

func completeParams(password string) json.RawMessage {
	b, _ := json.Marshal(protocol.AuthCompleteParams{Params: map[string]string{"password": password}})
	return b
}

func TestComplete(t *testing.T) {
	h, err := bcrypt.GenerateFromPassword([]byte("letmein"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	res, err := complete(string(h), completeParams("letmein"))
	if err != nil || res.(protocol.AuthCompleteResult).Subject != "owner" {
		t.Fatalf("complete = %v, %v", res, err)
	}
	for name, c := range map[string]struct{ hash, password string }{
		"wrong password": {string(h), "nope"},
		"no password":    {string(h), ""},
		"no hash":        {"", "letmein"},
		"not bcrypt":     {"letmein", "letmein"},
	} {
		if _, err := complete(c.hash, completeParams(c.password)); err == nil {
			t.Errorf("%s: logged in", name)
		}
	}
}

func TestHash(t *testing.T) {
	var out bytes.Buffer
	if err := printHash(strings.NewReader("letmein\n"), &out); err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword(bytes.TrimSpace(out.Bytes()), []byte("letmein")); err != nil {
		t.Fatalf("hash %q doesn't match: %v", out.String(), err)
	}
	if err := printHash(strings.NewReader("\n"), io.Discard); err == nil {
		t.Fatal("hashed an empty password")
	}
}

func TestSetup(t *testing.T) {
	res, err := setupComplete(json.RawMessage(`{"params": {"password": "correct horse battery", "confirm": "correct horse battery"}}`))
	if err != nil {
		t.Fatal(err)
	}
	r := res.(protocol.AuthSetupCompleteResult)
	if r.Subject != subject || bcrypt.CompareHashAndPassword([]byte(r.Secrets["password_hash"]), []byte("correct horse battery")) != nil {
		t.Fatalf("got %+v", r)
	}
	for _, bad := range []string{
		`{"params": {"password": "correct horse battery", "confirm": "correct horse"}}`,
		`{"params": {"password": "short", "confirm": "short"}}`,
	} {
		if _, err := setupComplete(json.RawMessage(bad)); err == nil {
			t.Errorf("%s: accepted", bad)
		}
	}
}

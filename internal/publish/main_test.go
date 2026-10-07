package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestPublish(t *testing.T) {
	out := t.TempDir()
	tag, err := publish("../..", out, "https://example.com/dl", "password-login")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tag, "password-login-v") {
		t.Fatalf("tag = %s", tag)
	}
	var index struct {
		Plugin []entry `toml:"plugin"`
	}
	if _, err := toml.DecodeFile(filepath.Join(out, tag, "entry.toml"), &index); err != nil {
		t.Fatal(err)
	}
	if len(index.Plugin) != 1 || len(index.Plugin[0].Artifact) != len(platforms) || index.Plugin[0].Path != "" {
		t.Fatalf("entry = %+v", index.Plugin)
	}
	for _, a := range index.Plugin[0].Artifact {
		file := filepath.Join(out, tag, filepath.Base(a.URL))
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != a.SHA256 || a.URL != "https://example.com/dl/"+tag+"/"+filepath.Base(a.URL) {
			t.Fatalf("artifact %+v doesn't match its file", a)
		}
		f, _ := os.Open(file)
		gz, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		tr := tar.NewReader(gz)
		for h, err := tr.Next(); err == nil; h, err = tr.Next() {
			names = append(names, h.Name)
		}
		f.Close()
		slices.Sort(names)
		if !slices.Equal(names, []string{"password-login", "plugin.toml"}) {
			t.Fatalf("%s holds %v", a.URL, names)
		}
	}
}

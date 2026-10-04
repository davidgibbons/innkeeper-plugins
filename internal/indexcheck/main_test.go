package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRepoIndex(t *testing.T) {
	if errs := check("../.."); len(errs) > 0 {
		t.Fatal(errs)
	}
}

func TestCheckFindsProblems(t *testing.T) {
	root := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest := "name = %q\nversion = \"1.0.0\"\nprotocol = 1\ncommand = [%q]\ncapabilities = [\"push\"]\n"
	write("a/plugin.toml", fmt.Sprintf(manifest, "a", "./a"))
	write("b/plugin.toml", fmt.Sprintf(manifest, "b", "./b"))
	write("c/plugin.toml", fmt.Sprintf(manifest, "c", "./other"))
	write("index.toml", `
[[plugin]]
name = "a"
path = "a"
version = "2.0.0"
protocol = 1
capabilities = ["push"]
description = "Version doesn't match."

[[plugin]]
name = "c"
path = "c"
version = "1.0.0"
protocol = 1
capabilities = ["push"]
description = "Command isn't ./c."

[[plugin]]
name = "gone"
path = "gone"
version = "1.0.0"
protocol = 1
capabilities = ["push"]
description = "No folder."
`)
	// a's version, c's command, gone's folder, and b missing from the index.
	if errs := check(root); len(errs) != 4 {
		t.Fatalf("got %d problems, want 4: %v", len(errs), errs)
	}
}

func TestCheckRejectsRepoRootPath(t *testing.T) {
	root := t.TempDir()
	index := "[[plugin]]\nname = \"x\"\npath = \".\"\nversion = \"1.0.0\"\nprotocol = 1\ncapabilities = []\ndescription = \"d\"\n"
	if err := os.WriteFile(filepath.Join(root, "index.toml"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	if errs := check(root); len(errs) != 1 {
		t.Fatalf("got %d problems, want 1: %v", len(errs), errs)
	}
}

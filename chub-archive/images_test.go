package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadImage(t *testing.T) {
	dirs := []string{"testdata/missing", "testdata/images"}
	if b, err := readImage(dirs, "abc0000000000000000000000000000d"); err != nil || len(b) == 0 {
		t.Fatalf("didn't find the fixture image in the second folder: %v", err)
	}
	for _, hash := range []string{"", "0", "fff0000000000000000000000000000f", "../../../../etc/passwd", "ABC0000000000000000000000000000D"} {
		if b, err := readImage(dirs, hash); b != nil || err != nil {
			t.Errorf("readImage(%q) read %d bytes, err %v", hash, len(b), err)
		}
	}

	// A file that exists but can't be read is an error, not a missing image.
	broken := t.TempDir()
	if err := os.MkdirAll(filepath.Join(broken, "a", "b", "c", "0000000000000000000000000000d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if b, err := readImage([]string{broken, "testdata/images"}, "abc0000000000000000000000000000d"); err == nil {
		t.Fatalf("read %d bytes from a directory, want an error", len(b))
	}
}

// fixtureHash names the fixture image, a PNG in testdata/images.
const fixtureHash = "abc0000000000000000000000000000d"

func TestSaveImage(t *testing.T) {
	store := t.TempDir()
	b, err := os.ReadFile("testdata/images/a/b/c/0000000000000000000000000000d")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := saveImage(store, fixtureHash, b); err != nil {
			t.Fatal(err)
		}
	}
	want := filepath.Join(store, "ab", "c0", fixtureHash+".png")
	if got := findStored(store, fixtureHash); got != want {
		t.Fatalf("findStored = %q, want %q", got, want)
	}
	if entries, _ := os.ReadDir(filepath.Dir(want)); len(entries) != 1 {
		t.Fatalf("store folder holds %d files, want only the image", len(entries))
	}

	// Bytes that aren't an image, and hashes that could name another path, store nothing.
	other := "fff0000000000000000000000000000f"
	if err := saveImage(store, other, []byte("not an image")); err != nil || findStored(store, other) != "" {
		t.Fatalf("stored a non-image, err %v", err)
	}
	if err := saveImage(store, "../x", b); err != nil || findStored(store, "../x") != "" {
		t.Fatalf("stored under a bad hash, err %v", err)
	}
	if findStored("", fixtureHash) != "" {
		t.Fatal("found an image with no store set")
	}
}

func TestCheckWritable(t *testing.T) {
	if err := checkWritable(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"", "testdata/missing", "testdata/dump.sql"} {
		if err := checkWritable(dir); err == nil {
			t.Errorf("checkWritable(%q) passed, want an error", dir)
		}
	}
}

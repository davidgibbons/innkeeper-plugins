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

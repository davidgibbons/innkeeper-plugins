package main

import "testing"

func TestReadImage(t *testing.T) {
	dirs := []string{"testdata/missing", "testdata/images"}
	if b := readImage(dirs, "abc0000000000000000000000000000d"); len(b) == 0 {
		t.Fatal("didn't find the fixture image in the second folder")
	}
	for _, hash := range []string{"", "0", "fff0000000000000000000000000000f", "../../../../etc/passwd", "ABC0000000000000000000000000000D"} {
		if b := readImage(dirs, hash); b != nil {
			t.Errorf("readImage(%q) read %d bytes", hash, len(b))
		}
	}
}

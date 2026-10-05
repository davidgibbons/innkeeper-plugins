package pluginio

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadFileCaps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxInput + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := ReadFile(path); err == nil {
		t.Fatal("read a file over MaxInput")
	}
}

func TestWriteTmp(t *testing.T) {
	tmp := t.TempDir()
	path, err := WriteTmp(tmp, "avatar-*.png", []byte("x"))
	if err != nil || filepath.Dir(path) != tmp {
		t.Fatalf("WriteTmp = %q, %v", path, err)
	}
	if _, err := WriteTmp("", "avatar-*.png", nil); err == nil {
		t.Fatal("wrote without a blob_tmp")
	}
}

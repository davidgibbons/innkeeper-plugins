package main

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/davidgibbons/innkeeper-plugins/internal/pluginio"
)

// imageExts are the image types the store keeps, most common first, since
// findStored tries each in turn.
var imageExts = []struct{ mime, ext string }{{"image/png", "png"}, {"image/webp", "webp"}, {"image/jpeg", "jpg"}, {"image/gif", "gif"}}

// validHash reports whether hash is lowercase hex long enough to name a
// file, so it can't name a path outside a folder.
func validHash(hash string) bool {
	return len(hash) >= 4 && strings.Trim(hash, "0123456789abcdef") == ""
}

// readImage reads the image the archive stores under hash, from the first of
// dirs that has it. It returns nil and no error when none has it. The
// archive keeps each at <a>/<b>/<c>/<the rest of the hash>.
func readImage(dirs []string, hash string) ([]byte, error) {
	if !validHash(hash) {
		return nil, nil
	}
	for _, dir := range dirs {
		b, err := pluginio.ReadFile(filepath.Join(dir, hash[:1], hash[1:2], hash[2:3], hash[3:]))
		if !errors.Is(err, fs.ErrNotExist) {
			return b, err
		}
	}
	return nil, nil
}

// storePath is where the image store keeps the image with hash.
func storePath(store, hash, ext string) string {
	return filepath.Join(store, hash[:2], hash[2:4], hash+"."+ext)
}

// findStored returns the path of the image with hash in the store, or "" if
// the store doesn't have it.
func findStored(store, hash string) string {
	if store == "" || !validHash(hash) {
		return ""
	}
	for _, t := range imageExts {
		if p := storePath(store, hash, t.ext); fileExists(p) {
			return p
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// saveImage writes b, the image with hash, into the store unless it is there
// already. Bytes that aren't a stored image type are skipped. It writes a
// temporary file and renames it, so a crashed ingest never leaves a partial image.
func saveImage(store, hash string, b []byte) error {
	var ext string
	mime := http.DetectContentType(b)
	for _, t := range imageExts {
		if t.mime == mime {
			ext = t.ext
		}
	}
	if ext == "" || !validHash(hash) {
		return nil
	}
	path := storePath(store, hash, ext)
	if fileExists(path) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	// CreateTemp makes files only their owner can read.
	if err == nil {
		err = os.Chmod(f.Name(), 0o644)
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

// checkWritable fails unless dir is a folder ingest can write to. It
// doesn't create dir: a missing store usually means a missing mount.
func checkWritable(dir string) error {
	if dir == "" {
		return errors.New("is not set")
	}
	f, err := os.CreateTemp(dir, ".check-*")
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(f.Name())
}

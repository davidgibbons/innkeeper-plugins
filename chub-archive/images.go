package main

import (
	"errors"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/inkandale/innkeeper-plugins/internal/pluginio"
)

// imageExts are the image types an avatar may be, by sniffed MIME type.
var imageExts = map[string]string{"image/png": "png", "image/webp": "webp", "image/jpeg": "jpg", "image/gif": "gif"}

// imageExt returns the extension for b's image type, or "" if b isn't one.
func imageExt(b []byte) string {
	return imageExts[http.DetectContentType(b)]
}

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

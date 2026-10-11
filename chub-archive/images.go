package main

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/davidgibbons/innkeeper-plugins/internal/pluginio"
)

// readImage reads the image the archive stores under hash, from the first of
// dirs that has it. It returns nil and no error when none has it. The
// archive keeps each at <a>/<b>/<c>/<the rest of the hash>.
func readImage(dirs []string, hash string) ([]byte, error) {
	// Lowercase hex only, so a hash can't name a path outside dirs.
	if len(hash) < 4 || strings.Trim(hash, "0123456789abcdef") != "" {
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

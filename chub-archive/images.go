package main

import (
	"path/filepath"
	"strings"

	"github.com/davidgibbons/innkeeper-plugins/internal/pluginio"
)

// readImage reads the image the archive stores under hash, from the first of
// dirs that has it, or returns nil. The archive keeps each at
// <a>/<b>/<c>/<the rest of the hash>.
func readImage(dirs []string, hash string) []byte {
	// Lowercase hex only, so a hash can't name a path outside dirs.
	if len(hash) < 4 || strings.Trim(hash, "0123456789abcdef") != "" {
		return nil
	}
	for _, dir := range dirs {
		if b, err := pluginio.ReadFile(filepath.Join(dir, hash[:1], hash[1:2], hash[2:3], hash[3:])); err == nil {
			return b
		}
	}
	return nil
}

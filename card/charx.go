package card

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/davidgibbons/innkeeper-plugins/internal/pluginio"
)

func isZip(b []byte) bool { return bytes.HasPrefix(b, []byte("PK\x03\x04")) }

// decodeCharX reads a CharX zip: the card from card.json and the avatar from
// the main icon asset. A missing or unreadable icon gives no avatar.
func decodeCharX(b []byte) (card json.RawMessage, img []byte, err error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrNotCard, err)
	}
	// card.json and the icon share one cap, so a zip bomb can't exhaust memory.
	budget := int64(pluginio.MaxInput)
	raw, err := readZipEntry(zr, "card.json", &budget)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: CharX card.json: %w", ErrNotCard, err)
	}
	if card, err = toV3(raw); err != nil {
		return nil, nil, err
	}
	var c struct {
		Data struct {
			Assets []struct {
				Type string `json:"type"`
				Name string `json:"name"`
				URI  string `json:"uri"`
			} `json:"assets"`
		} `json:"data"`
	}
	_ = json.Unmarshal(card, &c)
	for _, a := range c.Data.Assets {
		if name, ok := strings.CutPrefix(a.URI, "embeded://"); ok && a.Type == "icon" && a.Name == "main" {
			icon, err := readZipEntry(zr, name, &budget)
			if err != nil {
				return card, nil, nil
			}
			if img, err = asPNG(icon); err != nil {
				img = nil
			}
			return card, img, nil
		}
	}
	return card, nil, nil
}

// readZipEntry reads one file from zr, taking its size from budget. zr.Open
// rejects names that aren't valid fs paths, such as ones with "..".
func readZipEntry(zr *zip.Reader, name string, budget *int64) ([]byte, error) {
	f, err := zr.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, *budget+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > *budget {
		return nil, errors.New("CharX is over the size limit")
	}
	*budget -= int64(len(b))
	return b, nil
}

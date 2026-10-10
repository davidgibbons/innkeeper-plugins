package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// errBadRow marks a dump row the plugin can't read. Ingest skips it.
var errBadRow = errors.New("bad row")

// checkpoint is where ingest is in the dump: the byte offset of the next
// line, and the COPY block it is in, if any.
type checkpoint struct {
	Table   string   `json:"table,omitempty"`
	Columns []string `json:"columns,omitempty"`
	Offset  int64    `json:"offset"`
}

// dump reads the rows of a pg_dump plain-text file's COPY blocks.
type dump struct {
	f    *os.File
	r    *bufio.Reader
	at   checkpoint
	size int64
}

var copyHeader = regexp.MustCompile(`^COPY public\.(\w+) \((.*)\) FROM stdin;$`)

// openDump opens the dump at path, positioned at at.
func openDump(path string, at checkpoint) (*dump, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil {
		_, err = f.Seek(at.Offset, io.SeekStart)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return &dump{f: f, r: bufio.NewReaderSize(f, 1<<20), at: at, size: info.Size()}, nil
}

func (d *dump) Close() error { return d.f.Close() }

// next returns the next row of a table ingest reads, as column to value. A
// NULL column is absent. It returns io.EOF after the last row, and errBadRow
// for a row with the wrong number of fields.
func (d *dump) next() (table string, row map[string]string, err error) {
	for {
		line, err := d.r.ReadBytes('\n')
		if errors.Is(err, io.EOF) && len(line) > 0 {
			err = nil
		}
		if err != nil {
			return "", nil, err
		}
		d.at.Offset += int64(len(line))
		line = bytes.TrimSuffix(line, []byte("\n"))
		if d.at.Table == "" {
			if m := copyHeader.FindSubmatch(line); m != nil {
				d.at.Table, d.at.Columns = string(m[1]), strings.Split(string(m[2]), ", ")
			}
			continue
		}
		if string(line) == `\.` {
			d.at.Table, d.at.Columns = "", nil
			continue
		}
		if !read(d.at.Table) {
			continue
		}
		fields := bytes.Split(line, []byte("\t"))
		if len(fields) != len(d.at.Columns) {
			return d.at.Table, nil, fmt.Errorf("%w: %d fields, want %d", errBadRow, len(fields), len(d.at.Columns))
		}
		row = make(map[string]string, len(fields))
		for i, f := range fields {
			if string(f) != `\N` {
				row[d.at.Columns[i]] = unescape(f)
			}
		}
		return d.at.Table, row, nil
	}
}

// escapes are the characters pg_dump escapes besides the backslash.
var escapes = map[byte]byte{'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t', 'v': '\v'}

// unescape undoes COPY text escaping.
func unescape(b []byte) string {
	if bytes.IndexByte(b, '\\') < 0 {
		return string(b)
	}
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] != '\\' || i+1 == len(b) {
			out = append(out, b[i])
			continue
		}
		i++
		if c, ok := escapes[b[i]]; ok {
			out = append(out, c)
		} else {
			out = append(out, b[i])
		}
	}
	return string(out)
}

func read(table string) bool { _, ok := sources[table]; _, p := parents[table]; return ok || p }

var sources = map[string]struct{}{"booru_character_def": {}, "chub_character_def": {}, "chub_lorebook_def": {},
	"generic_character_def": {}, "nyaime_character_def": {}, "risuai_character_def": {}, "webring_character_def": {}}
var parents = map[string]string{"chub_character": "chub_character_def", "chub_lorebook": "chub_lorebook_def"}

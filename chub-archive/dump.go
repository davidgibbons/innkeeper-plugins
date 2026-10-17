package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// errBadRow marks a dump row the plugin can't read. The import skips it.
var errBadRow = errors.New("bad row")

// checkpoint is where the import is in the dump: the byte offset of the
// next line, and the COPY block it is in, if any. Size is the dump's, so a
// checkpoint isn't used on another dump. Parents is the offset of chub's
// per-item block once read, for a resumed run to read it again.
type checkpoint struct {
	Table   string   `json:"table,omitempty"`
	Columns []string `json:"columns,omitempty"`
	Offset  int64    `json:"offset"`
	Size    int64    `json:"size,omitempty"`
	Parents int64    `json:"parents,omitempty"`
}

// dump reads the rows of a pg_dump plain-text file's COPY blocks.
type dump struct {
	f     *os.File
	r     *bufio.Reader
	at    checkpoint
	lines int64 // read since open, to check for a cancel now and then
}

var copyHeader = regexp.MustCompile(`^COPY public\.(\w+) \((.*)\) FROM stdin;$`)

// errDumpChanged means a checkpoint was taken on a dump of another size.
var errDumpChanged = errors.New("the dump changed since the checkpoint")

// openDump opens the dump at path, positioned at at. It refuses a checkpoint
// taken on a dump of another size.
func openDump(path string, at checkpoint) (*dump, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil && at.Size != 0 && at.Size != info.Size() {
		err = fmt.Errorf("%w: it was %d bytes, and is %d", errDumpChanged, at.Size, info.Size())
	}
	if err == nil {
		_, err = f.Seek(at.Offset, io.SeekStart)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	at.Size = info.Size()
	return &dump{f: f, r: bufio.NewReaderSize(f, 1<<20), at: at}, nil
}

func (d *dump) Close() error { return d.f.Close() }

// next returns the next row of a table the import reads, as column to value. A
// NULL column is absent. It returns io.EOF after the last row, and errBadRow
// for a row with the wrong number of fields.
func (d *dump) next(ctx context.Context) (table string, row map[string]string, err error) {
	for {
		// Skipped tables can run to millions of lines without a row to return.
		if d.lines%1000 == 0 {
			if err := ctx.Err(); err != nil {
				return "", nil, err
			}
		}
		line, err := d.r.ReadBytes('\n')
		if errors.Is(err, io.EOF) && len(line) > 0 {
			err = nil
		}
		if err != nil {
			return "", nil, err
		}
		d.lines++
		start := d.at.Offset
		d.at.Offset += int64(len(line))
		line = bytes.TrimSuffix(line, []byte("\n"))
		if d.at.Table == "" {
			if m := copyHeader.FindSubmatch(line); m != nil {
				d.at.Table, d.at.Columns = string(m[1]), strings.Split(string(m[2]), ", ")
				if d.at.Table == parentTable {
					d.at.Parents = start
				}
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

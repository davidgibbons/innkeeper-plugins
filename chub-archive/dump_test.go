package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestUnescape(t *testing.T) {
	cases := map[string]string{
		`plain`:           "plain",
		`a\\b`:            `a\b`,
		`say \\"hi\\"`:    `say \"hi\"`,
		`line\nnext\tcol`: "line\nnext\tcol",
		`\b\f\r\v`:        "\b\f\r\v",
		`\q`:              "q",
		`trailing\`:       `trailing\`,
	}
	for in, want := range cases {
		if got := unescape([]byte(in)); got != want {
			t.Errorf("unescape(%q) = %q, want %q", in, got, want)
		}
	}
}

// readAll returns table:name for every row, and the position after each.
func readAll(t *testing.T, d *dump) (rows []string, after []checkpoint) {
	t.Helper()
	for {
		table, row, err := d.next(context.Background())
		if errors.Is(err, io.EOF) {
			return rows, after
		}
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, table+":"+row["name"]+row["author"])
		after = append(after, d.at)
	}
}

func TestDumpRows(t *testing.T) {
	d, err := openDump("testdata/dump.sql", checkpoint{})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	rows, after := readAll(t, d)
	// Skipped tables don't appear; parents come before their defs.
	if len(rows) != 12 || rows[0] != "booru_character_def:Coracat" || rows[1] != "chub_character:Ariaann" {
		t.Fatalf("rows = %v", rows)
	}
	if d.at.Offset != d.size || d.at.Table != "" {
		t.Fatalf("ended at %+v of %d", d.at, d.size)
	}

	// Resuming after Aria's first version gives the same remaining rows.
	i := 2 // chub_character_def's first row
	if after[i].Table != "chub_character_def" {
		t.Fatalf("row %d is in %q", i, after[i].Table)
	}
	r, err := openDump("testdata/dump.sql", after[i])
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	rest, _ := readAll(t, r)
	if len(rest) != len(rows)-i-1 || rest[0] != rows[i+1] {
		t.Fatalf("resumed rows = %v, want %v", rest, rows[i+1:])
	}
}

func TestDumpRowValues(t *testing.T) {
	d, err := openDump("testdata/dump.sql", checkpoint{})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for {
		table, row, err := d.next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if table == "chub_character_def" && row["increment"] == "9" {
			if want := `"An elf ranger who says \"hi\".\nShe guards\tthe woods."`; !strings.Contains(row["definition"], want) {
				t.Fatalf("definition = %s, want it to hold %s", row["definition"], want)
			}
			return
		}
		if table == "booru_character_def" {
			if _, ok := row["tagline"]; ok {
				t.Fatal("a NULL tagline should be absent")
			}
		}
	}
}

func TestDumpSeesCancel(t *testing.T) {
	d, err := openDump("testdata/dump.sql", checkpoint{})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := d.next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

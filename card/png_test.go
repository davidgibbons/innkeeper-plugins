package card

import (
	"bytes"
	"testing"
)

func TestChunksRoundTrip(t *testing.T) {
	img, err := placeholder()
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := readChunks(img)
	if err != nil {
		t.Fatal(err)
	}
	if chunks[0].typ != "IHDR" || chunks[len(chunks)-1].typ != "IEND" {
		t.Fatalf("chunks = %v", chunks)
	}
	if !bytes.Equal(writeChunks(chunks), img) {
		t.Fatal("writing the chunks back changed the PNG")
	}
	if _, err := readChunks(img[:len(img)-5]); err == nil {
		t.Fatal("no error for a truncated PNG")
	}
}

func TestTextChunk(t *testing.T) {
	k, text, ok := textChunk("chara", []byte("abc")).text()
	if !ok || k != "chara" || string(text) != "abc" {
		t.Fatalf("got %q %q %v", k, text, ok)
	}
	if _, _, ok := (chunk{typ: "IDAT"}).text(); ok {
		t.Fatal("IDAT read as text")
	}
}

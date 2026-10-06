package card

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
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

func TestReadChunksRejects(t *testing.T) {
	img, err := placeholder()
	if err != nil {
		t.Fatal(err)
	}
	huge := append([]byte(nil), img...)
	copy(huge[8:12], []byte{0xff, 0xff, 0xff, 0xff})
	for name, in := range map[string][]byte{
		"cut inside IDAT": img[:len(img)/2],
		"no IEND":         img[:len(img)-12],
		"huge length":     huge,
	} {
		if _, err := readChunks(in); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestIsPlaceholder(t *testing.T) {
	img, err := placeholder()
	if err != nil {
		t.Fatal(err)
	}
	gray := func(r image.Rectangle, mark bool) []byte {
		m := image.NewRGBA(r)
		draw.Draw(m, r, image.NewUniform(color.Gray{Y: placeholderGray}), image.Point{}, draw.Src)
		if mark {
			m.Set(10, 10, color.Black)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, m); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	for _, c := range []struct {
		name string
		img  []byte
		want bool
	}{
		{"the placeholder", img, true},
		{"the placeholder re-saved as RGBA, as apps do", gray(placeholderBounds, false), true},
		{"the placeholder with one pixel drawn on", gray(placeholderBounds, true), false},
		{"the same gray at 800x1200", gray(image.Rect(0, 0, 800, 1200), false), false},
		{"not an image", []byte("x"), false},
	} {
		if got := IsPlaceholder(c.img); got != c.want {
			t.Errorf("IsPlaceholder(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

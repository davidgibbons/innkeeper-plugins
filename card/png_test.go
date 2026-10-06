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
	// Apps re-save it as RGBA.
	rgba := image.NewRGBA(placeholderBounds)
	draw.Draw(rgba, rgba.Bounds(), image.NewUniform(color.Gray{Y: placeholderGray}), image.Point{}, draw.Src)
	var resaved bytes.Buffer
	if err := png.Encode(&resaved, rgba); err != nil {
		t.Fatal(err)
	}
	rgba.Set(10, 10, color.Black)
	var drawn bytes.Buffer
	if err := png.Encode(&drawn, rgba); err != nil {
		t.Fatal(err)
	}
	if !IsPlaceholder(img) || !IsPlaceholder(resaved.Bytes()) || IsPlaceholder(drawn.Bytes()) || IsPlaceholder([]byte("x")) {
		t.Fatal("IsPlaceholder is wrong")
	}
}

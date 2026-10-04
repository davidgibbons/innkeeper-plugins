package card

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
)

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// chunk is one PNG chunk, without its length and CRC.
type chunk struct {
	typ  string
	data []byte
}

func isPNG(b []byte) bool { return bytes.HasPrefix(b, pngSignature) }

// readChunks splits a PNG into its chunks, up to IEND. It doesn't check CRCs.
func readChunks(b []byte) ([]chunk, error) {
	if !isPNG(b) {
		return nil, errors.New("not a PNG")
	}
	var out []chunk
	for rest := b[len(pngSignature):]; ; {
		if len(rest) < 12 {
			return nil, errors.New("truncated PNG")
		}
		n := binary.BigEndian.Uint32(rest)
		if uint64(n) > uint64(len(rest)-12) {
			return nil, errors.New("truncated PNG")
		}
		c := chunk{typ: string(rest[4:8]), data: rest[8 : 8+n]}
		out = append(out, c)
		if c.typ == "IEND" {
			return out, nil
		}
		rest = rest[12+n:]
	}
}

// writeChunks joins chunks into a PNG.
func writeChunks(chunks []chunk) []byte {
	var buf bytes.Buffer
	buf.Write(pngSignature)
	for _, c := range chunks {
		buf.Write(binary.BigEndian.AppendUint32(nil, uint32(len(c.data))))
		buf.WriteString(c.typ)
		buf.Write(c.data)
		crc := crc32.NewIEEE()
		crc.Write([]byte(c.typ))
		crc.Write(c.data)
		buf.Write(binary.BigEndian.AppendUint32(nil, crc.Sum32()))
	}
	return buf.Bytes()
}

// text returns a tEXt chunk's keyword and text. ok is false for other chunks.
func (c chunk) text() (keyword string, text []byte, ok bool) {
	if c.typ != "tEXt" {
		return "", nil, false
	}
	k, t, found := bytes.Cut(c.data, []byte{0})
	return string(k), t, found
}

func textChunk(keyword string, text []byte) chunk {
	return chunk{typ: "tEXt", data: append(append([]byte(keyword), 0), text...)}
}

// placeholder is the image for a PNG card without an avatar: plain gray, in
// the 2:3 shape card apps show.
func placeholder() ([]byte, error) {
	img := image.NewGray(image.Rect(0, 0, 400, 600))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	return buf.Bytes(), err
}

// Command card-codec is a codec plugin for character cards: CCv3, V2, and V1
// as PNG and JSON.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"

	"github.com/davidgibbons/innkeeper-plugins/card"
	"github.com/davidgibbons/innkeeper/protocol"
)

const version = "0.1.0"

// maxInput caps the files the plugin reads. Card PNGs are a few MB at most.
const maxInput = 32 << 20

func main() {
	// Set by initialize; requests run on other goroutines.
	var blobTmp atomic.Value
	handler := func(_ context.Context, method string, params json.RawMessage) (any, error) {
		tmp, _ := blobTmp.Load().(string)
		switch method {
		case protocol.MethodInitialize:
			var p protocol.InitializeParams
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, invalid(err)
			}
			blobTmp.Store(p.BlobTmp)
			return protocol.InitializeResult{Protocol: protocol.Version, Name: "card-codec", Version: version,
				Capabilities: []string{"codec"}}, nil
		case protocol.MethodShutdown:
			return nil, nil
		case protocol.MethodCodecDecode:
			return decode(tmp, params)
		case protocol.MethodCodecEncode:
			return encode(tmp, params)
		}
		return nil, protocol.MethodNotFound(method)
	}
	conn := protocol.NewConn(os.Stdin, os.Stdout, handler)
	if err := conn.Run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func decode(tmp string, params json.RawMessage) (any, error) {
	var p protocol.CodecDecodeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, invalid(err)
	}
	b, err := readFile(p.Path)
	if err != nil {
		return nil, invalid(err)
	}
	c, img, err := card.Decode(b)
	if err != nil {
		return nil, invalid(err)
	}
	res := protocol.CodecDecodeResult{Card: c}
	if img != nil {
		if res.Avatar, err = writeTmp(tmp, "avatar-*.png", img); err != nil {
			return nil, err
		}
	}
	return res, nil
}

func encode(tmp string, params json.RawMessage) (any, error) {
	var p protocol.CodecEncodeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, invalid(err)
	}
	var avatar []byte
	if p.Avatar != "" {
		var err error
		if avatar, err = readFile(p.Avatar); err != nil {
			return nil, err
		}
	}
	out, ext, err := card.Encode(p.Card, p.Format, avatar)
	if err != nil {
		return nil, invalid(err)
	}
	path, err := writeTmp(tmp, "card-*."+ext, out)
	if err != nil {
		return nil, err
	}
	return protocol.CodecEncodeResult{Path: path, Extension: ext}, nil
}

func readFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxInput+1))
	if err == nil && len(b) > maxInput {
		err = fmt.Errorf("%s is over %d MiB", path, maxInput>>20)
	}
	return b, err
}

// writeTmp writes b to a new file in tmp and returns its path.
func writeTmp(tmp, pattern string, b []byte) (string, error) {
	if tmp == "" {
		return "", errors.New("initialize sent no blob_tmp")
	}
	f, err := os.CreateTemp(tmp, pattern)
	if err != nil {
		return "", err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func invalid(err error) error {
	return protocol.NewError(protocol.CodeInvalidParams, err.Error(), false)
}

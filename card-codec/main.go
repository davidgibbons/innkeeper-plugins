// Command card-codec is a codec plugin for character cards: CCv3, V2, and V1
// as PNG and JSON. It also reads CharX.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/davidgibbons/innkeeper-plugins/card"
	"github.com/davidgibbons/innkeeper-plugins/internal/pluginio"
	"github.com/davidgibbons/innkeeper/protocol"
)

const version = "0.2.0"

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
	b, err := pluginio.ReadFile(p.Path)
	if err != nil {
		return nil, invalid(err)
	}
	c, img, err := card.Decode(b)
	if err != nil {
		return nil, invalid(err)
	}
	res := protocol.CodecDecodeResult{Card: c}
	if img != nil {
		if res.Avatar, err = pluginio.WriteTmp(tmp, "avatar-*.png", img); err != nil {
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
		if avatar, err = pluginio.ReadFile(p.Avatar); err != nil {
			return nil, err
		}
	}
	out, ext, err := card.Encode(p.Card, p.Format, avatar)
	if err != nil {
		return nil, invalid(err)
	}
	path, err := pluginio.WriteTmp(tmp, "card-*."+ext, out)
	if err != nil {
		return nil, err
	}
	return protocol.CodecEncodeResult{Path: path, Extension: ext}, nil
}

func invalid(err error) error {
	return protocol.NewError(protocol.CodeInvalidParams, err.Error(), false)
}

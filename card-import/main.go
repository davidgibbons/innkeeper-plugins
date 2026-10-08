// Command card-import is an import plugin. It imports character cards and
// lorebooks from a folder, or from one file uploaded to the core.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/davidgibbons/innkeeper/protocol"
)

const version = "0.2.0"

func main() {
	// Set by initialize; requests run on other goroutines.
	var cur atomic.Pointer[env]
	var conn *protocol.Conn
	handler := func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		switch method {
		case protocol.MethodInitialize:
			var p protocol.InitializeParams
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, invalid(err)
			}
			folder, _ := p.Config["folder"].(string)
			cur.Store(&env{folder: folder, blobTmp: p.BlobTmp, library: p.BlobStores["library"]})
			return protocol.InitializeResult{Protocol: protocol.Version, Name: "card-import", Version: version,
				Capabilities: []string{"import"}}, nil
		case protocol.MethodShutdown:
			return nil, nil
		case protocol.MethodImportRun:
			e := cur.Load()
			if e == nil {
				return nil, errors.New("import.run before initialize")
			}
			return run(ctx, *e, conn.Notify, params)
		}
		return nil, protocol.MethodNotFound(method)
	}
	conn = protocol.NewConn(os.Stdin, os.Stdout, handler)
	if err := conn.Run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func invalid(err error) error {
	return protocol.NewError(protocol.CodeInvalidParams, err.Error(), false)
}

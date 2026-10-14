// Command url-import is an import plugin. It imports one card or lorebook
// from its page on a public site, such as chub.ai.
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

const version = "0.1.1"

func main() {
	// Set by initialize; requests run on other goroutines.
	var blobTmp atomic.Pointer[string]
	var conn *protocol.Conn
	f := newFetcher()
	handler := func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		switch method {
		case protocol.MethodInitialize:
			var p protocol.InitializeParams
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, invalid(err)
			}
			blobTmp.Store(&p.BlobTmp)
			return protocol.InitializeResult{Protocol: protocol.Version, Name: "url-import", Version: version,
				Capabilities: []string{"import"}}, nil
		case protocol.MethodShutdown:
			return nil, nil
		case protocol.MethodImportRun:
			tmp := blobTmp.Load()
			if tmp == nil {
				return nil, errors.New("import.run before initialize")
			}
			return run(ctx, f, *tmp, conn.Notify, params)
		}
		return nil, protocol.MethodNotFound(method)
	}
	conn = protocol.NewConn(os.Stdin, os.Stdout, handler)
	if err := conn.Run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// Command sillytavern is a target plugin for SillyTavern
// (github.com/SillyTavern/SillyTavern). It pushes cards as characters and
// lorebooks as world info files, and reads both back.
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

const version = "0.1.0"

type plugin struct{ c *client }

// describe says what SillyTavern stores. It needs no server.
func describe() protocol.TargetInfo {
	return protocol.TargetInfo{
		Fields: cardFields,
		Pull:   true, Update: true, Lorebooks: protocol.LorebooksStandalone,
		LorebookFields: []string{"name", "description", "scan_depth", "token_budget", "recursive_scanning",
			"extensions", "entries"},
	}
}

func main() {
	// Set by initialize; requests run on other goroutines.
	var cur atomic.Pointer[plugin]
	handler := func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		switch method {
		case protocol.MethodInitialize:
			var p protocol.InitializeParams
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, invalid(err)
			}
			url, _ := p.Config["url"].(string)
			auth, _ := p.Config["auth"].(string)
			username, _ := p.Config["username"].(string)
			cur.Store(&plugin{newClient(url, auth, username, p.Secrets["password"])})
			return protocol.InitializeResult{Protocol: protocol.Version, Name: "sillytavern", Version: version,
				Capabilities: []string{"push", "pull"}}, nil
		case protocol.MethodShutdown:
			return nil, nil
		case protocol.MethodTargetDescribe:
			return describe(), nil
		}
		p := cur.Load()
		if p == nil {
			return nil, errors.New(method + " before initialize")
		}
		switch method {
		case protocol.MethodTargetPut:
			return p.put(ctx, params)
		case protocol.MethodTargetGet:
			return p.get(ctx, params)
		case protocol.MethodTargetPutLorebook:
			return p.putLorebook(ctx, params)
		case protocol.MethodTargetGetLorebook:
			return p.getLorebook(ctx, params)
		case protocol.MethodTargetList:
			return p.list(ctx)
		case protocol.MethodTargetListLorebooks:
			return p.listLorebooks(ctx)
		}
		return nil, protocol.MethodNotFound(method)
	}
	conn := protocol.NewConn(os.Stdin, os.Stdout, handler)
	if err := conn.Run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

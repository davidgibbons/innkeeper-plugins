package catalogkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/inkandale/innkeeper/protocol"
)

// Plugin is a catalog plugin for Serve to run. Serve answers the protocol
// from the Store; the plugin sets up each instance and answers catalog.get
// and catalog.ingest.
type Plugin[E any] struct {
	Name, Version string
	Config        Config
	// Open sets up an instance from its initialize params. store is nil
	// without a database_url.
	Open   func(p protocol.InitializeParams, store *Store) (E, error)
	Get    func(ctx context.Context, e E, params json.RawMessage) (protocol.CatalogGetResult, error)
	Ingest func(ctx context.Context, e E, notify func(string, any) error, params json.RawMessage) (protocol.CatalogIngestResult, error)
}

var errNoDatabase = errors.New("no database: the core sends database_url to a plugin whose manifest sets database = true")

// Serve runs p over stdin and stdout until the core disconnects.
func Serve[E any](p Plugin[E]) error {
	type instance struct {
		store *Store
		env   E
	}
	// Set by initialize; requests run on other goroutines.
	var cur atomic.Pointer[instance]
	var conn *protocol.Conn
	handler := func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		switch method {
		case protocol.MethodInitialize:
			var ip protocol.InitializeParams
			if err := json.Unmarshal(params, &ip); err != nil {
				return nil, invalid(err)
			}
			in := &instance{}
			if ip.DatabaseURL != "" {
				var err error
				if in.store, err = Open(ctx, ip.DatabaseURL, ip.InstanceID, p.Config); err != nil {
					return nil, err
				}
			}
			var err error
			if in.env, err = p.Open(ip, in.store); err != nil {
				if in.store != nil {
					in.store.Close()
				}
				return nil, err
			}
			cur.Store(in)
			return protocol.InitializeResult{Protocol: protocol.Version, Name: p.Name, Version: p.Version,
				Capabilities: []string{"catalog"}}, nil
		case protocol.MethodShutdown:
			if in := cur.Load(); in != nil && in.store != nil {
				in.store.Close()
			}
			return nil, nil
		case protocol.MethodCatalogDescribe:
			return Describe(p.Config), nil
		case protocol.MethodCatalogSearch, protocol.MethodCatalogGet, protocol.MethodCatalogSimilar, protocol.MethodCatalogIngest:
		default:
			return nil, protocol.MethodNotFound(method)
		}
		in := cur.Load()
		if in == nil {
			return nil, fmt.Errorf("%s before initialize", method)
		}
		// Search checks its params first, so plugin check's filter check
		// passes without a database too.
		if method == protocol.MethodCatalogSearch {
			var sp protocol.CatalogSearchParams
			if err := json.Unmarshal(params, &sp); err != nil {
				return nil, invalid(err)
			}
			if err := Describe(p.Config).CheckSearch(sp); err != nil {
				return nil, invalid(err)
			}
			if in.store == nil {
				return nil, errNoDatabase
			}
			return in.store.Search(ctx, sp)
		}
		if in.store == nil {
			return nil, errNoDatabase
		}
		switch method {
		case protocol.MethodCatalogGet:
			return p.Get(ctx, in.env, params)
		case protocol.MethodCatalogSimilar:
			var sp protocol.CatalogSimilarParams
			if err := json.Unmarshal(params, &sp); err != nil {
				return nil, invalid(err)
			}
			return in.store.Similar(ctx, sp)
		}
		return p.Ingest(ctx, in.env, conn.Notify, params)
	}
	conn = protocol.NewConn(os.Stdin, os.Stdout, handler)
	return conn.Run(context.Background())
}

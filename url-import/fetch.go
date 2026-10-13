package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/davidgibbons/innkeeper-plugins/internal/pluginio"
	"github.com/davidgibbons/innkeeper/protocol"
)

// maxBody caps each response. RisuRealm's PNGs with embedded assets run past 10 MiB.
const maxBody = pluginio.MaxInput

// fetcher makes the plugin's requests to sites.
type fetcher struct{ http *http.Client }

func newFetcher() *fetcher { return &fetcher{http: &http.Client{Timeout: 30 * time.Second}} }

// get fetches url. site names the site in errors. A 429, a 5xx, or a network
// error is retryable, so the core re-queues the job.
func (f *fetcher) get(ctx context.Context, site, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// chub.ai blocks curl/ agents, and JanitorAI blocks Go's default.
	req.Header.Set("User-Agent", "innkeeper-url-import/"+version)
	resp, err := f.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, retryable(fmt.Errorf("%s: %w", site, err))
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, permanent(fmt.Errorf("not found on %s", site))
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return nil, retryable(fmt.Errorf("%s answered %s", site, resp.Status))
	case resp.StatusCode != http.StatusOK:
		return nil, permanent(fmt.Errorf("%s answered %s", site, resp.Status))
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, retryable(fmt.Errorf("%s: %w", site, err))
	}
	if len(b) > maxBody {
		return nil, permanent(fmt.Errorf("%s sent over %d MiB", site, maxBody>>20))
	}
	return b, nil
}

// getJSON fetches url and decodes it into v.
func (f *fetcher) getJSON(ctx context.Context, site, url string, v any) error {
	b, err := f.get(ctx, site, url)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return permanent(fmt.Errorf("%s sent unreadable JSON: %w", site, err))
	}
	return nil
}

func invalid(err error) error {
	return protocol.NewError(protocol.CodeInvalidParams, err.Error(), false)
}

func retryable(err error) error {
	return protocol.NewError(protocol.CodeInternalError, err.Error(), true)
}

func permanent(err error) error {
	return protocol.NewError(protocol.CodeInternalError, err.Error(), false)
}

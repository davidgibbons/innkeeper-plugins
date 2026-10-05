package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"

	"github.com/davidgibbons/innkeeper/protocol"
)

// client calls one SillyTavern server's API as one user.
type client struct {
	base, auth         string // auth is "none", "basic", or "account"
	username, password string

	mu   sync.Mutex
	sess *session
	// failed is set by a rejected login. SillyTavern allows five logins per IP
	// a minute, so the plugin doesn't try again until it restarts, which a
	// change to the instance's config or secrets does.
	failed error
}

// session is one cookie jar and the CSRF token that goes with it.
type session struct {
	http *http.Client
	csrf string
}

func newClient(base, auth, username, password string) *client {
	return &client{base: strings.TrimRight(base, "/"), auth: auth, username: username, password: password}
}

// request is one API call. body is sent as is with contentType.
type request struct {
	method, path string
	body         []byte
	contentType  string
}

// call POSTs a JSON body to path and decodes the reply into out.
func (c *client) call(ctx context.Context, path string, in, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return c.do(ctx, request{method: http.MethodPost, path: path, body: raw, contentType: "application/json"}, out)
}

// upload POSTs a multipart form to path: fields, and file as the field
// "avatar", the only name SillyTavern's upload handler reads.
func (c *client) upload(ctx context.Context, path string, fields map[string]string, file []byte, out any) error {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			return err
		}
	}
	part, err := mw.CreateFormFile("avatar", "avatar.png")
	if err != nil {
		return err
	}
	part.Write(file)
	if err := mw.Close(); err != nil {
		return err
	}
	return c.do(ctx, request{method: http.MethodPost, path: path, body: buf.Bytes(), contentType: mw.FormDataContentType()}, out)
}

// do sends r with a session. A 403 may mean the session has ended, so it logs
// in again once; a 403 on a session made for this call is final.
func (c *client) do(ctx context.Context, r request, out any) error {
	sess, fresh, err := c.session(ctx, nil)
	if err != nil {
		return err
	}
	resp, err := c.send(ctx, sess, r)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusForbidden && !fresh {
		resp.Body.Close()
		if sess, _, err = c.session(ctx, sess); err != nil {
			return err
		}
		if resp, err = c.send(ctx, sess, r); err != nil {
			return err
		}
	}
	defer resp.Body.Close()
	what := r.method + " " + r.path
	if resp.StatusCode == http.StatusForbidden {
		return protocol.NewError(-32000, what+": SillyTavern answered 403 on a fresh login. Either its whitelist "+
			"doesn't include this server's address, it refused the CSRF token, or the account is disabled", false)
	}
	return decode(resp, what, out)
}

func (c *client) send(ctx context.Context, s *session, r request) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, r.method, c.base+r.path, bytes.NewReader(r.body))
	if err != nil {
		return nil, err
	}
	if r.contentType != "" {
		req.Header.Set("Content-Type", r.contentType)
	}
	if c.auth == "basic" {
		req.SetBasicAuth(c.username, c.password)
	}
	if r.method == http.MethodPost && s.csrf != "" {
		req.Header.Set("X-CSRF-Token", s.csrf)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, protocol.NewError(-32000, fmt.Sprintf("%s %s: %v", r.method, r.path, err), true)
	}
	return resp, nil
}

// decode maps a reply's status to a protocol error, or decodes its body.
func decode(resp *http.Response, what string, out any) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return protocol.NewError(-32000, what+": "+err.Error(), true)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return protocol.NewError(protocol.CodeRemoteNotFound, what+": not found", false)
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests:
		return protocol.NewError(-32000, fmt.Sprintf("%s: %s: %s", what, resp.Status, snippet(body)), true)
	case resp.StatusCode >= 300:
		return protocol.NewError(-32000, fmt.Sprintf("%s: %s: %s", what, resp.Status, snippet(body)), false)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return protocol.NewError(-32000, what+": bad reply: "+err.Error(), false)
	}
	return nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// session returns the current session, logging in if there is none or if it
// is stale. fresh reports that this call made it.
func (c *client) session(ctx context.Context, stale *session) (s *session, fresh bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failed != nil {
		return nil, false, c.failed
	}
	if c.sess != nil && c.sess != stale {
		return c.sess, false, nil
	}
	if c.base == "" || c.auth != "none" && (c.username == "" || c.password == "") {
		return nil, false, protocol.NewError(-32000, "set the instance's url and username config and its password secret", false)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, false, err
	}
	s = &session{http: &http.Client{Jar: jar, Timeout: time.Minute}}
	if err := c.login(ctx, s); err != nil {
		var pe *protocol.Error
		if errors.As(err, &pe) && pe.Retryable() {
			return nil, false, err
		}
		c.failed = protocol.NewError(-32000, "SillyTavern rejected the login; fix the username or password, "+
			"or add this server's address to SillyTavern's whitelist, which restarts the plugin. "+err.Error(), false)
		return nil, false, c.failed
	}
	c.sess = s
	return s, true, nil
}

// login makes s a session SillyTavern accepts. For basic auth, GET /login
// binds the session to the account when accounts are on. The CSRF token comes
// first for an account login, which is itself a POST.
func (c *client) login(ctx context.Context, s *session) error {
	get := func(path string, out any) error {
		resp, err := c.send(ctx, s, request{method: http.MethodGet, path: path})
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		return decode(resp, "login: GET "+path, out)
	}
	if c.auth == "basic" {
		if err := get("/login", nil); err != nil {
			return err
		}
	}
	var tok struct {
		Token string `json:"token"`
	}
	if err := get("/csrf-token", &tok); err != nil {
		return err
	}
	s.csrf = tok.Token
	if c.auth != "account" {
		return nil
	}
	raw, _ := json.Marshal(map[string]string{"handle": c.username, "password": c.password})
	resp, err := c.send(ctx, s, request{method: http.MethodPost, path: "/api/users/login", body: raw, contentType: "application/json"})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decode(resp, "login: POST /api/users/login", nil)
}

func notFound(err error) bool {
	var pe *protocol.Error
	return errors.As(err, &pe) && pe.Code == protocol.CodeRemoteNotFound
}

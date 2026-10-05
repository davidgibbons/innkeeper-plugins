package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/davidgibbons/innkeeper/protocol"
)

// newTestClient connects to f in the given mode, with the credentials f expects.
func newTestClient(f *fake, auth string) *client {
	switch auth {
	case "basic":
		f.basicUser, f.basicPass = "owner", fakePassword
	case "account":
		f.accounts = true
	}
	return newClient(f.srv.URL+"/", auth, "owner", fakePassword)
}

func list(c *client) error {
	return c.call(context.Background(), "/api/worldinfo/list", map[string]any{}, nil)
}

func final(t *testing.T, err error) *protocol.Error {
	t.Helper()
	var pe *protocol.Error
	if !errors.As(err, &pe) || pe.Retryable() {
		t.Fatalf("got %v, want a final error", err)
	}
	return pe
}

func retryable(t *testing.T, err error) {
	t.Helper()
	var pe *protocol.Error
	if !errors.As(err, &pe) || !pe.Retryable() {
		t.Fatalf("got %v, want a retryable error", err)
	}
}

func TestLogsInOnceAndReusesTheSession(t *testing.T) {
	for _, auth := range []string{"basic", "account"} {
		t.Run(auth, func(t *testing.T) {
			f := newFake(t)
			f.accounts = true // with accounts on, basic auth needs GET /login to pick the account
			c := newTestClient(f, auth)
			for range 3 {
				if err := list(c); err != nil {
					t.Fatal(err)
				}
			}
			if f.logins != 1 || f.csrfTokens != 1 {
				t.Fatalf("%d logins and %d CSRF tokens, want 1 of each", f.logins, f.csrfTokens)
			}
		})
	}
}

func TestAuthNoneNeedsNoLogin(t *testing.T) {
	f := newFake(t)
	if err := list(newClient(f.srv.URL, "none", "", "")); err != nil || f.logins != 0 {
		t.Fatalf("%v, %d logins", err, f.logins)
	}
}

func TestSendsTheCSRFToken(t *testing.T) {
	f := newFake(t)
	c := newTestClient(f, "account")
	if err := list(c); err != nil {
		t.Fatal(err)
	}
	if f.lastCSRF == "" || f.lastCSRF == "disabled" {
		t.Fatalf("X-CSRF-Token = %q, want the server's token", f.lastCSRF)
	}
}

func TestSendsADisabledToken(t *testing.T) {
	f := newFake(t)
	f.csrfDisabled = true
	if err := list(newTestClient(f, "account")); err != nil || f.lastCSRF != "disabled" {
		t.Fatalf("%v, X-CSRF-Token = %q", err, f.lastCSRF)
	}
}

func TestGetsANewSessionOnceAfterA403(t *testing.T) {
	f := newFake(t)
	c := newTestClient(f, "account")
	if err := list(c); err != nil {
		t.Fatal(err)
	}
	f.endSessions()
	if err := list(c); err != nil || f.logins != 2 {
		t.Fatalf("after the session ended: %v, %d logins", err, f.logins)
	}
}

func TestASecond403IsFinalAndNamesTheWhitelist(t *testing.T) {
	f := newFake(t)
	c := newTestClient(f, "account")
	if err := list(c); err != nil {
		t.Fatal(err)
	}
	f.deny = true
	pe := final(t, list(c))
	if f.logins != 2 || !strings.Contains(pe.Message, "whitelist") {
		t.Fatalf("%d logins, error %q", f.logins, pe.Message)
	}
}

func TestStopsAfterARejectedLogin(t *testing.T) {
	cases := map[string]func(*fake){
		"wrong account password": func(f *fake) { f.password = "other" },
		"wrong basic password":   func(f *fake) { f.basicPass = "other" },
		"address not whitelisted": func(f *fake) {
			f.refuse = true
		},
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFake(t)
			auth := "account"
			if strings.Contains(name, "basic") {
				auth = "basic"
			}
			c := newTestClient(f, auth)
			spoil(f)
			final(t, list(c))
			sent := f.requests
			pe := final(t, list(c))
			if f.requests != sent || f.logins != 0 {
				t.Fatalf("sent %d more requests after the rejection", f.requests-sent)
			}
			if !strings.Contains(pe.Message, "whitelist") {
				t.Fatalf("error %q should name the whitelist", pe.Message)
			}
		})
	}
}

func TestARateLimitedLoginCanBeRetried(t *testing.T) {
	f := newFake(t)
	c := newTestClient(f, "account")
	f.loginAttempts = 5
	retryable(t, list(c))
	f.loginAttempts = 0
	if err := list(c); err != nil {
		t.Fatal(err)
	}
}

func TestErrors(t *testing.T) {
	f := newFake(t)
	c := newTestClient(f, "account")
	for status, want := range map[int]string{429: "retryable", 500: "retryable", 503: "retryable", 400: "final", 413: "final"} {
		f.failWith = status
		if want == "retryable" {
			retryable(t, list(c))
		} else {
			final(t, list(c))
		}
	}
	f.failWith = 0
	err := c.call(context.Background(), "/api/characters/get", map[string]any{"avatar_url": "Nobody.png"}, nil)
	if !notFound(err) {
		t.Fatalf("missing character: %v, want -32004", err)
	}
	if f.logins != 1 {
		t.Fatalf("%d logins, want 1", f.logins)
	}
	f.srv.Close()
	retryable(t, list(c))
}

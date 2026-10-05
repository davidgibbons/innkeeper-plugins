// Command password-login is a login provider that checks one password and
// reports the subject "owner". Run "password-login hash" to make the
// password_hash secret from a password on stdin.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"

	"github.com/davidgibbons/innkeeper/protocol"
	"golang.org/x/crypto/bcrypt"
)

const version = "0.1.0"

// subject is who every login is. The owner sets it as the instance's owner_subject.
const subject = "owner"

// cost is the bcrypt cost for new hashes: about a quarter second each.
const cost = 12

const form = `{"type": "object", "properties": {"password": {"type": "string", "format": "password", "title": "Password"}}, "required": ["password"]}`

func main() {
	if len(os.Args) == 2 && os.Args[1] == "hash" {
		if err := printHash(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	// Set by initialize; requests run on other goroutines.
	var hash atomic.Value
	handler := func(_ context.Context, method string, params json.RawMessage) (any, error) {
		switch method {
		case protocol.MethodInitialize:
			var p protocol.InitializeParams
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, protocol.NewError(protocol.CodeInvalidParams, err.Error(), false)
			}
			hash.Store(p.Secrets["password_hash"])
			return protocol.InitializeResult{Protocol: protocol.Version, Name: "password-login", Version: version,
				Capabilities: []string{"auth"}}, nil
		case protocol.MethodShutdown:
			return nil, nil
		case protocol.MethodAuthBegin:
			return protocol.AuthBeginResult{Form: json.RawMessage(form)}, nil
		case protocol.MethodAuthComplete:
			h, _ := hash.Load().(string)
			return complete(h, params)
		}
		return nil, protocol.MethodNotFound(method)
	}
	conn := protocol.NewConn(os.Stdin, os.Stdout, handler)
	if err := conn.Run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// complete checks the form's password against hash.
func complete(hash string, params json.RawMessage) (any, error) {
	var p protocol.AuthCompleteParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, protocol.NewError(protocol.CodeInvalidParams, err.Error(), false)
	}
	if hash == "" {
		return nil, protocol.NewError(-32000, "the instance has no password_hash secret; make one with: password-login hash", false)
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(p.Params["password"]))
	switch {
	case err == nil:
		return protocol.AuthCompleteResult{Subject: subject, Name: "Owner"}, nil
	case errors.Is(err, bcrypt.ErrMismatchedHashAndPassword):
		return nil, protocol.NewError(-32000, "wrong password", false)
	}
	return nil, protocol.NewError(-32000, "password_hash is not a bcrypt hash: "+err.Error(), false)
}

// printHash reads a password line from r and writes its bcrypt hash to w.
func printHash(r io.Reader, w io.Writer) error {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && err != io.EOF {
		return err
	}
	password := strings.TrimRight(line, "\r\n")
	if password == "" {
		return errors.New("no password on stdin")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(h))
	return err
}

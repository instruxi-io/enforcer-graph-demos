package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// credential supplies the headers that authenticate a request. It never logs
// or returns anything but the headers themselves.
//
// fresh asks for new headers after a 401: an OAuth access token expires, and
// the helper refreshes it (the refresh_token grant) the next time it runs.
type credential interface {
	headers(ctx context.Context, fresh bool) (map[string]string, error)
}

// apiKey is the long-lived X-API-Key from GRAPH_API_KEY.
type apiKey string

func (k apiKey) headers(context.Context, bool) (map[string]string, error) {
	return map[string]string{"X-API-Key": string(k)}, nil
}

// helper runs a command that prints the auth headers as a JSON object, e.g.
// the enforcer plugin's bin/enforcer-headers.mjs, which reads the OAuth
// session /enforcer:login saved in ~/.enforcer/credentials.json and refreshes
// it when it is close to expiry. The same helper Claude Code runs for the
// Enforcer MCP server, so one sign-in covers both. Run through `sh -c` so a
// `~` or quoted path in GRAPH_AUTH_HELPER works as typed.
type helper struct {
	cmd string

	mu     sync.Mutex
	cached map[string]string
}

func (h *helper) headers(ctx context.Context, fresh bool) (map[string]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cached != nil && !fresh {
		return h.cached, nil
	}
	got, err := runHelper(ctx, h.cmd)
	if err != nil {
		return nil, err
	}
	h.cached = got
	return got, nil
}

// errSignedOut is what an empty helper answer means: the enforcer helper
// prints {} when there is no session (or its refresh failed).
var errSignedOut = errors.New("the auth helper returned no headers — signed out? run /enforcer:login")

func runHelper(ctx context.Context, cmd string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	var out, stderr bytes.Buffer
	c.Stdout, c.Stderr = &out, &stderr
	if err := c.Run(); err != nil {
		// stderr only: stdout may hold a token.
		return nil, fmt.Errorf("auth helper failed: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var raw map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &raw); err != nil {
		return nil, errors.New("auth helper did not print a JSON object of headers")
	}
	h := map[string]string{}
	for k, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			h[k] = s
		}
	}
	if len(h) == 0 {
		return nil, errSignedOut
	}
	return h, nil
}

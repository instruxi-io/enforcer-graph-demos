package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// client speaks the graph API with one credential. It never logs it.
type client struct {
	base string // …/api/v1/graph
	cred credential
	http *http.Client
}

func newClient(base string, cred credential) *client {
	return &client{base: strings.TrimRight(base, "/") + "/api/v1/graph", cred: cred,
		http: &http.Client{Timeout: 30 * time.Second}}
}

// authorize sets the credential's headers on req; fresh re-reads them (after a
// 401, so an expired OAuth token is refreshed by the helper).
func (c *client) authorize(ctx context.Context, req *http.Request, fresh bool) error {
	h, err := c.cred.headers(ctx, fresh)
	if err != nil {
		return err
	}
	for k, v := range h {
		req.Header.Set(k, v)
	}
	return nil
}

func (c *client) do(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = b
	}
	// One retry on 401 with fresh headers: an OAuth access token can expire
	// mid-session, and the helper refreshes it. The request was refused, so
	// resending it cannot apply anything twice.
	var resp *http.Response
	for attempt := 0; attempt < 2; attempt++ {
		var rd io.Reader
		if payload != nil {
			rd = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
		if err != nil {
			return err
		}
		if err := c.authorize(ctx, req, attempt > 0); err != nil {
			return &apiError{status: http.StatusUnauthorized, msg: err.Error()}
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err = c.http.Do(req)
		if err != nil {
			return &apiError{msg: fmt.Sprintf("%s %s: %v", method, path, err)}
		}
		if resp.StatusCode != http.StatusUnauthorized || attempt > 0 {
			break
		}
		resp.Body.Close()
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		body := strings.TrimSpace(string(b))
		if len(body) > 200 {
			body = body[:200] + "…"
		}
		return &apiError{status: resp.StatusCode, msg: fmt.Sprintf("%s %s: %d %s", method, path, resp.StatusCode, body)}
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

// apiError carries the status, 0 when no response arrived at all.
type apiError struct {
	status int
	msg    string
}

func (e *apiError) Error() string { return e.msg }

// retry repeats a call that is SAFE to repeat — an upsert, a status set, a
// completion reporting the same outcome — on a network failure or a 5xx.
// Never wrap a claim: a claim whose reply was lost has already taken the node.
func retry(ctx context.Context, fn func() error) error {
	var err error
	for i := 0; i < 6; i++ {
		if err = fn(); err == nil {
			return nil
		}
		var ae *apiError
		if errors.As(err, &ae) && ae.status != 0 && ae.status < 500 {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Duration(400<<i) * time.Millisecond):
		}
	}
	return err
}

type apiGraph struct {
	ID                 string `json:"id"`
	Slug               string `json:"slug"`
	Name               string `json:"name"`
	DependencyEdgeType string `json:"dependency_edge_type"`
}

type apiNode struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type apiEdge struct {
	FromNodeID string `json:"from_node_id"`
	ToNodeID   string `json:"to_node_id"`
	Type       string `json:"type"`
}

func (c *client) graph(ctx context.Context, id string) (apiGraph, error) {
	var r struct{ Data apiGraph }
	err := c.do(ctx, http.MethodGet, "/graphs/"+id, nil, &r)
	return r.Data, err
}

// page walks a list endpoint 200 rows at a time.
func page[T any](ctx context.Context, c *client, path string) ([]T, error) {
	var all []T
	for off := 0; ; off += 200 {
		var r struct {
			Data []T
			Meta struct{ Total int }
		}
		if err := c.do(ctx, http.MethodGet, path+"?limit=200&offset="+strconv.Itoa(off), nil, &r); err != nil {
			return nil, err
		}
		all = append(all, r.Data...)
		if len(r.Data) < 200 || len(all) >= r.Meta.Total {
			return all, nil
		}
	}
}

func (c *client) nodes(ctx context.Context, id string) ([]apiNode, error) {
	return page[apiNode](ctx, c, "/graphs/"+id+"/nodes")
}

func (c *client) edges(ctx context.Context, id string) ([]apiEdge, error) {
	return page[apiEdge](ctx, c, "/graphs/"+id+"/edges")
}

// sseEvent is one frame of docs/STREAM_CONTRACT.md §3.
type sseEvent struct {
	Event, ID string
	Data      json.RawMessage
}

type streamRow struct {
	Seq          int64             `json:"seq"`
	Verb         string            `json:"verb"`
	ResourceType string            `json:"resource_type"`
	ResourceID   *string           `json:"resource_id"`
	Metadata     map[string]string `json:"metadata"`
}

// parseSSE reads frames until the body ends, calling fn for each one. Comments
// (the keepalive) are reported as event ":".
func parseSSE(r io.Reader, fn func(sseEvent)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	var ev sseEvent
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		switch {
		case line == "":
			if ev.Event != "" || len(ev.Data) > 0 {
				fn(ev)
			}
			ev = sseEvent{}
		case strings.HasPrefix(line, ":"):
			fn(sseEvent{Event: ":"})
		case strings.HasPrefix(line, "event:"):
			ev.Event = strings.TrimSpace(line[len("event:"):])
		case strings.HasPrefix(line, "id:"):
			ev.ID = strings.TrimSpace(line[len("id:"):])
		case strings.HasPrefix(line, "data:"):
			ev.Data = json.RawMessage(strings.TrimSpace(line[len("data:"):]))
		}
	}
	return sc.Err()
}

// stream holds the SSE connection open, reconnecting with Last-Event-ID so a
// blip loses nothing (contract §4). status reports live / reconnecting.
func (c *client) stream(ctx context.Context, graphID string, fn func(sseEvent), status func(string)) {
	last := ""
	backoff := time.Second
	refreshed := false
	for ctx.Err() == nil {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/graphs/"+graphID+"/stream", nil)
		if err := c.authorize(ctx, req, refreshed); err != nil {
			status("refused: " + err.Error())
			return
		}
		req.Header.Set("Accept", "text/event-stream")
		if last != "" {
			req.Header.Set("Last-Event-ID", last)
		}
		resp, err := (&http.Client{}).Do(req) // no timeout: the body never ends
		if err == nil && resp.StatusCode == http.StatusOK {
			backoff = time.Second
			refreshed = false
			_ = parseSSE(resp.Body, func(ev sseEvent) {
				if ev.ID != "" {
					last = ev.ID
				}
				fn(ev)
			})
			resp.Body.Close()
		} else if resp != nil {
			resp.Body.Close()
			// A 401 gets one reconnect with fresh headers (an expired OAuth
			// token); a second 401 in a row, or a 404, is final.
			if resp.StatusCode == http.StatusUnauthorized && !refreshed {
				refreshed = true
				continue
			}
			if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized {
				status("refused " + strconv.Itoa(resp.StatusCode))
				return
			}
		}
		if ctx.Err() != nil {
			return
		}
		status("reconnecting")
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 15*time.Second)
	}
}

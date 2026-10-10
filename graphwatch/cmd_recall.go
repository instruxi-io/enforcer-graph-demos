package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func init() {
	register("recall", "search a graph's memory: recall <graph> [--q text] [--node key|id] [--file path] [--kind k]", runRecall)
	commandFlags["recall"] = func(fs *flag.FlagSet) {
		fs.String("q", "", "full-text query over the observations' bodies")
		fs.String("node", "", "only observations about this node (key or id)")
		fs.String("file", "", "only observations that mention this file path")
		fs.String("kind", "", "only observations of this kind")
	}
	register("context", "show a graph's context pack: its hash, size, build time and whether recall is on: context <graph>", runContext)
}

// recallHit is one observation as the recall endpoint returns it. Every field is
// optional on the wire; a missing one prints as a dash.
type recallHit struct {
	Kind      string `json:"kind"`
	NodeKey   string `json:"node_key"`
	NodeID    string `json:"node_id"`
	CreatedAt string `json:"created_at"`
	RunID     string `json:"run_id"`
	Body      string `json:"body"`
}

// graphGet reads path under a graph and maps a 404 to the one-line "no graph"
// answer. The graph is taken as given: a recall is one read, so there is no
// listing to resolve a slug against.
func graphGet(ctx context.Context, env *cliEnv, graph, suffix string, q url.Values) (json.RawMessage, int) {
	path := "/graphs/" + url.PathEscape(graph) + suffix
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	d, err := env.client.graphData(ctx, path)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.status == http.StatusNotFound {
			fmt.Fprintf(env.errOut, "graphwatch: no graph %s\n", graph)
			return nil, exitRuntime
		}
		return nil, reportError(env.errOut, err)
	}
	return d, exitOK
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return dash(s)
}

func runRecall(ctx context.Context, env *cliEnv, args []string) int {
	pos, ok := positionals(env, args)
	if !ok || len(pos) != 1 {
		fmt.Fprintln(env.errOut, "usage: graphwatch recall <graph> [--q text] [--node key|id] [--file path] [--kind k] [--limit n]")
		return exitUsage
	}
	q := url.Values{}
	for _, name := range []string{"q", "node", "file", "kind"} {
		if v := flagVal(env, name); v != "" {
			q.Set(name, v)
		}
	}
	if env.limit > 0 {
		q.Set("limit", strconv.Itoa(env.limit))
	}
	d, code := graphGet(ctx, env, pos[0], "/recall", q)
	if code != exitOK {
		return code
	}
	if env.json || flagVal(env, "json") == "true" {
		fmt.Fprintln(env.out, string(d))
		return exitOK
	}
	// The payload is a list of hits, or an object that carries them under "hits".
	var hits []recallHit
	if err := json.Unmarshal(d, &hits); err != nil {
		var wrapped struct {
			Hits []recallHit `json:"hits"`
		}
		if err := json.Unmarshal(d, &wrapped); err != nil {
			return reportError(env.errOut, err)
		}
		hits = wrapped.Hits
	}
	// The server honours limit; this also bounds a server that does not.
	if env.limit > 0 && len(hits) > env.limit {
		hits = hits[:env.limit]
	}
	if len(hits) == 0 {
		fmt.Fprintln(env.out, "no memory matches")
		return exitOK
	}
	rows := make([][]string, len(hits))
	for i, h := range hits {
		node := h.NodeKey
		if node == "" {
			node = h.NodeID
		}
		rows[i] = []string{dash(h.Kind), dash(node), dash(h.CreatedAt), dash(h.RunID), firstLine(h.Body)}
	}
	env.table([]string{"KIND", "NODE", "CREATED", "RUN", "BODY"}, rows)
	return exitOK
}

// contextInfo is the pack's row on the graph, as GET /graphs/{id}/context returns it.
type contextInfo struct {
	SHA256         string `json:"sha256"`
	Size           int64  `json:"size"`
	BuiltAt        string `json:"built_at"`
	RecallEnabled  bool   `json:"recall_enabled"`
	ContextEnabled *bool  `json:"context_enabled"`
}

func runContext(ctx context.Context, env *cliEnv, args []string) int {
	pos, ok := positionals(env, args)
	if !ok || len(pos) != 1 {
		fmt.Fprintln(env.errOut, "usage: graphwatch context <graph>")
		return exitUsage
	}
	d, code := graphGet(ctx, env, pos[0], "/context", nil)
	if code != exitOK {
		return code
	}
	if env.json || flagVal(env, "json") == "true" {
		fmt.Fprintln(env.out, string(d))
		return exitOK
	}
	var c contextInfo
	if err := json.Unmarshal(d, &c); err != nil {
		return reportError(env.errOut, err)
	}
	if c.SHA256 == "" {
		fmt.Fprintln(env.out, "no context pack built yet")
	} else {
		env.table([]string{"FIELD", "VALUE"}, [][]string{
			{"sha256", c.SHA256},
			{"size", fmt.Sprintf("%d bytes", c.Size)},
			{"built at", dash(c.BuiltAt)},
			{"recall enabled", strconv.FormatBool(c.RecallEnabled)},
		})
	}
	return exitOK
}

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
)

func init() {
	register("inbox", "list everything that needs a person: open review items and ready gates: inbox <graph> [--json]", runInbox)
	register("resolve", "answer a review item (a write): resolve <graph> <item> approve|reject|override [--note text]", runResolve)
	commandFlags["resolve"] = func(fs *flag.FlagSet) {
		fs.String("note", "", "why; required for override")
	}
}

const (
	inboxUsage   = "usage: graphwatch inbox <graph> [--json]"
	resolveUsage = "usage: graphwatch resolve <graph> <item> approve|reject|override [--note text]"
)

// inboxEntry is one line of the inbox, shaped for both the table and --json.
type inboxEntry struct {
	Type    string   `json:"type"` // "review" or "gate"
	ID      string   `json:"id"`   // review item id, or gate node id
	Kind    string   `json:"kind,omitempty"`
	Node    string   `json:"node"`
	Title   string   `json:"title,omitempty"`
	Detail  string   `json:"detail,omitempty"`
	Options []string `json:"options,omitempty"`
	Age     string   `json:"age"`
}

// verdictDetail summarises the vote tally and the weakest criterion when the
// item carries them, and says nothing when it does not.
func verdictDetail(v *apiVerdictRef) string {
	if v == nil {
		return ""
	}
	var parts []string
	if v.Validation != nil && v.Validation.Tally != nil {
		t := v.Validation.Tally
		parts = append(parts, fmt.Sprintf("votes %d+ %d- %d?", t.Accept, t.Reject, t.Undecided))
	}
	if len(v.Criteria) > 0 {
		w := 0
		for i, c := range v.Criteria {
			if c.Noul < v.Criteria[w].Noul {
				w = i
			}
		}
		parts = append(parts, fmt.Sprintf("weakest %.2f %q", v.Criteria[w].Noul, truncate(v.Criteria[w].Text, 24)))
	}
	return strings.Join(parts, "; ")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "..."
}

// collectInbox reads the open review items and the ready gates once. attend
// calls it on every refresh, so the listing and the follower cannot disagree.
func collectInbox(ctx context.Context, c *client, graphID string) ([]inboxEntry, error) {
	items, err := c.reviewItems(ctx, graphID)
	if err != nil {
		return nil, err
	}
	nodes, err := c.nodesFull(ctx, graphID)
	if err != nil {
		return nil, err
	}
	edges, err := c.edges(ctx, graphID)
	if err != nil {
		return nil, err
	}
	keys := map[string]string{}
	for _, n := range nodes {
		keys[n.ID] = n.Key
	}
	var entries []inboxEntry
	for _, it := range items {
		if !isOpenReview(it.State) {
			continue
		}
		key := it.NodeKey
		if key == "" {
			key = keys[it.NodeID]
		}
		detail := verdictDetail(it.Verdict)
		if detail == "" {
			detail = it.Reason
		}
		entries = append(entries, inboxEntry{Type: "review", ID: it.ID, Kind: it.Kind, Node: key, Detail: detail, Age: age(it.CreatedAt)})
	}
	for _, g := range gatesAwaitingDecision(nodes, edges) {
		entries = append(entries, inboxEntry{Type: "gate", ID: g.ID, Kind: "gate", Node: g.Key, Title: g.Title, Options: gateOptions(g), Age: "-"})
	}
	return entries, nil
}

func runInbox(ctx context.Context, env *cliEnv, args []string) int {
	// --json is a global flag; accept it after the graph argument too.
	var pos []string
	for _, a := range args {
		if a == "--json" || a == "-json" {
			env.json = true
			continue
		}
		pos = append(pos, a)
	}
	if len(pos) != 1 {
		fmt.Fprintln(env.errOut, inboxUsage)
		return exitUsage
	}
	graphID := pos[0]
	entries, err := collectInbox(ctx, env.client, graphID)
	if err != nil {
		return reportError(env.errOut, err)
	}
	if env.limit > 0 && len(entries) > env.limit {
		entries = entries[:env.limit]
	}
	if env.json {
		if entries == nil {
			entries = []inboxEntry{}
		}
		if err := renderJSON(env.out, entries); err != nil {
			return reportError(env.errOut, err)
		}
		return exitOK
	}
	if len(entries) == 0 {
		fmt.Fprintln(env.out, "nothing needs you")
		return exitOK
	}
	rows := make([][]string, len(entries))
	for i, e := range entries {
		detail := e.Detail
		if e.Type == "gate" {
			detail = e.Title
			if len(e.Options) > 0 {
				detail += " [" + strings.Join(e.Options, " | ") + "]"
			}
		}
		id := e.ID
		if e.Type == "review" {
			id = reviewShortID(id)
		}
		rows[i] = []string{fmt.Sprint(i + 1), e.Kind, id, e.Node, e.Age, detail}
	}
	env.table([]string{"#", "KIND", "ID", "NODE", "AGE", "DETAIL"}, rows)
	fmt.Fprintln(env.out, "\nanswer a review item: graphwatch resolve <graph> <item> approve|reject|override\ndecide a gate:        graphwatch decide <graph> <node> <decision>")
	return exitOK
}

func runResolve(ctx context.Context, env *cliEnv, args []string) int {
	fs := flag.NewFlagSet("resolve", flag.ContinueOnError)
	fs.SetOutput(env.errOut)
	note := fs.String("note", "", "")
	if f := env.fs.Lookup("note"); f != nil && f.Value.String() != "" {
		*note = f.Value.String()
	}
	// Flags may follow the positional arguments.
	var pos []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return exitUsage
		}
		args = fs.Args()
		if len(args) > 0 {
			pos = append(pos, args[0])
			args = args[1:]
		}
	}
	if len(pos) != 3 {
		fmt.Fprintln(env.errOut, resolveUsage)
		return exitUsage
	}
	graphID, want, action := pos[0], pos[1], pos[2]
	switch action {
	case "approve", "reject", "override":
	default:
		fmt.Fprintf(env.errOut, "graphwatch: unknown action %q\n%s\n", action, resolveUsage)
		return exitUsage
	}
	if action == "override" && strings.TrimSpace(*note) == "" {
		fmt.Fprintln(env.errOut, "graphwatch: override needs a reason: pass --note text")
		return exitUsage
	}

	items, err := env.client.reviewItems(ctx, graphID)
	if err != nil {
		return reportError(env.errOut, err)
	}
	var found *apiReviewItem
	for i := range items {
		if items[i].ID == want || (len(want) >= 4 && strings.HasPrefix(items[i].ID, want)) {
			if found != nil {
				fmt.Fprintf(env.errOut, "graphwatch: item prefix %q is ambiguous\n", want)
				return exitUsage
			}
			found = &items[i]
		}
	}
	if found == nil {
		fmt.Fprintf(env.errOut, "graphwatch: no review item %q in graph %s\n", want, graphID)
		return exitRuntime
	}

	resp, err := env.client.resolveReview(ctx, graphID, found.ID, resolveRequest{Action: action, Note: *note})
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			switch {
			case ae.status == 403:
				fmt.Fprintln(env.errOut, "you are not an arbitrator on this graph")
				return exitRuntime
			case ae.status == 409 && ae.code == "validation_not_decided":
				msg := ae.detail
				if msg == "" {
					msg = ae.msg
				}
				fmt.Fprintln(env.errOut, msg)
				return exitRuntime
			}
		}
		return reportError(env.errOut, err)
	}

	// The resolution does not list what it released, so read the node and the
	// nodes that require it back and show what the service now holds.
	nodes, nerr := env.client.nodesFull(ctx, graphID)
	edges, eerr := env.client.edges(ctx, graphID)
	byID := map[string]apiNodeFull{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	var deps []apiNodeFull
	if nerr == nil && eerr == nil {
		// A requires edge points from the dependent to its prerequisite.
		for _, e := range edges {
			if e.Type == "requires" && e.ToNodeID == found.NodeID {
				if n, ok := byID[e.FromNodeID]; ok {
					deps = append(deps, n)
				}
			}
		}
	}
	held := byID[found.NodeID]
	if env.json {
		out := map[string]any{"item": found.ID, "action": resp.Data.Action, "node": held.Key, "status": held.Status, "dependents": depSummary(deps)}
		if err := renderJSON(env.out, out); err != nil {
			return reportError(env.errOut, err)
		}
		return exitOK
	}
	fmt.Fprintf(env.out, "%s %s on %s: node status %s\n", found.Kind, action, held.Key, orUnknown(held.Status))
	fmt.Fprintf(env.out, "dependents: %d\n", len(deps))
	for _, d := range deps {
		fmt.Fprintf(env.out, "  %s  %s  %s\n", d.Key, orUnknown(d.Status), d.WorkState)
	}
	return exitOK
}

func depSummary(deps []apiNodeFull) []map[string]string {
	out := make([]map[string]string, 0, len(deps))
	for _, d := range deps {
		out = append(out, map[string]string{"key": d.Key, "status": d.Status, "work_state": d.WorkState})
	}
	return out
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

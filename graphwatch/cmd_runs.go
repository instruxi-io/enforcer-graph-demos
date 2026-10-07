package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"time"
)

func init() {
	register("runs", "a node's runs and how they were judged: runs <graph> <node> | runs show <graph> <node> <run> [--full]", runRuns)
}

const runsUsage = "usage: graphwatch runs <graph> <node-id>\n       graphwatch runs show <graph> <node-id> <run-id> [--full]"

// evidenceLines is how much of one evidence output `show` prints without --full.
const evidenceLines = 12

func tstamp(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "-"
	}
	return t.UTC().Format("2006-01-02 15:04:05")
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// verdictState prints the service's state verbatim, or - when never judged.
func (r apiRun) verdictState() string {
	if r.Verification == nil {
		return "-"
	}
	s := dash(r.Verification.State)
	if r.Validation != nil && r.Validation.State != "" {
		s += " (validation " + r.Validation.State + ")"
	}
	return s
}

// heldNote says why a run's node is held, or "" when it is not. The server owns
// the state; this only reads what the run carries.
func (r apiRun) heldNote() string {
	var why []string
	if v := r.Validation; v != nil && (v.State == "pending" || v.State == "escalated") {
		why = append(why, "verifying: validation "+v.State)
	}
	if v := r.Verification; v != nil {
		if v.State == "pending" {
			why = append(why, "verifying: judgment pending")
		}
		if v.PendingHuman || v.Escalated != "" && v.DecidedBy == "" {
			why = append(why, "unresolved review item: awaiting a human")
		}
	}
	if len(why) == 0 {
		return ""
	}
	return "HELD " + strings.Join(why, "; ")
}

func runRuns(ctx context.Context, env *cliEnv, args []string) int {
	if len(args) > 0 && args[0] == "show" {
		return runRunsShow(ctx, env, args[1:])
	}
	if len(args) != 2 {
		fmt.Fprintln(env.errOut, runsUsage)
		return exitUsage
	}
	runs, raws, err := env.client.nodeRuns(ctx, args[0], args[1])
	if err != nil {
		return reportError(env.errOut, err)
	}
	if env.limit > 0 && len(runs) > env.limit {
		runs, raws = runs[:env.limit], raws[:env.limit]
	}
	if env.json {
		if err := renderJSON(env.out, raws); err != nil {
			return reportError(env.errOut, err)
		}
		return exitOK
	}
	rows := make([][]string, len(runs))
	for i, r := range runs {
		runner := r.Runner
		if runner == "" {
			runner = r.Client
		}
		rows[i] = []string{fmt.Sprint(r.Attempt), r.Status, dash(runner), tstamp(&r.StartedAt), tstamp(r.EndedAt), r.verdictState()}
	}
	env.table([]string{"ATTEMPT", "STATUS", "RUNNER", "STARTED", "FINISHED", "VERDICT"}, rows)
	return exitOK
}

func runRunsShow(ctx context.Context, env *cliEnv, args []string) int {
	fs := flag.NewFlagSet("runs show", flag.ContinueOnError)
	fs.SetOutput(env.errOut)
	full := fs.Bool("full", false, "print evidence output in full")
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
		fmt.Fprintln(env.errOut, runsUsage)
		return exitUsage
	}
	g, n, id := pos[0], pos[1], pos[2]
	run, raw, err := env.client.nodeRun(ctx, g, n, id)
	if err != nil {
		return reportError(env.errOut, err)
	}
	verdicts, err := env.client.runVerdicts(ctx, g, n, id)
	if err != nil {
		return reportError(env.errOut, err)
	}
	votes, err := env.client.runVotes(ctx, g, n, id)
	if err != nil {
		return reportError(env.errOut, err)
	}
	if env.json {
		out := map[string]any{"run": raw, "verdicts": verdicts, "votes": votes}
		if err := renderJSON(env.out, out); err != nil {
			return reportError(env.errOut, err)
		}
		return exitOK
	}
	w := env.out
	if h := run.heldNote(); h != "" {
		fmt.Fprintln(w, h)
	}
	fmt.Fprintf(w, "run %s  attempt %d  status %s\n", run.ID, run.Attempt, run.Status)
	fmt.Fprintf(w, "started %s  finished %s\n", tstamp(&run.StartedAt), tstamp(run.EndedAt))
	if run.Error != "" {
		fmt.Fprintf(w, "error: %s\n", run.Error)
	}
	if run.Report != "" {
		fmt.Fprintf(w, "\nreport:\n%s\n", indent(run.Report))
	}
	if len(run.Outputs) > 0 {
		b, _ := json.Marshal(run.Outputs)
		fmt.Fprintf(w, "\noutputs: %s\n", b)
	}
	fmt.Fprintf(w, "\nevidence (%d):\n", len(run.Evidence))
	for i, e := range run.Evidence {
		printEvidence(w, i+1, e, *full)
	}
	fmt.Fprintf(w, "\nverdicts (%d):\n", len(verdicts))
	for _, v := range verdicts {
		fmt.Fprintf(w, "  %s  %s  outcome %s  model %s  node_moved %t\n",
			shortID(v.ID, false), v.CreatedAt.UTC().Format(time.RFC3339), dash(v.State), dash(v.Model), v.NodeMoved)
		printVerification(w, v.Verification)
	}
	fmt.Fprintln(w, "\nvalidation:")
	if v := run.Validation; v == nil {
		fmt.Fprintln(w, "  none (the graph had no validation config)")
	} else {
		fmt.Fprintf(w, "  state %s", dash(v.State))
		if v.Tally != nil {
			fmt.Fprintf(w, "  accept %d  reject %d  undecided %d", v.Tally.Accept, v.Tally.Reject, v.Tally.Undecided)
		}
		if v.DecidedBy != "" {
			fmt.Fprintf(w, "  decided_by %s", v.DecidedBy)
		}
		if v.Reason != "" {
			fmt.Fprintf(w, "  reason %s", v.Reason)
		}
		fmt.Fprintln(w)
		if len(v.Config) > 0 && string(v.Config) != "null" {
			fmt.Fprintf(w, "  config: %s\n", v.Config)
		}
	}
	fmt.Fprintf(w, "votes (%d):\n", len(votes))
	for _, v := range votes {
		fmt.Fprintf(w, "  %s  %s  %s", v.CreatedAt.UTC().Format(time.RFC3339), shortID(v.Voter, false), v.Vote)
		if v.Note != "" {
			fmt.Fprintf(w, "  %q", v.Note)
		}
		fmt.Fprintln(w)
	}
	return exitOK
}

func indent(s string) string {
	return "  " + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n  ")
}

func printEvidence(w interface{ Write([]byte) (int, error) }, n int, e apiEvidence, full bool) {
	switch e.Kind {
	case "command":
		exit := "?"
		if e.Exit != nil {
			exit = fmt.Sprint(*e.Exit)
		}
		fmt.Fprintf(w, "  %d. command (exit %s): %s\n", n, exit, e.Cmd)
	case "file":
		fmt.Fprintf(w, "  %d. file: %s\n", n, e.Path)
	default:
		fmt.Fprintf(w, "  %d. %s: %s\n", n, dash(e.Kind), e.Label)
	}
	body := e.Output
	if body == "" {
		body = e.Excerpt
	}
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if body == "" {
		return
	}
	shown := lines
	if !full && len(lines) > evidenceLines {
		shown = lines[:evidenceLines]
	}
	for _, l := range shown {
		fmt.Fprintf(w, "       | %s\n", l)
	}
	if len(shown) < len(lines) {
		fmt.Fprintf(w, "       … %d more lines (--full to expand)\n", len(lines)-len(shown))
	}
}

func printVerification(w interface{ Write([]byte) (int, error) }, v apiVerification) {
	fmt.Fprintf(w, "    state %s  policy %s", dash(v.State), dash(v.Policy))
	if v.Confidence != nil {
		fmt.Fprintf(w, "  confidence %.2f", *v.Confidence)
	}
	if v.DecidedBy != "" {
		fmt.Fprintf(w, "  decided_by %s", v.DecidedBy)
	}
	if v.Escalated != "" {
		fmt.Fprintf(w, "  escalated %s", v.Escalated)
	}
	fmt.Fprintln(w)
	if v.Reason != "" {
		fmt.Fprintf(w, "    reason: %s\n", v.Reason)
	}
	for i, c := range v.Criteria {
		// noul is P(met); the verdict's own state, not this split, decides.
		res := "?"
		p := ""
		if c.Noul != nil {
			res = "NOT MET"
			if *c.Noul >= 0.5 {
				res = "MET"
			}
			p = fmt.Sprintf(" (p=%.2f)", *c.Noul)
		}
		fmt.Fprintf(w, "    criterion %d: %s%s  %s\n", i+1, res, p, c.Text)
	}
	if len(v.Checks) > 0 && string(v.Checks) != "null" && string(v.Checks) != "[]" {
		fmt.Fprintf(w, "    checks: %s\n", v.Checks)
	}
}

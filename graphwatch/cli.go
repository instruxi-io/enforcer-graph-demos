package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
)

// Exit codes every command returns.
const (
	exitOK      = 0
	exitRuntime = 1 // runtime or API error
	exitUsage   = 2 // bad flags or arguments
)

// cliEnv is what a command gets: a resolved client and the global flags.
type cliEnv struct {
	client  *client
	base    string
	out     io.Writer
	errOut  io.Writer
	json    bool
	limit   int
	noColor bool
	isTTY   bool
}

type command struct {
	name, summary string
	run           func(ctx context.Context, env *cliEnv, args []string) int
}

var commands = map[string]command{}

// register is called from a command's init(). A duplicate name is a programming
// error, so it panics at start-up rather than silently shadowing.
func register(name, summary string, run func(ctx context.Context, env *cliEnv, args []string) int) {
	if _, dup := commands[name]; dup {
		panic("graphwatch: command registered twice: " + name)
	}
	commands[name] = command{name: name, summary: summary, run: run}
}

func init() {
	register("help", "list commands, or describe one: help <command>", nil)
}

func sortedCommands() []command {
	out := make([]command, 0, len(commands))
	for _, c := range commands {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// dispatch runs args[0] when it is a registered command. handled is false when
// it is not, so main falls through to the bare-flag watch/demo path.
func dispatch(args []string, stdout, stderr io.Writer) (code int, handled bool) {
	if len(args) == 0 {
		return 0, false
	}
	cmd, ok := commands[args[0]]
	if !ok {
		return 0, false
	}
	if cmd.name == "help" {
		return runHelp(args[1:], stdout, stderr), true
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return runCommand(ctx, cmd, args[1:], stdout, stderr), true
}

// globalFlags registers the flags every command parses.
type globalFlags struct {
	base    *string
	json    *bool
	limit   *int
	noColor *bool
}

func newGlobalFlags(fs *flag.FlagSet) globalFlags {
	return globalFlags{
		base:    fs.String("base", envOr("GRAPH_BASE_URL", "https://api.instruxi.dev"), "api origin"),
		json:    fs.Bool("json", false, "print the API's data payload as JSON"),
		limit:   fs.Int("limit", 0, "show at most this many rows (0: all)"),
		noColor: fs.Bool("no-color", false, "no colour (NO_COLOR is also respected)"),
	}
}

func runCommand(ctx context.Context, cmd command, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("graphwatch "+cmd.name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	g := newGlobalFlags(fs)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if *g.limit < 0 {
		fmt.Fprintln(stderr, "graphwatch: --limit must be 0 or more")
		return exitUsage
	}
	cred, err := credentialFromEnv()
	if err != nil {
		fmt.Fprintln(stderr, "graphwatch:", err)
		return exitUsage
	}
	env := &cliEnv{client: newClient(*g.base, cred), base: *g.base, out: stdout, errOut: stderr,
		json: *g.json, limit: *g.limit, noColor: *g.noColor, isTTY: isTerminal(stdout)}
	return cmd.run(ctx, env, fs.Args())
}

func runHelp(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		cmd, ok := commands[args[0]]
		if !ok {
			fmt.Fprintf(stderr, "graphwatch: unknown command %q\n", args[0])
			return exitUsage
		}
		fmt.Fprintf(stdout, "usage: graphwatch %s [flags]\n\n%s\n\n", cmd.name, cmd.summary)
		fs := flag.NewFlagSet(cmd.name, flag.ContinueOnError)
		fs.SetOutput(stdout)
		newGlobalFlags(fs)
		fmt.Fprintln(stdout, "flags:")
		fs.PrintDefaults()
		return exitOK
	}
	fmt.Fprintln(stdout, "usage: graphwatch <command> [flags]")
	fmt.Fprintln(stdout, "       graphwatch --graph <id> | --demo   (watch; the default command)")
	fmt.Fprintln(stdout, "\ncommands:")
	for _, c := range sortedCommands() {
		fmt.Fprintf(stdout, "  %-12s %s\n", c.name, c.summary)
	}
	fmt.Fprintln(stdout, "\nglobal flags: --base <url>  --json  --limit <n>  --no-color")
	return exitOK
}

// reportError prints an API failure as `graphwatch: <status> <code>: <message>`
// from the API's error envelope. Only the response is used, never the request,
// so no header value can leak. It returns the exit code.
func reportError(w io.Writer, err error) int {
	var ae *apiError
	if errors.As(err, &ae) && ae.status != 0 {
		code, msg := ae.code, ae.detail
		if code == "" {
			code = "error"
		}
		if msg == "" {
			msg = ae.msg
		}
		fmt.Fprintf(w, "graphwatch: %d %s: %s\n", ae.status, code, msg)
		return exitRuntime
	}
	fmt.Fprintln(w, "graphwatch:", err)
	return exitRuntime
}

// parseErrorEnvelope pulls the machine code and message out of an error body:
// {"error":{"code","message"}}, {"code","message"} or {"error":"text"}.
func parseErrorEnvelope(b []byte) (code, msg string) {
	var raw struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if json.Unmarshal(b, &raw) != nil {
		return "", ""
	}
	code, msg = raw.Code, raw.Message
	if len(raw.Error) > 0 {
		var o struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		var s string
		if json.Unmarshal(raw.Error, &o) == nil && (o.Code != "" || o.Message != "") {
			code, msg = o.Code, o.Message
		} else if json.Unmarshal(raw.Error, &s) == nil && msg == "" {
			msg = s
		}
	}
	return strings.TrimSpace(code), strings.TrimSpace(msg)
}

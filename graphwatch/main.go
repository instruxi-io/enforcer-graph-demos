// Command graphwatch draws a graph in the terminal and animates it from its
// activity stream (GET /graphs/{id}/stream): nodes are dots whose shading is
// their state, prerequisites sit above what they unblock so the DAG fans out
// downward, and work shows as heat — pulses flow down an edge into a node when
// it starts and out of it when it finishes, and everything cools when idle.
//
//	GRAPH_AUTH_HELPER='node …/enforcer/<v>/bin/enforcer-headers.mjs' go run ./cmd/graphwatch --graph <id>
//	GRAPH_API_KEY=… go run ./cmd/graphwatch --graph <id>
//	GRAPH_API_KEY=… go run ./cmd/graphwatch --demo          # grow a plan and work it
//	go run ./cmd/graphwatch --demo --layout mycelium         # offline: a colony grows, no API needed
//
// GRAPH_AUTH_HELPER (preferred) runs a command that prints the auth headers as
// JSON — the Enforcer plugin's OAuth session from /enforcer:login, refreshed as
// it expires — and wins over GRAPH_API_KEY when both are set.
//
// It reads with the ordinary endpoints (/nodes, /edges) and only uses the
// stream as its clock: every event means "refetch", exactly as a browser would.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"math/rand/v2"
)

func main() {
	if code, ok := dispatch(os.Args[1:], os.Stdout, os.Stderr); ok {
		os.Exit(code)
	}
	watchMain()
}

// watchMain is the bare-flag form (--graph, --demo): the default command.
func watchMain() {
	base := flag.String("base", envOr("GRAPH_BASE_URL", "https://api.instruxi.dev"), "api origin")
	graphID := flag.String("graph", "", "graph id to watch")
	demoMode := flag.Bool("demo", false, "create a graph, grow it and work it while watching; with --layout mycelium, an offline demo that needs no API")
	size := flag.Int("size", 36, "demo: nodes to plan")
	workers := flag.Int("workers", 4, "demo: concurrent harnesses")
	failRate := flag.Float64("fail", 0.08, "demo: chance a run fails (and is retried)")
	stay := flag.Bool("stay", false, "keep watching after the plan completes")
	fps := flag.Int("fps", 15, "frames per second (only changed cells are sent)")
	motion := flag.String("motion", "events", "events: motion follows graph events and a quiet graph stops drawing; continuous: running nodes pulse and stream particles every frame")
	layout := flag.String("layout", "layers", "layers: top-to-bottom layers; mycelium: a radial growth view with roots at the centre")
	flag.Parse()
	if *layout != "layers" && *layout != "mycelium" {
		fmt.Fprintln(os.Stderr, "graphwatch: --layout must be layers or mycelium")
		os.Exit(2)
	}

	// The mycelium demo needs no graph: it works a plan in-process.
	if *demoMode && *layout == "mycelium" {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		v := newView()
		v.mycelium = true
		v.continuous = *motion == "continuous"
		runLocalDemo(ctx, v, *workers, *fps, *stay)
		return
	}

	cred, err := credentialFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "graphwatch:", err)
		os.Exit(2)
	}
	if *graphID == "" && !*demoMode {
		fmt.Fprintln(os.Stderr, "graphwatch: --graph <id> or --demo")
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	c := newClient(*base, cred)
	v := newView()
	v.continuous = *motion == "continuous"
	if v.mycelium = *layout == "mycelium"; v.mycelium {
		fmt.Fprintln(os.Stderr, "graphwatch: mycelium mode")
		v.say("mycelium mode", rgb{90, 210, 170}) // the alt screen hides stderr
	}

	var d *demo
	if *demoMode {
		d = &demo{c: c, size: *size, workers: *workers, failRate: *failRate,
			rng:  rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 7)),
			logf: func(s string, col rgb) { v.mu.Lock(); v.say(s, col); v.mu.Unlock() }}
		if err := d.create(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "graphwatch:", err)
			os.Exit(1)
		}
		*graphID = d.graphID
	}

	g, err := c.graph(ctx, *graphID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "graphwatch:", err)
		os.Exit(1)
	}
	v.title = g.Slug
	if g.DependencyEdgeType != "" {
		v.edgeT = g.DependencyEdgeType
	}

	// Refetch on every event, coalesced: a burst of events is one read.
	refresh := make(chan struct{}, 1)
	poke := func() {
		select {
		case refresh <- struct{}{}:
		default:
		}
	}
	go func() {
		t := time.NewTicker(10 * time.Second) // a safety net, not the clock
		defer t.Stop()
		for {
			ns, err1 := c.nodes(ctx, *graphID)
			es, err2 := c.edges(ctx, *graphID)
			if err1 == nil && err2 == nil {
				v.apply(ns, es)
			}
			select {
			case <-ctx.Done():
				return
			case <-refresh:
			case <-t.C:
			}
		}
	}()
	go c.stream(ctx, *graphID, func(ev sseEvent) {
		v.event(ev)
		if ev.Event != ":" {
			poke()
		}
	}, v.setLink)

	var wg sync.WaitGroup
	if d != nil {
		finished := func() bool { _, ok := v.completed(); return ok && d.planned.Load() }
		wg.Add(1)
		go func() { defer wg.Done(); d.plan(ctx) }()
		for i := 0; i < d.workers; i++ {
			wg.Add(1)
			go func(i int) { defer wg.Done(); d.work(ctx, i, finished) }(i + 1)
		}
	}

	// The terminal: alternate screen, no cursor, restored on every exit path.
	os.Stdout.WriteString("\x1b[?1049h\x1b[?25l\x1b[2J")
	restore := func() { os.Stdout.WriteString("\x1b[0m\x1b[?25h\x1b[?1049l") }
	defer restore()

	w, h := termSize()
	sizeT := time.NewTicker(time.Second)
	defer sizeT.Stop()
	frameT := time.NewTicker(time.Second / time.Duration(max(*fps, 1)))
	defer frameT.Stop()
	last := time.Now()
	var scr screen
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-sizeT.C:
			if nw, nh := termSize(); nw != w || nh != h {
				w, h = nw, nh
				scr.reset()
			}
		case now := <-frameT.C:
			dt := now.Sub(last).Seconds()
			last = now
			if out := scr.diff(v.frame(w, h, dt)); out != "" {
				os.Stdout.WriteString(out)
			}
			if at, ok := v.completed(); ok && !*stay && (d == nil || d.planned.Load()) && time.Since(at) > 6*time.Second {
				break loop
			}
		}
	}
	cancel()
	wg.Wait()
	restore()
	fmt.Printf("graphwatch: %s (%s)\n", g.Slug, *graphID)
}

// credentialFromEnv resolves GRAPH_AUTH_HELPER (OAuth, preferred: a command
// printing the auth headers as JSON, fed by /enforcer:login, so no long-lived
// key sits in the environment) then GRAPH_API_KEY. The auth-resolution task
// replaces this function.
func credentialFromEnv() (credential, error) {
	if cmd := strings.TrimSpace(os.Getenv("GRAPH_AUTH_HELPER")); cmd != "" {
		h := &helper{cmd: cmd}
		if _, err := h.headers(context.Background(), false); err != nil {
			return nil, err
		}
		return h, nil
	}
	if key := os.Getenv("GRAPH_API_KEY"); key != "" {
		return apiKey(key), nil
	}
	return nil, errors.New("set GRAPH_AUTH_HELPER (OAuth, preferred) or GRAPH_API_KEY")
}

// termSize asks stty, so the command needs nothing outside the standard
// library. Falls back to 100×32.
func termSize() (int, int) {
	cmd := exec.Command("stty", "size")
	cmd.Stdin = os.Stdin
	if out, err := cmd.Output(); err == nil {
		if f := strings.Fields(string(out)); len(f) == 2 {
			h, _ := strconv.Atoi(f[0])
			w, _ := strconv.Atoi(f[1])
			if w > 20 && h > 10 {
				return w, h
			}
		}
	}
	return 100, 32
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

package main

import (
	"strings"
	"testing"
	"time"
)

func recallRoutes() map[string]any {
	return map[string]any{
		"/graphs/g1/recall": []any{
			map[string]any{"kind": "decision", "node_key": "build", "created_at": "2026-10-01T10:00:00Z",
				"run_id": "run-1", "body": "use sqlite\nbecause it is embedded"},
			map[string]any{"kind": "note", "node_key": "test", "created_at": "2026-10-02T10:00:00Z",
				"run_id": "run-2", "body": "flaky on CI"},
		},
		"/graphs/g1/context": map[string]any{
			"sha256": "abc123def", "size": 2048, "built_at": "2026-10-03T09:00:00Z", "recall_enabled": true},
	}
}

func TestRecallPrintsHits(t *testing.T) {
	f := newFakeAPI(t, recallRoutes())
	out, errOut, code := runCLI(t, f, "recall", "--q", "sqlite", "--node", "build", "--limit", "1", "g1")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "decision") || !strings.Contains(out, "build") ||
		!strings.Contains(out, "run-1") || !strings.Contains(out, "use sqlite") {
		t.Errorf("missing hit fields: %s", out)
	}
	if strings.Contains(out, "because it is embedded") {
		t.Errorf("printed more than the first line: %s", out)
	}
	if strings.Contains(out, "flaky on CI") {
		t.Errorf("--limit 1 printed a second hit: %s", out)
	}
	reqs := strings.Join(f.requests(), "\n")
	for _, want := range []string{"q=sqlite", "node=build", "limit=1"} {
		if !strings.Contains(reqs, want) {
			t.Errorf("request lacks %s: %s", want, reqs)
		}
	}
	f.onlyReads(t)
}

func TestContextPrintsPack(t *testing.T) {
	f := newFakeAPI(t, recallRoutes())
	out, errOut, code := runCLI(t, f, "context", "g1")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"abc123def", "2048", "2026-10-03T09:00:00Z", "true"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
	f.onlyReads(t)
}

func TestRecallUnknownGraphExitsOne(t *testing.T) {
	f := newFakeAPI(t, recallRoutes())
	for _, cmd := range []string{"recall", "context"} {
		_, errOut, code := runCLI(t, f, cmd, "nope")
		if code != 1 || !strings.Contains(errOut, "no graph nope") {
			t.Errorf("%s: exit %d, stderr %q", cmd, code, errOut)
		}
	}
}

func withRecallNow(t *testing.T, now string) {
	t.Helper()
	fixed, err := time.Parse(time.RFC3339, now)
	if err != nil {
		t.Fatal(err)
	}
	old := recallNow
	recallNow = func() time.Time { return fixed }
	t.Cleanup(func() { recallNow = old })
}

func TestRecallSinceDropsOldHits(t *testing.T) {
	withRecallNow(t, "2026-10-02T12:00:00Z")
	f := newFakeAPI(t, recallRoutes())
	out, errOut, code := runCLI(t, f, "recall", "--since", "3h", "g1")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "flaky on CI") {
		t.Errorf("dropped the recent hit: %s", out)
	}
	if strings.Contains(out, "use sqlite") {
		t.Errorf("kept the hit older than the window: %s", out)
	}
}

func TestRecallSinceAcceptsDays(t *testing.T) {
	withRecallNow(t, "2026-10-04T11:00:00Z")
	f := newFakeAPI(t, recallRoutes())
	out, errOut, code := runCLI(t, f, "recall", "--since", "3d", "g1")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if strings.Contains(out, "use sqlite") || !strings.Contains(out, "flaky on CI") {
		t.Errorf("3d window kept the wrong hits: %s", out)
	}
}

func TestRecallSinceBadDurationExitsTwo(t *testing.T) {
	f := newFakeAPI(t, recallRoutes())
	_, errOut, code := runCLI(t, f, "recall", "--since", "soon", "g1")
	if code != 2 || !strings.Contains(errOut, "--since: bad duration soon") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}

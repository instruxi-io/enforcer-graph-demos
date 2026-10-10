package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func attendFake(t *testing.T) *fakeAPI {
	f := newFakeAPI(t, inboxRoutes())
	f.posts = map[string]fakeReply{
		"/graphs/g1/review/run:aaaaaaaa-1111/resolve": {200, map[string]any{
			"success": true, "data": map[string]any{"action": "approve", "item_id": "run:aaaaaaaa-1111", "kind": "verdict_pending"}}},
		"/graphs/g1/nodes/n2/decide": {200, map[string]any{
			"success": true, "data": map[string]any{"frontier": []map[string]any{{"node_id": "n6", "key": "release", "title": "Release", "type": "task"}}}}},
	}
	return f
}

func runAttendScript(t *testing.T, f *fakeAPI, script string) (string, string, int) {
	t.Helper()
	attendIn = strings.NewReader(script)
	t.Cleanup(func() { attendIn = nil })
	return runCLI(t, f, "attend", "g1")
}

func TestAttendPostsApproveFromStdin(t *testing.T) {
	f := attendFake(t)
	out, _, code := runAttendScript(t, f, "1 approve looks good\nquit\n")
	if code != 0 {
		t.Fatalf("code %d:\n%s", code, out)
	}
	for _, want := range []string{"1  verdict_pending  alpha", "2  gate  ship-gate", "\a2 need you", "approve on alpha"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if len(f.bodies) != 1 {
		t.Fatalf("bodies %v", f.bodies)
	}
	var got resolveRequest
	if err := json.Unmarshal([]byte(f.bodies[0]), &got); err != nil || got.Action != "approve" || got.Note != "looks good" {
		t.Fatalf("body %s (%v)", f.bodies[0], err)
	}
}

func TestAttendDecidesGateFromStdin(t *testing.T) {
	f := attendFake(t)
	out, _, code := runAttendScript(t, f, "2 decide ship go now\nquit\n")
	if code != 0 {
		t.Fatalf("code %d:\n%s", code, out)
	}
	if !strings.Contains(out, `ship-gate decided "ship"`) || !strings.Contains(out, "release") {
		t.Errorf("outcome missing:\n%s", out)
	}
	var got decideRequest
	if len(f.bodies) != 1 || json.Unmarshal([]byte(f.bodies[0]), &got) != nil || got.Decision != "ship" || got.Note != "go now" {
		t.Fatalf("bodies %v", f.bodies)
	}
}

func TestAttendRefusesDecisionOutsideOptions(t *testing.T) {
	f := attendFake(t)
	out, errOut, code := runAttendScript(t, f, "2 decide maybe\n9 approve\nquit\n")
	if code != 0 {
		t.Fatalf("code %d:\n%s", code, out)
	}
	if len(f.bodies) != 0 {
		t.Fatalf("a refused decision was posted: %v", f.bodies)
	}
	if !strings.Contains(errOut, "ship | hold") || !strings.Contains(errOut, "no item 9") {
		t.Errorf("stderr:\n%s", errOut)
	}
}

func TestAttendExitsOnEOF(t *testing.T) {
	f := attendFake(t)
	out, _, code := runAttendScript(t, f, "")
	if code != 0 || !strings.Contains(out, "2 need you") {
		t.Fatalf("code %d:\n%s", code, out)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) != 0 {
		t.Fatalf("EOF wrote: %v", f.bodies)
	}
}

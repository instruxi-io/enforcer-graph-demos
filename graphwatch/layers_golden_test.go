package main

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// layersGolden is the sha256 of 90 layers-mode frames of testGraph, with a
// status change at frame 30, captured on master before mycelium v2. The
// mycelium work must leave `--layout layers` byte-identical.
const layersGolden = "e7654873c9c886f4409c7cf7295faaf9cd0d9233e3d16e51f3d3db62943fa8b4"

func TestLayersOutputUnchanged(t *testing.T) {
	v := newView()
	tick := simulated(v, 15)
	v.apply(testGraph())
	h := sha256.New()
	for i := 0; i < 90; i++ {
		tick()
		if i == 30 {
			ns, es := testGraph()
			ns[1].Status = "done"
			v.apply(ns, es)
		}
		h.Write([]byte(v.frame(100, 36, 1.0/15)))
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != layersGolden {
		t.Errorf("layers output changed: sha256 %s, want %s", got, layersGolden)
	}
}

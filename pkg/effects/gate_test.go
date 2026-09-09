package effects

import (
	"math"
	"testing"
)

func TestGateBuild(t *testing.T) {
	n := buildNode(t, "gate", nil)
	buf := stereoFrames(64)
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatalf("process: %v", err)
	}
	if err := n.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestGateInvalidParams(t *testing.T) {
	cases := []map[string]any{
		{"threshold": math.NaN()},
		{"ratio": 0.5},
		{"ratio": 101},
		{"attack": 0},
		{"attack": 0.05},
		{"attack": 2000},
		{"release": 0},
		{"release": 6000},
		{"hold": -1},
		{"hold": 6000},
	}
	for _, p := range cases {
		if _, err := registry["gate"](24000, p); err == nil {
			t.Errorf("params %v: want error", p)
		}
	}
}

// TestGateLowThresholdPassthrough verifies an open gate passes the signal
// through. algo-dsp's Gate opens (unity gain) when the input level is above
// threshold, so a very low threshold (-300 dB, far below any real signal)
// keeps the gate open for the whole buffer and the output is bit-exact.
func TestGateLowThresholdPassthrough(t *testing.T) {
	n := buildNode(t, "gate", map[string]any{"threshold": -300})
	buf := stereoFrames(1024)
	tone := sineTone(440, 1024)
	for j := range 1024 {
		buf[2*j] = float32(tone[j])
		buf[2*j+1] = float32(tone[j]) * 0.5
	}
	want := append([]float32(nil), buf...)
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	for i := range buf {
		if buf[i] != want[i] {
			t.Fatalf("sample %d: got %v want %v (open gate must be passthrough)", i, buf[i], want[i])
		}
	}
}

// TestGateSuppression verifies the gate attenuates low-level input toward
// the range floor: a -26 dB tone under a -20 dB threshold with the hardest
// ratio (100) is pushed down to the -80 dB range floor (1e-4), i.e. near
// zero output from the first sample (the envelope starts at 0, already far
// below threshold).
func TestGateSuppression(t *testing.T) {
	n := buildNode(t, "gate", map[string]any{
		"threshold": -20, "ratio": 100,
		"attack": 5, "release": 200,
	})
	frames := 48000
	buf := stereoFrames(frames)
	for j := range frames {
		buf[2*j] = 0.05 * float32(math.Sin(2*math.Pi*440*float64(j)/24000))
	}
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	maxOut := float64(0)
	for j := range frames {
		if v := math.Abs(float64(buf[2*j])); v > maxOut {
			maxOut = v
		}
	}
	if maxOut >= 1e-3 {
		t.Fatalf("gated low-level tone peaks at %v, want < 1e-3 (near the -80 dB range floor)", maxOut)
	}
}

func TestGateLRSeparation(t *testing.T) {
	n := buildNode(t, "gate", nil)
	buf := stereoFrames(4096)
	for j := range 4096 {
		buf[2*j] = 0.4 * float32(math.Sin(2*math.Pi*440*float64(j)/24000))
	}
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	seenL := false
	for j := range 4096 {
		if buf[2*j] != 0 {
			seenL = true
		}
		if buf[2*j+1] != 0 {
			t.Fatalf("R frame %d: got %v want 0", j, buf[2*j+1])
		}
	}
	if !seenL {
		t.Fatal("L channel silent: gate did not process the tone")
	}
}

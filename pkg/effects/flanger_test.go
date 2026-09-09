package effects

import (
	"math"
	"testing"
)

func TestFlangerBuild(t *testing.T) {
	n := buildNode(t, "flanger", nil)
	buf := stereoFrames(64)
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatalf("process: %v", err)
	}
	if err := n.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestFlangerInvalidParams(t *testing.T) {
	cases := []map[string]any{
		{"rateHz": 0},
		{"rateHz": -1},
		{"depth": 0},
		{"depth": -0.001},
		{"baseDelay": -0.001},
		{"baseDelay": 0.02}, // > 10 ms algo-dsp max
		{"feedback": -1},
		{"feedback": 1},
		{"mix": -0.1},
		{"mix": 1.5},
		{"mix": math.NaN()},
	}
	for _, p := range cases {
		if _, err := registry["flanger"](24000, p); err == nil {
			t.Errorf("params %v: want error", p)
		}
	}
}

// TestFlangerMixZeroPassthrough verifies mix=0 passes the signal through
// bit-exactly: algo-dsp's Flanger.Process computes
// sample*(1-mix) + delayed*mix, so with mix=0 the wet path cancels out.
func TestFlangerMixZeroPassthrough(t *testing.T) {
	n := buildNode(t, "flanger", map[string]any{"mix": 0})
	buf := stereoFrames(256)
	for i := range buf {
		buf[i] = float32(i%7)*0.1 - 0.3
	}
	want := append([]float32(nil), buf...)
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	for i := range buf {
		if buf[i] != want[i] {
			t.Fatalf("sample %d: got %v want %v (mix=0 must be passthrough)", i, buf[i], want[i])
		}
	}
}

// TestFlangerImpulseModulation feeds one impulse into the L channel and
// locates it in the output. With mix=1, feedback=0, baseDelay=2 ms and
// depth=8 ms the impulse must reappear roughly baseDelay+depth/2 ~ 6 ms
// (about 149 samples at 24 kHz, the LFO is at mid-sweep when processing
// starts) later — deep inside the modulation sweep, not at the base delay
// (48 samples) — and never leak into R. A pure 2 ms delay would put the
// peak at frame 548; landing near 650 proves the depth modulation is live
// and measurable.
func TestFlangerImpulseModulation(t *testing.T) {
	n := buildNode(t, "flanger", map[string]any{
		"rateHz": 0.4, "depth": 0.008, "baseDelay": 0.002,
		"feedback": 0, "mix": 1,
	})
	buf := stereoFrames(8192)
	buf[1000] = 1 // impulse at L frame 500 (buffer index 1000)
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	peak, peakVal := 0, float64(-1)
	for j := range 8192 {
		if math.Abs(float64(buf[2*j])) > 0.5 {
			if math.Abs(float64(buf[2*j])) > peakVal {
				peak, peakVal = j, math.Abs(float64(buf[2*j]))
			}
		}
	}
	if peak < 600 || peak > 720 {
		t.Fatalf("impulse peak at L frame %d, want ~649 (6.2 ms = baseDelay 2 ms + LFO-mid depth 4.2 ms); base delay alone would be 548", peak)
	}
	if peakVal < 0.5 {
		t.Fatalf("impulse peak magnitude %v, want > 0.5", peakVal)
	}
	for j := range 8192 {
		if buf[2*j+1] != 0 {
			t.Fatalf("R frame %d: got %v want 0 (L impulse leaked into R)", j, buf[2*j+1])
		}
	}
}

// TestFlangerSinePitchPreserved verifies the flanger's delayed copy shifts a
// sine measurably (output differs from input) without changing its
// fundamental: the comb output is the same 440 Hz tone delayed by the LFO
// sweep.
func TestFlangerSinePitchPreserved(t *testing.T) {
	n := buildNode(t, "flanger", map[string]any{
		"rateHz": 0.4, "depth": 0.008, "baseDelay": 0.002,
		"feedback": 0.5, "mix": 1,
	})
	frames := 24000 // 1 s
	buf := stereoFrames(frames)
	tone := sineTone(440, frames)
	for j := range frames {
		buf[2*j] = float32(tone[j])
	}
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	out := make([]float64, frames)
	differ := false
	for j := range frames {
		out[j] = float64(buf[2*j])
		if buf[2*j] != float32(tone[j]) {
			differ = true
		}
	}
	if !differ {
		t.Fatal("flanger output identical to input: no measurable movement")
	}
	f0 := estimateF0(out)
	if f0 < 430 || f0 > 450 {
		t.Fatalf("flanged sine fundamental %v Hz, want ~440", f0)
	}
}

func TestFlangerLRSeparation(t *testing.T) {
	n := buildNode(t, "flanger", nil)
	buf := stereoFrames(4096)
	for j := range 4096 {
		buf[2*j] = 0.4 * float32(math.Sin(2*math.Pi*440*float64(j)/24000))
	}
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	for j := range 4096 {
		if buf[2*j+1] != 0 {
			t.Fatalf("R frame %d: got %v want 0", j, buf[2*j+1])
		}
	}
}

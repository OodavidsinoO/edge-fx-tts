package effects

import (
	"math"
	"testing"
)

func TestEQBuild(t *testing.T) {
	for _, typ := range []string{"highpass", "lowpass", "peaking"} {
		buildNode(t, "eq", map[string]any{"type": typ, "freq": 1000, "q": 0.707, "gain": 3})
	}
}

func TestEQInvalidParams(t *testing.T) {
	cases := []map[string]any{
		{"type": "bandpass"},
		{"freq": 0},
		{"freq": 20000}, // >= Nyquist at 24 kHz
		{"q": 0},
	}
	for _, p := range cases {
		if _, err := registry["eq"](24000, p); err == nil {
			t.Errorf("params %v: want error", p)
		}
	}
}

func TestEQHighpassRejectsDC(t *testing.T) {
	n := buildNode(t, "eq", map[string]any{"type": "highpass", "freq": 1000, "q": 0.707})
	buf := stereoFrames(4096)
	for i := range buf {
		buf[i] = 1.0 // DC
	}
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	// A highpass settles to zero on DC. The biquad state decays with the
	// filter's time constant; by the end of the buffer it must be ~0.
	for i := 3000; i < len(buf); i++ {
		if got := float32(math.Abs(float64(buf[i]))); got > 1e-3 {
			t.Fatalf("sample %d: got %v, want ~0 (highpass rejects DC)", i, got)
		}
	}
}

func TestEQLowpassPassesDC(t *testing.T) {
	n := buildNode(t, "eq", map[string]any{"type": "lowpass", "freq": 1000, "q": 0.707})
	buf := stereoFrames(4096)
	for i := range buf {
		buf[i] = 1.0 // DC
	}
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	// A lowpass passes DC with unity gain.
	for i := 1000; i < len(buf); i++ {
		if got := float32(math.Abs(float64(buf[i]) - 1)); got > 1e-3 {
			t.Fatalf("sample %d: got %v, want 1.0 (lowpass passes DC)", i, buf[i])
		}
	}
}

func TestEQPeakingBoostsBand(t *testing.T) {
	n := buildNode(t, "eq", map[string]any{"type": "peaking", "freq": 1000, "q": 0.707, "gain": 12})
	buf := stereoFrames(8192)
	for i := range buf {
		buf[i] = float32(math.Sin(2 * math.Pi * 1000 * float64(i/2) / 24000))
	}
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	// +12 dB peaking at 1 kHz: steady-state peak must be ~4x the input.
	peak := float32(0)
	for i := 4000; i < len(buf); i++ {
		if a := float32(math.Abs(float64(buf[i]))); a > peak {
			peak = a
		}
	}
	if peak < 3.0 || peak > 5.0 {
		t.Fatalf("peaking peak %v, want ~4 (12 dB boost)", peak)
	}
}

func TestEQZeroAllocs(t *testing.T) {
	n := buildNode(t, "eq", map[string]any{"type": "peaking", "freq": 1000, "gain": 3})
	assertZeroAllocs(t, n, stereoFrames(4096))
}

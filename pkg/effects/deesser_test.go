package effects

import (
	"math"
	"testing"
)

func TestDeEsserBuild(t *testing.T) {
	buildNode(t, "deesser", nil)
}

func TestDeEsserInvalidParams(t *testing.T) {
	cases := []map[string]any{
		{"frequency": 500},   // below 1000 Hz minimum
		{"frequency": 20000}, // >= Nyquist at 24 kHz
		{"q": 0},
		{"ratio": 0.5},
		{"attack": 0},
		{"release": 0},
	}
	for _, p := range cases {
		if _, err := registry["deesser"](24000, p); err == nil {
			t.Errorf("params %v: want error", p)
		}
	}
}

func TestDeEsserAttenuatesSibilanceBand(t *testing.T) {
	// A loud 6 kHz tone (the default detection center) must be attenuated.
	n := buildNode(t, "deesser", map[string]any{
		"frequency": 6000, "q": 1.5, "threshold": -20, "ratio": 2,
		"attack": 1, "release": 50,
	})
	buf := stereoFrames(8192)
	// Each channel's instance sees every other buffer sample (L at 0,2,4,...),
	// so the per-channel sample index is i/2. Generate a 6 kHz tone at the
	// true 24 kHz per-channel rate.
	for i := range buf {
		s := i / 2
		buf[i] = float32(0.5 * math.Sin(2*math.Pi*6000*float64(s)/24000))
	}
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	// Steady-state peak must be well below the 0.5 input peak.
	peak := float32(0)
	for i := 4000; i < len(buf); i++ {
		if a := float32(math.Abs(float64(buf[i]))); a > peak {
			peak = a
		}
	}
	if peak > 0.2 {
		t.Fatalf("sibilance peak %v, want < 0.2 (loud 6 kHz must be de-essed)", peak)
	}
}

func TestDeEsserQuietSignalUntouched(t *testing.T) {
	n := buildNode(t, "deesser", map[string]any{
		"frequency": 6000, "q": 1.5, "threshold": -20, "ratio": 2,
		"attack": 1, "release": 50,
	})
	buf := stereoFrames(8192)
	for i := range buf {
		buf[i] = float32(0.01 * math.Sin(2*math.Pi*6000*float64(i)/24000))
	}
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	// -40 dBFS sibilance is far below the -20 dB threshold: passthrough.
	for i := range buf {
		if got := float32(math.Abs(float64(buf[i]))); got > 0.011 {
			t.Fatalf("sample %d: got %v, want passthrough of 0.01 tone", i, got)
		}
	}
}

func TestDeEsserZeroAllocs(t *testing.T) {
	n := buildNode(t, "deesser", nil)
	assertZeroAllocs(t, n, stereoFrames(4096))
}

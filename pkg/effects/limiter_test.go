package effects

import "testing"

func TestLimiterBuild(t *testing.T) {
	buildNode(t, "limiter", nil)
}

func TestLimiterInvalidParams(t *testing.T) {
	cases := []map[string]any{
		{"release": 0},
	}
	for _, p := range cases {
		if _, err := registry["limiter"](24000, p); err == nil {
			t.Errorf("params %v: want error", p)
		}
	}
}

func TestLimiterClipsToThreshold(t *testing.T) {
	n := buildNode(t, "limiter", map[string]any{"threshold": -6, "release": 100})
	buf := stereoFrames(8192)
	for i := range buf {
		buf[i] = 1.0 // 0 dBFS, 6 dB above the -6 dB ceiling
	}
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	// -6 dBFS = 10^(-6/20) ≈ 0.5012. The limiter's fast attack (0.1 ms)
	// must pin the steady-state output at the ceiling.
	for i := 1000; i < len(buf); i++ {
		if got := buf[i]; got > 0.55 || got < 0.45 {
			t.Fatalf("sample %d: got %v, want ~0.5012 (limited to -6 dBFS)", i, got)
		}
	}
}

func TestLimiterBelowThresholdUntouched(t *testing.T) {
	n := buildNode(t, "limiter", map[string]any{"threshold": -1, "release": 100})
	buf := stereoFrames(4096)
	for i := range buf {
		buf[i] = 0.1 // -20 dBFS, far below the -1 dB ceiling
	}
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	for i := range buf {
		if buf[i] != 0.1 {
			t.Fatalf("sample %d: got %v want 0.1 (below ceiling must pass)", i, buf[i])
		}
	}
}

func TestLimiterZeroAllocs(t *testing.T) {
	n := buildNode(t, "limiter", nil)
	assertZeroAllocs(t, n, stereoFrames(4096))
}

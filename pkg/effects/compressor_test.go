package effects

import "testing"

func TestCompressorBuild(t *testing.T) {
	buildNode(t, "compressor", nil)
}

func TestCompressorInvalidParams(t *testing.T) {
	cases := []map[string]any{
		{"ratio": 0.5},
		{"attack": 0},
		{"release": 0},
		{"sidechainLowCut": -1},
		{"sidechainLowCut": 20000}, // >= Nyquist at 24 kHz
		{"sidechainLowCut": 1000, "sidechainHighCut": 500},
	}
	for _, p := range cases {
		if _, err := registry["compressor"](24000, p); err == nil {
			t.Errorf("params %v: want error", p)
		}
	}
}

func TestCompressorReducesLoudSignal(t *testing.T) {
	// Hard knee, no makeup, ratio 2: a 0 dBFS signal 6 dB above the -6 dB
	// threshold must be compressed to -3 dBFS (0.7079).
	n := buildNode(t, "compressor", map[string]any{
		"threshold": -6, "ratio": 2, "knee": 0, "makeupGain": 0, "attack": 1, "release": 50,
	})
	buf := stereoFrames(4096)
	for i := range buf {
		buf[i] = 1.0
	}
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	// Envelope settles within ~10 ms (240 samples at 24 kHz); the
	// steady-state output must be 0.7079 (2:1 compression of 6 dB overshoot).
	for i := 1000; i < len(buf); i++ {
		if got := buf[i]; got > 0.75 || got < 0.65 {
			t.Fatalf("sample %d: got %v, want ~0.7079", i, got)
		}
	}
}

func TestCompressorBelowThresholdUntouched(t *testing.T) {
	n := buildNode(t, "compressor", map[string]any{
		"threshold": -20, "ratio": 2, "knee": 0, "makeupGain": 0, "attack": 1, "release": 50,
	})
	buf := stereoFrames(4096)
	for i := range buf {
		buf[i] = 0.01 // -40 dBFS, well below -20 dB threshold
	}
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	for i := range buf {
		if buf[i] != 0.01 {
			t.Fatalf("sample %d: got %v want 0.01 (below threshold must pass)", i, buf[i])
		}
	}
}

func TestCompressorZeroAllocs(t *testing.T) {
	n := buildNode(t, "compressor", nil)
	assertZeroAllocs(t, n, stereoFrames(4096))
}

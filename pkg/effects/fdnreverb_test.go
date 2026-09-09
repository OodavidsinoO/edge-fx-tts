package effects

import "testing"

func TestFDNReverbBuild(t *testing.T) {
	buildNode(t, "fdnreverb", nil)
}

func TestFDNReverbInvalidParams(t *testing.T) {
	cases := []map[string]any{
		{"rt60": -1.0},
		{"damp": 1.5},
		{"preDelay": -0.1},
		{"wet": -1.0},
	}
	for _, p := range cases {
		if _, err := registry["fdnreverb"](24000, p); err == nil {
			t.Errorf("params %v: want error", p)
		}
	}
}

func TestFDNReverbWetZeroIsDryPassthrough(t *testing.T) {
	n := buildNode(t, "fdnreverb", map[string]any{"wet": 0, "dry": 1})
	buf := stereoFrames(64)
	for i := range buf {
		buf[i] = float32(i%7) * 0.1
	}
	want := append([]float32(nil), buf...)
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	for i := range buf {
		if buf[i] != want[i] {
			t.Fatalf("sample %d: got %v want %v (wet=0 must be dry passthrough)", i, buf[i], want[i])
		}
	}
}

func TestFDNReverbImpulseTails(t *testing.T) {
	n := buildNode(t, "fdnreverb", map[string]any{"wet": 1, "dry": 0, "preDelay": 0.01, "rt60": 0.5})
	buf := stereoFrames(4096)
	buf[0] = 1 // impulse in L
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	// Pre-delay 0.01 s = 240 samples; the shortest FDN line then echoes the
	// impulse ~836 samples later, so a non-zero tail must appear well after
	// the pre-delay window.
	tail := false
	for i := 600; i < len(buf); i += 2 {
		if buf[i] != 0 {
			tail = true
			break
		}
	}
	if !tail {
		t.Fatal("expected non-zero reverb tail after pre-delay")
	}
}

func TestFDNReverbZeroAllocs(t *testing.T) {
	n := buildNode(t, "fdnreverb", nil)
	assertZeroAllocs(t, n, stereoFrames(4096))
}

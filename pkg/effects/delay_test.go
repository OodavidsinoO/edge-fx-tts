package effects

import "testing"

func TestDelayBuild(t *testing.T) {
	buildNode(t, "delay", nil)
}

func TestDelayInvalidParams(t *testing.T) {
	cases := []map[string]any{
		{"time": 0},
		{"time": 3.0},
		{"feedback": -0.1},
		{"feedback": 1.0},
		{"mix": 1.5},
	}
	for _, p := range cases {
		if _, err := registry["delay"](24000, p); err == nil {
			t.Errorf("params %v: want error", p)
		}
	}
}

func TestDelayMixZeroPassthrough(t *testing.T) {
	n := buildNode(t, "delay", map[string]any{"mix": 0})
	buf := stereoFrames(256)
	for i := range buf {
		buf[i] = float32(i%5) * 0.2
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

func TestDelayImpulseEcho(t *testing.T) {
	n := buildNode(t, "delay", map[string]any{"time": 0.1, "mix": 1, "feedback": 0})
	buf := stereoFrames(8192)
	buf[0] = 1 // impulse in L
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	// 0.1 s at 24 kHz = 2400 samples. The echo must land at L sample 2400
	// (buffer index 4800) and nowhere else (feedback=0, mix=1).
	for i := 0; i < len(buf); i += 2 {
		want := float32(0)
		if i == 4800 {
			want = 1
		}
		if buf[i] != want {
			t.Fatalf("L sample %d: got %v want %v", i, buf[i], want)
		}
	}
}

func TestDelayFeedbackRepeats(t *testing.T) {
	n := buildNode(t, "delay", map[string]any{"time": 0.1, "mix": 1, "feedback": 0.5})
	buf := stereoFrames(8192)
	buf[0] = 1
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	// With feedback 0.5 the echo at L sample 2400 (buffer 4800) must be 1.0
	// and the repeat at L sample 4800 (buffer 9600) must be 0.5.
	if got := buf[4800]; got != 1 {
		t.Fatalf("first echo: got %v want 1", got)
	}
	if got := buf[9600]; got != 0.5 {
		t.Fatalf("second echo: got %v want 0.5", got)
	}
}

func TestDelayZeroAllocs(t *testing.T) {
	n := buildNode(t, "delay", nil)
	assertZeroAllocs(t, n, stereoFrames(4096))
}

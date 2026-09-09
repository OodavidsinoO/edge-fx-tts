package effects

import "testing"

func TestChorusBuild(t *testing.T) {
	buildNode(t, "chorus", nil)
}

func TestChorusInvalidParams(t *testing.T) {
	cases := []map[string]any{
		{"speedHz": 0},
		{"depth": -0.1},
		{"baseDelay": 0},
		{"stages": 0},
		{"mix": 1.5},
	}
	for _, p := range cases {
		if _, err := registry["chorus"](24000, p); err == nil {
			t.Errorf("params %v: want error", p)
		}
	}
}

func TestChorusMixZeroPassthrough(t *testing.T) {
	n := buildNode(t, "chorus", map[string]any{"mix": 0})
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

func TestChorusDCPassthroughAtFullWet(t *testing.T) {
	n := buildNode(t, "chorus", map[string]any{"mix": 1})
	buf := stereoFrames(1024)
	for i := range buf {
		buf[i] = 1.0
	}
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	// The delay line (max ~504 samples) must be fully primed with DC by the
	// time each channel's instance has seen 504 samples. With L/R split, the
	// L instance sees samples at buffer indices 0,2,4,... so it is primed
	// by buffer index ~1008; check from 1100.
	for i := 1100; i < len(buf); i++ {
		if buf[i] != 1.0 {
			t.Fatalf("sample %d: got %v want 1.0 (DC through modulated delay)", i, buf[i])
		}
	}
}

func TestChorusZeroAllocs(t *testing.T) {
	n := buildNode(t, "chorus", nil)
	assertZeroAllocs(t, n, stereoFrames(4096))
}

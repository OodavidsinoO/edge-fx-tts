package effects

import (
	"math"
	"testing"
)

// The wsola node necessarily allocates on every ProcessInPlace call:
// SpectralPitchShifter.Process returns a freshly allocated slice by
// contract. No assertZeroAllocs test here (see the allocation exception note
// in wsola.go); behavior is asserted instead.

func TestWsolaBuild(t *testing.T) {
	n := buildNode(t, "wsola", nil)
	buf := stereoFrames(64)
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatalf("process: %v", err)
	}
	if err := n.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestWsolaBuildCustom(t *testing.T) {
	n := buildNode(t, "wsola", map[string]any{"semitones": -1, "frameSize": 2048})
	buf := stereoFrames(2048)
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatalf("process: %v", err)
	}
}

func TestWsolaInvalidParams(t *testing.T) {
	cases := []map[string]any{
		{"semitones": math.NaN()},
		{"semitones": 25},  // ratio 2^(25/12) > 4, out of algo-dsp range
		{"semitones": -25}, // ratio 2^(-25/12) < 0.25
		{"frameSize": 0},
		{"frameSize": 32},  // < 64
		{"frameSize": 100}, // not a power of two
	}
	for _, p := range cases {
		if _, err := registry["wsola"](24000, p); err == nil {
			t.Errorf("params %v: want error", p)
		}
	}
}

// TestWsolaZeroSemitonesPassthrough verifies semitones=0 is bit-exact
// passthrough: the shifter's identity fast-path returns a copy of the input
// when the ratio is 1.
func TestWsolaZeroSemitonesPassthrough(t *testing.T) {
	n := buildNode(t, "wsola", nil) // semitones defaults to 0
	buf := stereoFrames(4096)
	for i := range buf {
		buf[i] = float32(i%11)*0.05 - 0.25
	}
	want := append([]float32(nil), buf...)
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	for i := range buf {
		if buf[i] != want[i] {
			t.Fatalf("sample %d: got %v want %v (semitones=0 must be passthrough)", i, buf[i], want[i])
		}
	}
}

// TestWsolaDownshiftHalfStep feeds a 440 Hz sine through a -1 semitone shift
// and expects the fundamental to land at 440*2^(-1/12) = 415.3 Hz, about
// 5.6% down. The bin-shifting path realizes the requested ratio exactly for
// small shifts (|1-ratio| = 0.056 < 0.15), so the drop must be measurable
// and not explained by windowing or chunk seams (median F0 estimate is
// robust to the odd glitched interval).
func TestWsolaDownshiftHalfStep(t *testing.T) {
	n := buildNode(t, "wsola", map[string]any{"semitones": -1, "frameSize": 2048})
	frames := 48000
	tone := sineTone(440, frames)
	out := make([]float64, 0, frames)
	for off := 0; off < frames; off += 4096 {
		sz := 4096
		if off+sz > frames {
			sz = frames - off
		}
		buf := stereoFrames(sz)
		for j := range sz {
			buf[2*j] = float32(tone[off+j])
		}
		if err := n.ProcessInPlace(buf); err != nil {
			t.Fatalf("process chunk at %d: %v", off, err)
		}
		for j := range sz {
			out = append(out, float64(buf[2*j]))
		}
	}
	if err := n.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	f0 := estimateF0(out[20000:])
	if f0 < 413 || f0 > 417.5 {
		t.Fatalf("shifted fundamental %v Hz, want ~415.3 (440 * 2^-1/12)", f0)
	}
}

func TestWsolaLRSeparation(t *testing.T) {
	n := buildNode(t, "wsola", map[string]any{"semitones": -1})
	frames := 8192
	buf := stereoFrames(frames)
	tone := sineTone(440, frames)
	for j := range frames {
		buf[2*j] = float32(tone[j])
	}
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	seenL := false
	for j := range frames {
		if buf[2*j] != 0 {
			seenL = true
		}
		if buf[2*j+1] != 0 {
			t.Fatalf("R frame %d: got %v want 0 (zeros must stay zeros)", j, buf[2*j+1])
		}
	}
	if !seenL {
		t.Fatal("L channel silent: shifter did not process the tone")
	}
}

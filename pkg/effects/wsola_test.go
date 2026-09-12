package effects

import (
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/OodavidsinoO/edge-fx-tts/internal/decode"
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

// TestWsolaChunkContinuity feeds the same input through the node once as a
// single call and once in 4096-frame chunks, then asserts the two outputs
// agree sample-by-sample within a tight bound. Before the overlap-carry
// streaming fix, every chunk boundary produced a transient spike of up to
// ~1.2e4x (maxdiff 12260 at the first boundary), because the shifter's
// one-shot STFT restarted its OLA normalization with a near-zero window norm
// at each chunk head. The overlap carry keeps the window coverage
// continuous, so the chunked output must track the one-shot output closely.
func TestWsolaChunkContinuity(t *testing.T) {
	const (
		frames  = 48000
		chunk   = 4096
		maxDiff = 1.0 // far below the pre-fix 12260 spike
	)
	tone := sineTone(440, frames)

	oneshot := buildNode(t, "wsola", map[string]any{"semitones": -1, "frameSize": 2048})
	buf := stereoFrames(frames)
	for j := range frames {
		buf[2*j] = float32(tone[j])
	}
	if err := oneshot.ProcessInPlace(buf); err != nil {
		t.Fatalf("one-shot process: %v", err)
	}
	if err := oneshot.Close(); err != nil {
		t.Fatalf("one-shot Close: %v", err)
	}

	chunked := buildNode(t, "wsola", map[string]any{"semitones": -1, "frameSize": 2048})
	got := make([]float64, frames)
	for off := 0; off < frames; off += chunk {
		sz := chunk
		if off+sz > frames {
			sz = frames - off
		}
		cb := stereoFrames(sz)
		for j := range sz {
			cb[2*j] = float32(tone[off+j])
		}
		if err := chunked.ProcessInPlace(cb); err != nil {
			t.Fatalf("chunked process at %d: %v", off, err)
		}
		for j := range sz {
			got[off+j] = float64(cb[2*j])
		}
	}
	if err := chunked.Close(); err != nil {
		t.Fatalf("chunked Close: %v", err)
	}

	md, mi := 0.0, 0
	for i := range frames {
		d := math.Abs(got[i] - float64(buf[2*i]))
		if d > md {
			md, mi = d, i
		}
	}
	if md > maxDiff {
		t.Fatalf("chunked vs one-shot maxdiff %.4f at frame %d, want <= %.1f (chunk seams must not spike)",
			md, mi, maxDiff)
	}
}

// TestWsolaStreamClickFree guards against the per-call phase reset that
// produced audible pops on real audio. The stock algo-dsp
// SpectralPitchShifter calls Reset() (zeroing prevPhase/sumPhase) at the top
// of every Process call, so a streamed-in-chunks signal restarts its
// phase-vocoder state at each chunk seam and emits a transient pop there (we
// measured 265 pops in a 10s clip). The fork keeps phase state live across
// Process calls. Single-tone sines are phase-coherent enough to mask this,
// so we drive the real offline fixture (internal/decode/testdata/sample.mp3)
// and assert the chunked stream stays click-free: no adjacent-sample jump
// above a fraction of full scale that the one-shot pass also stays under.
func TestWsolaStreamClickFree(t *testing.T) {
	const (
		chunk  = 4096
		maxAbs = 0.30 // clicks seen pre-fix were 0.34-0.62 full scale
	)
	// Decode the offline fixture via the package-under-test's streamed
	// decoder (same code path the CLI uses).
	f, err := os.Open(filepath.Join("..", "..", "internal", "decode", "testdata", "sample.mp3"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	dec, err := decode.NewMinimp3Decoder(f)
	if err != nil {
		t.Fatalf("decoder: %v", err)
	}
	var mono []float32
	b := make([]float32, 4096)
	for {
		n, err := dec.Read(b)
		mono = append(mono, b[:n]...)
		if err == io.EOF || err != nil {
			break
		}
	}

	oneshot := buildNode(t, "wsola", map[string]any{"semitones": -1, "frameSize": 2048})
	buf := stereoFrames(len(mono))
	for j, v := range mono {
		buf[2*j] = v
	}
	if err := oneshot.ProcessInPlace(buf); err != nil {
		t.Fatalf("one-shot process: %v", err)
	}

	chunked := buildNode(t, "wsola", map[string]any{"semitones": -1, "frameSize": 2048})
	got := make([]float32, len(mono))
	for off := 0; off < len(mono); off += chunk {
		sz := chunk
		if off+sz > len(mono) {
			sz = len(mono) - off
		}
		cb := stereoFrames(sz)
		for j := range sz {
			cb[2*j] = mono[off+j]
		}
		if err := chunked.ProcessInPlace(cb); err != nil {
			t.Fatalf("chunked process at %d: %v", off, err)
		}
		for j := range sz {
			got[off+j] = cb[2*j]
		}
	}

	// A phase-reset click is a step discontinuity far exceeding full scale;
	// it shows up in the chunked stream but not the one-shot reference.
	// Count adjacent-sample jumps above maxAbs in each.
	oneshotClicks := 0
	for i := 1; i < len(mono); i++ {
		s := math.Abs(float64(buf[2*i]) - float64(buf[2*(i-1)]))
		if s > maxAbs {
			oneshotClicks++
		}
	}
	chunkedClicks := 0
	for i := 1; i < len(mono); i++ {
		s := math.Abs(float64(got[i]) - float64(got[i-1]))
		if s > maxAbs {
			chunkedClicks++
		}
	}
	// Allow a couple of source transients the one-shot shares; the pre-fix
	// chunked path produced dozens of seam clicks the one-shot did not.
	if chunkedClicks > oneshotClicks+3 {
		t.Fatalf("chunked stream click count %d > one-shot %d + 3: phase reset at chunk seams",
			chunkedClicks, oneshotClicks)
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

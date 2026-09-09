package effects

import (
	"math"
	"strings"
	"testing"
)

func TestFormantBuild(t *testing.T) {
	// Defaults (shift 1.0, frameSize 2048, hops 4, lifter 0.2) build and run.
	n := buildNode(t, "formant", nil)
	buf := stereoFrames(64)
	for i := range buf {
		buf[i] = float32((i%7)-3) * 0.1
	}
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFormantInvalidParams(t *testing.T) {
	cases := []map[string]any{
		{"shift": 0},
		{"shift": -1},
		{"frameSize": 1000},
		{"frameSize": 3},
		{"frameSize": 0},
		{"frameSize": 15},
		{"lifter": -0.1},
		{"lifter": 1.5},
		{"hops": 0},
		{"hops": 3, "frameSize": 512}, // 512 % 3 != 0
		{"hops": 2048, "frameSize": 512},
	}
	for _, p := range cases {
		_, err := registry["formant"](24000, p)
		if err == nil {
			t.Errorf("params %v: want error", p)
			continue
		}
		if !strings.HasPrefix(err.Error(), "effects: formant:") {
			t.Errorf("params %v: error %q missing effects: formant: prefix", p, err)
		}
	}
}

func TestFormantIdentity(t *testing.T) {
	// shift 1.0 is the documented bypass: output must equal input exactly.
	n := buildNode(t, "formant", map[string]any{"shift": 1.0, "frameSize": 512})
	buf := stereoFrames(2048)
	rng := newRng(42)
	for i := range buf {
		buf[i] = float32(rng())
	}
	want := make([]float32, len(buf))
	copy(want, buf)
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	for i := range buf {
		if buf[i] != want[i] {
			t.Fatalf("sample %d: got %v want %v (shift=1 must be passthrough)", i, buf[i], want[i])
		}
	}
}

// newRng returns a deterministic LCG on [0, 1).
func newRng(seed uint64) func() float64 {
	s := seed
	return func() float64 {
		s = s*6364136223846793005 + 1442695040888963407
		return float64(s>>33) / float64(1<<31)
	}
}

// harmonicSignal synthesizes 20 harmonic sines with an envelope peaked near
// the 4th harmonic (approx 900 Hz) and a guaranteed floor so no harmonic
// vanishes.
func harmonicSignal(n, f0 int, fs float64) []float64 {
	x := make([]float64, n)
	for i := range n {
		v := 0.0
		for h := 1; h <= 20; h++ {
			a := 0.5 + 2.0*math.Exp(-math.Pow(float64(h)*float64(f0)-900, 2)/(2*250*250))
			v += a * math.Sin(2*math.Pi*float64(h)*float64(f0)*float64(i)/fs+float64(h)*0.7)
		}
		x[i] = v
	}
	return x
}

// bandEnergy returns the summed squared DFT magnitudes of harmonics in
// [hFrom, hTo] over a 0.25 s coherent window starting at start.
func bandEnergy(x []float64, f0, hFrom, hTo, start int, fs float64) float64 {
	n := len(x) - start
	if n > int(fs/4) {
		n = int(fs / 4)
	}
	per := fs / float64(f0)
	k0 := n - n%int(per) // whole periods for coherent peaks
	if k0 == 0 {
		k0 = n
	}
	e := 0.0
	for h := hFrom; h <= hTo; h++ {
		freq := float64(h) * float64(f0)
		re, im := 0.0, 0.0
		for i := range k0 {
			ph := 2 * math.Pi * freq * float64(i) / fs
			re += x[start+i] * math.Cos(ph)
			im -= x[start+i] * math.Sin(ph)
		}
		e += re*re + im*im
	}
	return e
}

// formantF0Lag returns the smallest autocorrelation lag (in samples) in the
// [100, 400] Hz range whose correlation reaches 90% of the range maximum.
// Pure harmonic signals have identical correlation at every multiple of the
// period, so using the first strong peak yields the fundamental period.
func formantF0Lag(x []float64, fs float64) int {
	lo, hi := int(fs/400), int(fs/100)
	if hi > len(x) {
		hi = len(x)
	}
	best := lo
	var bestA float64
	for lag := lo; lag < hi; lag++ {
		s := 0.0
		for i := 0; i < len(x)-lag; i++ {
			s += x[i] * x[i+lag]
		}
		if s > bestA {
			bestA = s
			best = lag
		}
	}
	for lag := lo; lag < hi; lag++ {
		s := 0.0
		for i := 0; i < len(x)-lag; i++ {
			s += x[i] * x[i+lag]
		}
		if s >= 0.9*bestA {
			return lag
		}
	}
	return best
}

func TestFormantF0AndEnvelope(t *testing.T) {
	const (
		fs  = 24000.0
		f0  = 200
		sec = 2
	)
	frameSize := 1024
	x := harmonicSignal(int(fs)*sec, f0, fs)
	buf := make([]float32, 2*len(x))

	// measure runs one shift and returns the estimated f0 lag and the LF/HF
	// band energy ratio measured on the steady-state mid-section.
	measure := func(shift float64) (lag int, lf, hf float64, hasNaN bool) {
		for i := range x {
			buf[2*i] = float32(x[i])
			buf[2*i+1] = float32(x[i])
		}
		n := buildNode(t, "formant", map[string]any{"shift": shift, "frameSize": frameSize})
		if err := n.ProcessInPlace(buf); err != nil {
			t.Fatalf("shift %v: %v", shift, err)
		}
		start := 3 * frameSize
		end := len(x) - frameSize
		for i := start; i < end; i++ {
			if math.IsNaN(float64(buf[2*i])) {
				hasNaN = true
			}
		}
		mid := make([]float64, end-start)
		for i := range mid {
			mid[i] = float64(buf[2*(start+i)])
		}
		return formantF0Lag(mid, fs), bandEnergy(mid, f0, 1, 4, 0, fs), bandEnergy(mid, f0, 6, 10, 0, fs), hasNaN
	}

	// shift 1.0 is a passthrough: the measurement path itself is calibrated
	// on data identical to the input.
	_, inLF, inHF, _ := measure(1.0)
	inRatio := inHF / inLF

	upLag, upLF, upHF, upNaN := measure(1.6)
	downLag, downLF, downHF, downNaN := measure(0.6)

	for _, c := range []struct {
		name string
		lag  int
		nan  bool
	}{
		{"up", upLag, upNaN},
		{"down", downLag, downNaN},
	} {
		if c.nan {
			t.Errorf("shift %s: NaN in output", c.name)
		}
		if math.Abs(float64(c.lag)-float64(fs/float64(f0))) > 0.05*float64(fs/f0) {
			t.Errorf("shift %s: f0 lag %d (want ~%v), f0 must stay unchanged", c.name, c.lag, fs/float64(f0))
		}
	}

	// Envelope must move up with shift > 1 and down with shift < 1, so the
	// HF/LF energy ratio must rise and fall accordingly.
	if up := upHF / upLF; up < 2*inRatio {
		t.Errorf("shift 1.6: HF/LF ratio %v, want > %v (envelope should move up)", up, 2*inRatio)
	}
	if down := downHF / downLF; down > inRatio/2 {
		t.Errorf("shift 0.6: HF/LF ratio %v, want < %v (envelope should move down)", down, inRatio/2)
	}
}

func TestFormantNoCrash(t *testing.T) {
	// Silence, noise, and tiny buffers must not panic or error; the node must
	// keep working across calls (state continuity).
	build := func() Node {
		return buildNode(t, "formant", map[string]any{"shift": 1.5, "frameSize": 512})
	}

	n := build()
	silence := stereoFrames(96)
	if err := n.ProcessInPlace(silence); err != nil {
		t.Fatal(err)
	}
	buf := stereoFrames(96)
	rng := newRng(7)
	for i := range buf {
		buf[i] = float32(rng()*2 - 1)
	}
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err)
	}
	for _, v := range buf {
		if math.IsNaN(float64(v)) {
			t.Fatal("NaN in noise output")
		}
	}
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatal(err) // second call on the same state
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}

	n2 := build()
	tiny := stereoFrames(16)
	for i := range tiny {
		tiny[i] = float32(rng()*2 - 1)
	}
	if err := n2.ProcessInPlace(tiny); err != nil {
		t.Fatal(err)
	}
	for _, v := range tiny {
		if math.IsNaN(float64(v)) {
			t.Fatal("NaN in tiny-buffer output")
		}
	}
}

func TestFormantZeroAllocs(t *testing.T) {
	// shift 1.1 runs the full DSP path (not the bypass): the frame loop must
	// be allocation-free.
	n := buildNode(t, "formant", map[string]any{"shift": 1.1, "frameSize": 512})
	assertZeroAllocs(t, n, stereoFrames(4096))
}

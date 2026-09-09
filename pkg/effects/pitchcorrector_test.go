package effects

import (
	"math"
	"math/rand/v2"
	"sort"
	"testing"
)

// PitchCorrector is a block processor: algo-dsp's PitchCorrector queues input
// internally, emits blockSize corrected samples per blockSize fed, and adds a
// fixed latency of one block plus the seam crossfade. The node additionally
// primes one blockSize of zeros so pops before the first block is complete
// never underrun. The tests below never assert that ProcessInPlace is
// allocation-free: PitchProcessor.Process returns a freshly allocated slice
// by contract, so the hot path legitimately allocates (see the allocation
// exception note in pitchcorrector.go). Behavior is asserted instead.

const (
	pcSampleRate   = 24000.0
	pcDefaultBlock = 2048
	// pcNodeOffset is the node's total added latency at the default block
	// size and 24 kHz: one primed block (2048) plus the corrector's
	// block + 5 ms crossfade latency (2168). The bypass tests locate it
	// precisely via passthroughDelay; the f0 assertions skip a generous
	// warmup instead.
	pcNodeOffset = 4216
)

// sineTone returns n mono samples of a pure sine at freq (Hz) at 24 kHz.
// A pure fundamental keeps zero-crossing estimates unambiguous and gives the
// YIN detector near-perfect confidence.
func sineTone(freq float64, n int) []float64 {
	out := make([]float64, n)
	for i := range n {
		out[i] = 0.4 * math.Sin(2*math.Pi*freq*float64(i)/pcSampleRate)
	}
	return out
}

// noisyTone returns sineTone plus deterministic Gaussian noise, used by the
// confidence-gate test to lower the detector's confidence to ~0.9.
func noisyTone(freq float64, n int, sigma float64) []float64 {
	rng := rand.New(rand.NewPCG(7, 0x9e3779b97f4a7c15))
	out := sineTone(freq, n)
	for i := range out {
		out[i] += rng.NormFloat64() * sigma
	}
	return out
}

// mixedChunks partitions n samples into mixed-size chunks (both sub-block and
// multi-block) so the node's per-channel accumulation across calls is
// exercised. Always sums exactly to n.
func mixedChunks(n int) []int {
	pattern := []int{300, 4096, 64, 1000, 512, 2048, 256, 4096}
	var chunks []int
	for done := 0; done < n; {
		sz := pattern[done%len(pattern)]
		if done+sz > n {
			sz = n - done
		}
		chunks = append(chunks, sz)
		done += sz
	}
	return chunks
}

// processTone builds a pitchcorrector node, runs tone in the L channel (R is
// silence) through it in mixed-size chunks, and returns the concatenated L
// output. The node is closed before returning.
func processTone(t *testing.T, params map[string]any, tone []float64) []float32 {
	t.Helper()
	n := buildNode(t, "pitchcorrector", params)
	var out []float32
	for _, sz := range mixedChunks(len(tone)) {
		buf := stereoFrames(sz)
		off := len(out)
		for i := range sz {
			buf[2*i] = float32(tone[off+i])
		}
		if err := n.ProcessInPlace(buf); err != nil {
			t.Fatalf("process chunk at %d: %v", off, err)
		}
		for i := range sz {
			out = append(out, buf[2*i])
		}
	}
	if err := n.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return out
}

// estimateF0 returns the fundamental frequency of mono by counting
// positive-going zero crossings with linear sub-sample interpolation. The
// median crossing interval is used so a single glitched interval (e.g. a
// block seam) cannot skew the estimate.
func estimateF0(mono []float64) float64 {
	var crossings []float64
	for i := 1; i < len(mono); i++ {
		if mono[i-1] <= 0 && mono[i] > 0 {
			a, b := mono[i-1], mono[i]
			frac := 0.0
			if b != a {
				frac = -a / (b - a)
			}
			crossings = append(crossings, float64(i-1)+frac)
		}
	}
	if len(crossings) < 4 {
		return 0
	}
	intervals := make([]float64, 0, len(crossings)-1)
	for i := 1; i < len(crossings); i++ {
		intervals = append(intervals, crossings[i]-crossings[i-1])
	}
	sort.Float64s(intervals)
	return pcSampleRate / intervals[len(intervals)/2]
}

// passthroughDelay finds the smallest sample offset at which the node's L
// output exactly equals the fed input — valid only for a node whose
// correction never engages (amount=0, or a confidence floor no block
// reaches), where the corrector bypasses its shifter bit-exactly.
func passthroughDelay(t *testing.T, out []float32, in []float64, blockSize int) int {
	t.Helper()
	const probe = 4096
	if len(out) < blockSize+probe || len(in) < probe {
		t.Fatalf("passthroughDelay: buffers too short (out %d, in %d)", len(out), len(in))
	}
	for L := blockSize; L+probe <= len(out) && L < blockSize+3000; L++ {
		ok := true
		for j := range probe {
			if out[L+j] != float32(in[j]) {
				ok = false
				break
			}
		}
		if ok {
			return L
		}
	}
	t.Fatalf("passthroughDelay: no exact passthrough offset found")
	return 0
}

func TestPitchCorrectorBuild(t *testing.T) {
	n := buildNode(t, "pitchcorrector", nil)
	// A short buffer exercises the block-accumulation path without error.
	buf := stereoFrames(64)
	if err := n.ProcessInPlace(buf); err != nil {
		t.Fatalf("process: %v", err)
	}
	if err := n.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestPitchCorrectorInvalidParams(t *testing.T) {
	cases := []map[string]any{
		{"mode": "dorian"},
		{"amount": 1.5},
		{"amount": -0.1},
		{"speedMs": -5},
		{"confidence": 1.2},
		{"confidence": -0.1},
		{"blockSize": 32}, // below algo-dsp's minimum of 64
		{"mode": "fixed"}, // fixed mode requires targetHz
		{"mode": "fixed", "targetHz": 523.25, "amount": 2},
	}
	for _, p := range cases {
		if _, err := registry["pitchcorrector"](24000, p); err == nil {
			t.Errorf("params %v: want error", p)
		}
	}
}

// TestPitchCorrectorChromaticQuantization feeds a 441 Hz tone (about 4 cents
// sharp of A4=440) and expects the corrected steady-state fundamental to land
// on 440.
func TestPitchCorrectorChromaticQuantization(t *testing.T) {
	tone := sineTone(441, 36000) // 1.5 s: enough for the tracker to settle
	out := processTone(t, map[string]any{"mode": "chromatic", "amount": 1, "speedMs": 20}, tone)

	inF0 := estimateF0(tone[12000:])
	outF0 := estimateF0(toF64(out[pcNodeOffset+4096:]))

	if math.Abs(outF0-440) > 3 {
		t.Errorf("corrected f0 = %.2f Hz, want ~440 (A4)", outF0)
	}
	if math.Abs(outF0-inF0) < 0.5 {
		t.Errorf("output f0 %.2f did not move from input f0 %.2f", outF0, inF0)
	}
}

// TestPitchCorrectorFixedMode steers 441 Hz onto a fixed 523.25 Hz (C5) target.
func TestPitchCorrectorFixedMode(t *testing.T) {
	tone := sineTone(441, 36000)
	const target = 523.25
	out := processTone(t, map[string]any{"mode": "fixed", "targetHz": target, "amount": 1, "speedMs": 0}, tone)

	outF0 := estimateF0(toF64(out[pcNodeOffset+4096:]))
	if math.Abs(outF0-target) > 5 {
		t.Errorf("fixed-mode f0 = %.2f Hz, want ~%.2f", outF0, target)
	}
}

// TestPitchCorrectorAmountBypass verifies amount=0 passes the signal through
// bit-exactly (the shifter is bypassed entirely), while the default amount=1
// output differs from the input.
func TestPitchCorrectorAmountBypass(t *testing.T) {
	tone := sineTone(441, 30000)

	n := buildNode(t, "pitchcorrector", map[string]any{"amount": 0})
	var out []float32
	for _, sz := range mixedChunks(len(tone)) {
		buf := stereoFrames(sz)
		off := len(out)
		for i := range sz {
			buf[2*i] = float32(tone[off+i])
		}
		if err := n.ProcessInPlace(buf); err != nil {
			t.Fatalf("process chunk at %d: %v", off, err)
		}
		for i := range sz {
			out = append(out, buf[2*i])
		}
	}
	_ = n.Close()

	offset := passthroughDelay(t, out, tone, pcDefaultBlock)
	for j := offset; j < len(out); j++ {
		if out[j] != float32(tone[j-offset]) {
			t.Fatalf("amount=0: sample %d: got %v want %v (bypass must be bit-exact)",
				j, out[j], float32(tone[j-offset]))
		}
	}
}

// TestPitchCorrectorHardQuantize checks speedMs=0 (hard quantisation)
// constructs without error and still corrects, with the output differing from
// the input.
func TestPitchCorrectorHardQuantize(t *testing.T) {
	tone := sineTone(441, 36000)
	out := processTone(t, map[string]any{"mode": "chromatic", "amount": 1, "speedMs": 0}, tone)

	changed := 0
	for j := pcNodeOffset; j < len(out); j++ {
		if out[j] != float32(tone[j-pcNodeOffset]) {
			changed++
		}
	}
	if changed < 1000 {
		t.Errorf("speedMs=0 output changed only %d of %d samples, want a real correction", changed, len(out)-pcNodeOffset)
	}
	outF0 := estimateF0(toF64(out[pcNodeOffset+4096:]))
	if math.Abs(outF0-440) > 3 {
		t.Errorf("speedMs=0 corrected f0 = %.2f Hz, want ~440", outF0)
	}
}

// TestPitchCorrectorConfidenceGate feeds a tone whose detector confidence is
// ~0.9 (sinusoid plus Gaussian noise, deterministic seed): with the default
// confidence floor 0.5 the correction engages, while a floor of 0.99 makes
// every block unvoiced and the node behave as a bit-exact bypass.
func TestPitchCorrectorConfidenceGate(t *testing.T) {
	tone := noisyTone(441, 48000, 0.1)
	engaged := processTone(t, map[string]any{"mode": "chromatic", "amount": 1, "speedMs": 0, "confidence": 0.5}, tone)
	gated := processTone(t, map[string]any{"mode": "chromatic", "amount": 1, "speedMs": 0, "confidence": 0.99}, tone)

	offset := passthroughDelay(t, gated, tone, pcDefaultBlock)
	for j := offset; j < len(gated); j++ {
		if gated[j] != float32(tone[j-offset]) {
			t.Fatalf("gated (confidence=0.99): sample %d: got %v want %v (must bypass untouched)", j, gated[j], float32(tone[j-offset]))
		}
	}
	changed := 0
	for j := offset; j < len(engaged) && j-offset < len(tone); j++ {
		if engaged[j] != float32(tone[j-offset]) {
			changed++
		}
	}
	if changed < 1000 {
		t.Errorf("engaged (confidence=0.5): only %d of %d samples changed, want a real correction", changed, len(tone)-offset)
	}
}

// TestPitchCorrectorLRSeparation feeds tone only into L; R must stay exactly
// zero for the whole stream (including the primed warmup) while L carries
// corrected audio, proving the two channels never cross-contaminate.
func TestPitchCorrectorLRSeparation(t *testing.T) {
	tone := sineTone(441, 36000)
	n := buildNode(t, "pitchcorrector", map[string]any{"mode": "chromatic", "amount": 1, "speedMs": 20})
	var lOut, rOut []float32
	for _, sz := range mixedChunks(len(tone)) {
		buf := stereoFrames(sz)
		off := len(lOut)
		for i := range sz {
			buf[2*i] = float32(tone[off+i])
		}
		if err := n.ProcessInPlace(buf); err != nil {
			t.Fatalf("process chunk at %d: %v", off, err)
		}
		for i := range sz {
			lOut = append(lOut, buf[2*i])
			rOut = append(rOut, buf[2*i+1])
		}
	}
	_ = n.Close()

	for i, v := range rOut {
		if v != 0 {
			t.Fatalf("R sample %d: got %v, want 0 (L signal leaked into R)", i, v)
		}
	}
	signal := false
	for i := pcNodeOffset + 4096; i < len(lOut); i++ {
		if math.Abs(float64(lOut[i])) > 0.01 {
			signal = true
			break
		}
	}
	if !signal {
		t.Error("L channel is silent after the warmup; expected corrected audio")
	}
}

// TestPitchCorrectorBoundedBuffers feeds a long stream through mixed chunk
// sizes and checks the node's internal per-channel buffers never grow without
// a bound: pending stays strictly below blockSize and the output FIFO never
// exceeds blockSize. A runaway accumulation would trip these invariants (and
// eventually OOM the process); the corrector's own internal queues are
// bounded by design, so this covers the node's share of the memory contract
// without an assertZeroAllocs-style allocation test.
func TestPitchCorrectorBoundedBuffers(t *testing.T) {
	n := buildNode(t, "pitchcorrector", map[string]any{"blockSize": 2048}).(*pitchCorrectorNode)
	tone := sineTone(441, 4096*40) // ~6.8 s in mixed chunks
	for _, sz := range mixedChunks(len(tone)) {
		buf := stereoFrames(sz)
		for i := range sz {
			buf[2*i] = float32(tone[i%len(tone)])
		}
		if err := n.ProcessInPlace(buf); err != nil {
			t.Fatalf("process: %v", err)
		}
		for _, c := range []*pitchCorrectorChannel{n.left, n.right} {
			if len(c.pending) >= c.blockSize {
				t.Fatalf("pending len %d >= blockSize %d (unbounded accumulation)", len(c.pending), c.blockSize)
			}
			if len(c.out) > c.blockSize {
				t.Fatalf("out len %d > blockSize %d (unbounded output FIFO)", len(c.out), c.blockSize)
			}
		}
	}
}

// toF64 converts the float32 slice to float64.
func toF64(in []float32) []float64 {
	out := make([]float64, len(in))
	for i := range in {
		out[i] = float64(in[i])
	}
	return out
}

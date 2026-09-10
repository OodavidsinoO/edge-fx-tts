package effects

import (
	"fmt"
	"math"

	pitch "github.com/cwbudde/algo-dsp/dsp/effects/pitch"
)

func init() {
	Register("wsola", newWsola)
}

// wsolaNode approximates a duration-preserving WSOLA pitch shift with
// algo-dsp's SpectralPitchShifter (the same shifter PitchCorrector uses
// internally). algo-dsp ships no true WSOLA processor; the correct
// equivalent for the -1 st "slight downward detune" (report §6.2, WSOLA
// ratio 0.944) is a duration-preserving spectral pitch shift, which this
// shifter implements. It is mono and streamed across ProcessInPlace calls:
// each channel keeps an overlap carry of the previous call's tail so the
// shifter's OLA window coverage stays continuous across chunk boundaries
// (see processWsolaChannel). L and R each get their own shifter so their state
// stays independent.
//
// Allocation exception: SpectralPitchShifter.Process returns a freshly
// allocated slice by contract, so this node's hot path necessarily
// allocates on every call. The per-channel staging scratch is reused across
// calls, but do not add an assertZeroAllocs test to wsola_test.go; behavior
// tests only.
type wsolaNode struct {
	left  *wsolaChannel
	right *wsolaChannel
}

// wsolaChannel is one mono branch: the shifter, a scratch buffer reused
// across calls so staging the float32 input into float64 does not allocate
// per call, and the overlap carry holding the last (frameSize - hop) input
// samples of the previous call.
type wsolaChannel struct {
	shifter  *pitch.SpectralPitchShifter
	scratch  []float64
	carry    []float64 // overlap carry, len = frameSize - analysisHop
	carryLen int
}

func newWsola(sampleRate int, params map[string]any) (Node, error) {
	semitones := getFloat(params, "semitones", 0)
	frameSize := getInt(params, "frameSize", 2048)

	if math.IsNaN(semitones) {
		return nil, fmt.Errorf("effects: wsola: semitones must be finite: %g", semitones)
	}
	if frameSize < 64 || frameSize&(frameSize-1) != 0 {
		return nil, fmt.Errorf("effects: wsola: frameSize must be a power of two >= 64: %d", frameSize)
	}

	build := func() (*wsolaChannel, error) {
		s, err := pitch.NewSpectralPitchShifter(float64(sampleRate))
		if err != nil {
			return nil, err
		}
		if err := s.SetPitchSemitones(semitones); err != nil {
			return nil, fmt.Errorf("semitones: %w", err)
		}
		if err := s.SetFrameSize(frameSize); err != nil {
			return nil, fmt.Errorf("frameSize: %w", err)
		}
		// The carry must hold one full analysis hop less than a frame so the
		// kept region of the next call starts with full steady-state OLA
		// window coverage (frameSize - hop is a multiple of hop).
		overlap := frameSize - s.AnalysisHop()
		return &wsolaChannel{
			shifter: s,
			scratch: make([]float64, 0, frameSize),
			carry:   make([]float64, overlap),
		}, nil
	}
	left, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: wsola: %w", err)
	}
	right, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: wsola: %w", err)
	}
	return &wsolaNode{left: left, right: right}, nil
}

func (n *wsolaNode) ProcessInPlace(buf []float32) error {
	frames := len(buf) / 2
	if frames == 0 {
		return nil
	}
	processWsolaChannel(n.left, buf, 0, frames)
	processWsolaChannel(n.right, buf, 1, frames)
	return nil
}

func (n *wsolaNode) Close() error {
	n.left.shifter.Reset()
	n.right.shifter.Reset()
	return nil
}

// processWsolaChannel feeds one mono channel through the shifter with overlap
// carry. SpectralPitchShifter.Process is a one-shot STFT: its OLA
// normalization divides each output sample by the sum of squared window
// coefficients covering it, and at the head of a fresh call only the first
// frame covers the first samples, so the norm is ~w[0]^2 ~ 0 and the output
// blows up (measured ~1.2e4x transient spikes at every 4096-frame chunk
// boundary). Prepending the previous call's tail (frameSize - hop samples)
// makes the kept region start with full window coverage, and discarding the
// overlap region's output keeps the stream continuous across calls. The
// carry and scratch are preallocated; only SpectralPitchShifter.Process
// allocates (by contract).
func processWsolaChannel(c *wsolaChannel, buf []float32, ch, frames int) {
	callLen := c.carryLen + frames
	if len(c.scratch) < callLen {
		c.scratch = make([]float64, callLen)
	}
	for i := range c.carryLen {
		c.scratch[i] = c.carry[i]
	}
	for i := range frames {
		c.scratch[c.carryLen+i] = float64(buf[2*i+ch])
	}
	out := c.shifter.Process(c.scratch[:callLen])
	// Discard the overlap region's output; keep only the new chunk's.
	for i := range frames {
		buf[2*i+ch] = float32(out[c.carryLen+i])
	}
	// New carry: the last `overlap` input samples of this call.
	overlap := len(c.carry)
	newLen := callLen
	if newLen > overlap {
		newLen = overlap
	}
	copy(c.carry, c.scratch[callLen-newLen:callLen])
	c.carryLen = newLen
}

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
// shifter implements. It is mono and one-shot buffer oriented: L and R each
// get their own shifter so their state stays independent.
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

// wsolaChannel is one mono branch: the shifter plus a scratch buffer reused
// across calls so staging the float32 input into float64 does not allocate
// per call.
type wsolaChannel struct {
	shifter *pitch.SpectralPitchShifter
	scratch []float64
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
		return &wsolaChannel{shifter: s, scratch: make([]float64, 0, frameSize)}, nil
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
	if len(n.left.scratch) < frames {
		n.left.scratch = make([]float64, frames)
	}
	if len(n.right.scratch) < frames {
		n.right.scratch = make([]float64, frames)
	}
	for i := range frames {
		n.left.scratch[i] = float64(buf[2*i])
		n.right.scratch[i] = float64(buf[2*i+1])
	}
	outL := n.left.shifter.Process(n.left.scratch[:frames])
	outR := n.right.shifter.Process(n.right.scratch[:frames])
	for i := range frames {
		buf[2*i] = float32(outL[i])
		buf[2*i+1] = float32(outR[i])
	}
	return nil
}

func (n *wsolaNode) Close() error {
	n.left.shifter.Reset()
	n.right.shifter.Reset()
	return nil
}

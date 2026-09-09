package effects

import (
	"fmt"
	"math"

	pitch "github.com/cwbudde/algo-dsp/dsp/effects/pitch"
)

func init() {
	Register("pitchcorrector", newPitchCorrector)
}

// pitchCorrectorNode wraps one algo-dsp PitchCorrector per channel. It is a
// mono effect: L and R each get their own corrector instance so their state
// stays independent.
//
// Allocation exception: unlike the sample-by-sample effects in this package,
// the correction hot path allocates. algo-dsp's PitchProcessor.Process
// returns a freshly allocated slice by contract, so
// PitchCorrector.ProcessInPlace cannot be allocation free. The node reuses
// its accumulation buffers across calls and every allocation is bounded (see
// pitchCorrectorChannel), but do not add an assertZeroAllocs test to
// pitchcorrector_test.go; behavior tests only.
type pitchCorrectorNode struct {
	left, right *pitchCorrectorChannel
}

// pitchCorrectorChannel is one mono branch of a pitchCorrectorNode.
//
// The corrector is a block processor: it queues input internally and emits
// blockSize corrected samples per blockSize fed, with a fixed latency of one
// block plus the seam crossfade. Feeding it sample by sample would stall
// forever, so the channel accumulates mono samples in pending and hands the
// corrector complete blocks as they become available; out holds the emitted
// stream, primed with blockSize zeros so a pop before the first block is fed
// never underruns.
//
// Both buffers stay permanently bounded: after feeding j frames, pending
// holds j mod blockSize samples (< blockSize) and out holds
// blockSize-(j mod blockSize) samples (in (0, blockSize]), so repeated
// processing never grows them without limit.
type pitchCorrectorChannel struct {
	corr      *pitch.PitchCorrector
	blockSize int
	pending   []float64
	out       []float64
}

func newPitchCorrector(sampleRate int, params map[string]any) (Node, error) {
	mode := getString(params, "mode", "chromatic")
	if mode != "chromatic" && mode != "fixed" {
		return nil, fmt.Errorf("effects: pitchcorrector: mode must be \"chromatic\" or \"fixed\", got %q", mode)
	}

	amount := getFloat(params, "amount", 1)
	if amount < 0 || amount > 1 {
		return nil, fmt.Errorf("effects: pitchcorrector: amount must be in [0, 1], got %v", amount)
	}

	speedMs := getFloat(params, "speedMs", 20)
	if speedMs < 0 {
		return nil, fmt.Errorf("effects: pitchcorrector: speedMs must be >= 0, got %v", speedMs)
	}

	confidence := getFloat(params, "confidence", 0.5)
	if confidence < 0 || confidence > 1 {
		return nil, fmt.Errorf("effects: pitchcorrector: confidence must be in [0, 1], got %v", confidence)
	}

	blockSize := getInt(params, "blockSize", 2048)
	if blockSize < 64 {
		// algo-dsp requires at least 64 samples per correction block.
		return nil, fmt.Errorf("effects: pitchcorrector: blockSize must be >= 64, got %d", blockSize)
	}

	opts := []pitch.PitchCorrectorOption{
		pitch.WithCorrectionAmount(amount),
		pitch.WithCorrectionSpeedMs(speedMs),
		pitch.WithCorrectionConfidence(confidence),
		pitch.WithCorrectionBlockSize(blockSize),
	}
	if mode == "fixed" {
		targetHz := getFloat(params, "targetHz", math.NaN())
		if math.IsNaN(targetHz) {
			return nil, fmt.Errorf("effects: pitchcorrector: mode \"fixed\" requires targetHz")
		}
		opts = append(opts, pitch.WithCorrectionTargetHz(targetHz))
	} else {
		opts = append(opts, pitch.WithCorrectionScale(pitch.ScaleChromatic(pitch.PitchClassC)))
	}

	build := func() (*pitchCorrectorChannel, error) {
		c, err := pitch.NewPitchCorrector(float64(sampleRate), opts...)
		if err != nil {
			return nil, err
		}
		return &pitchCorrectorChannel{
			corr:      c,
			blockSize: blockSize,
			pending:   make([]float64, 0, blockSize),
			out:       make([]float64, blockSize), // prime: see channel doc
		}, nil
	}
	left, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: pitchcorrector: %w", err)
	}
	right, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: pitchcorrector: %w", err)
	}
	return &pitchCorrectorNode{left: left, right: right}, nil
}

// processBlocks feeds every full pending block to the corrector and appends
// the corrected output to out.
func (c *pitchCorrectorChannel) processBlocks() {
	for len(c.pending) >= c.blockSize {
		block := c.pending[:c.blockSize]
		c.corr.ProcessInPlace(block)
		c.out = append(c.out, block...)
		c.pending = c.pending[c.blockSize:]
	}
}

func (n *pitchCorrectorNode) ProcessInPlace(buf []float32) error {
	for i := 0; i+1 < len(buf); i += 2 {
		n.left.pending = append(n.left.pending, float64(buf[i]))
		n.right.pending = append(n.right.pending, float64(buf[i+1]))
	}
	n.left.processBlocks()
	n.right.processBlocks()
	for i := 0; i+1 < len(buf); i += 2 {
		buf[i] = float32(n.left.out[0])
		n.left.out = n.left.out[1:]
		buf[i+1] = float32(n.right.out[0])
		n.right.out = n.right.out[1:]
	}
	return nil
}

func (n *pitchCorrectorNode) Close() error {
	// Reset clears the tracker, shifter, sample queues and seam state,
	// returning both correctors to their freshly constructed condition.
	n.left.corr.Reset()
	n.right.corr.Reset()
	return nil
}

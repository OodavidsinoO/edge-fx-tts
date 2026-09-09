package effects

import (
	"fmt"

	"github.com/cwbudde/algo-dsp/dsp/effects/dynamics"
)

func init() {
	Register("compressor", newCompressor)
}

// compressorNode wraps an algo-dsp compressor. It is a mono effect: L and R
// each get their own compressor instance so their state stays independent.
//
// The Node seam has no sidechain input, so V1 implements plain compression
// (ProcessSample, input-driven detection). The sidechainLowCut/sidechainHighCut
// params still configure the detector pre-filter, which shapes what the
// detector responds to; true ducking (a separate sidechain signal) needs a
// dedicated ducking node and is out of scope for this ticket.
type compressorNode struct {
	left  *dynamics.Compressor
	right *dynamics.Compressor
}

func newCompressor(sampleRate int, params map[string]any) (Node, error) {
	build := func() (*dynamics.Compressor, error) {
		c, err := dynamics.NewCompressor(float64(sampleRate))
		if err != nil {
			return nil, err
		}
		if err := c.SetThreshold(getFloat(params, "threshold", -20)); err != nil {
			return nil, fmt.Errorf("threshold: %w", err)
		}
		if err := c.SetRatio(getFloat(params, "ratio", 2)); err != nil {
			return nil, fmt.Errorf("ratio: %w", err)
		}
		if err := c.SetKnee(getFloat(params, "knee", 6)); err != nil {
			return nil, fmt.Errorf("knee: %w", err)
		}
		if err := c.SetAttack(getFloat(params, "attack", 10)); err != nil {
			return nil, fmt.Errorf("attack: %w", err)
		}
		if err := c.SetRelease(getFloat(params, "release", 100)); err != nil {
			return nil, fmt.Errorf("release: %w", err)
		}
		if err := c.SetMakeupGain(getFloat(params, "makeupGain", 0)); err != nil {
			return nil, fmt.Errorf("makeupGain: %w", err)
		}
		if err := c.SetSidechainLowCut(getFloat(params, "sidechainLowCut", 0)); err != nil {
			return nil, fmt.Errorf("sidechainLowCut: %w", err)
		}
		if err := c.SetSidechainHighCut(getFloat(params, "sidechainHighCut", 0)); err != nil {
			return nil, fmt.Errorf("sidechainHighCut: %w", err)
		}
		return c, nil
	}
	left, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: compressor: %w", err)
	}
	right, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: compressor: %w", err)
	}
	return &compressorNode{left: left, right: right}, nil
}

func (n *compressorNode) ProcessInPlace(buf []float32) error {
	for i := 0; i+1 < len(buf); i += 2 {
		buf[i] = float32(n.left.ProcessSample(float64(buf[i])))
		buf[i+1] = float32(n.right.ProcessSample(float64(buf[i+1])))
	}
	return nil
}

func (n *compressorNode) Close() error { return nil }

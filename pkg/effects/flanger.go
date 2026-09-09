package effects

import (
	"fmt"
	"math"

	"github.com/cwbudde/algo-dsp/dsp/effects/modulation"
)

func init() {
	Register("flanger", newFlanger)
}

// flangerNode wraps an algo-dsp modulation.Flanger per channel. It is a mono
// effect: L and R each get their own flanger so their state stays
// independent.
type flangerNode struct {
	left  *modulation.Flanger
	right *modulation.Flanger
}

func newFlanger(sampleRate int, params map[string]any) (Node, error) {
	rateHz := getFloat(params, "rateHz", 0.4)
	depth := getFloat(params, "depth", 0.008)
	baseDelay := getFloat(params, "baseDelay", 0.002)
	feedback := getFloat(params, "feedback", 0)
	mix := getFloat(params, "mix", 0.25)

	// Node-level validation mirrors algo-dsp's ranges so bad values fail
	// with a stable "effects: flanger:" prefix; the setters re-check and
	// add their own detail (e.g. baseDelay+depth must stay <= 10 ms).
	if math.IsNaN(rateHz) || rateHz <= 0 {
		return nil, fmt.Errorf("effects: flanger: rateHz must be > 0: %g", rateHz)
	}
	if math.IsNaN(depth) || depth <= 0 {
		return nil, fmt.Errorf("effects: flanger: depth must be > 0: %g", depth)
	}
	if math.IsNaN(baseDelay) || baseDelay < 0 {
		return nil, fmt.Errorf("effects: flanger: baseDelay must be >= 0: %g", baseDelay)
	}
	if math.IsNaN(feedback) || feedback < -0.99 || feedback > 0.99 {
		return nil, fmt.Errorf("effects: flanger: feedback must be in [-0.99, 0.99]: %g", feedback)
	}
	if math.IsNaN(mix) || mix < 0 || mix > 1 {
		return nil, fmt.Errorf("effects: flanger: mix must be in [0, 1]: %g", mix)
	}

	build := func() (*modulation.Flanger, error) {
		f, err := modulation.NewFlanger(float64(sampleRate))
		if err != nil {
			return nil, err
		}
		if err := f.SetRateHz(rateHz); err != nil {
			return nil, fmt.Errorf("rateHz: %w", err)
		}
		if err := f.SetDepthSeconds(depth); err != nil {
			return nil, fmt.Errorf("depth: %w", err)
		}
		if err := f.SetBaseDelaySeconds(baseDelay); err != nil {
			return nil, fmt.Errorf("baseDelay: %w", err)
		}
		if err := f.SetFeedback(feedback); err != nil {
			return nil, fmt.Errorf("feedback: %w", err)
		}
		if err := f.SetMix(mix); err != nil {
			return nil, fmt.Errorf("mix: %w", err)
		}
		return f, nil
	}
	left, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: flanger: %w", err)
	}
	right, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: flanger: %w", err)
	}
	return &flangerNode{left: left, right: right}, nil
}

func (n *flangerNode) ProcessInPlace(buf []float32) error {
	for i := 0; i+1 < len(buf); i += 2 {
		buf[i] = float32(n.left.ProcessSample(float64(buf[i])))
		buf[i+1] = float32(n.right.ProcessSample(float64(buf[i+1])))
	}
	return nil
}

func (n *flangerNode) Close() error {
	n.left.Reset()
	n.right.Reset()
	return nil
}

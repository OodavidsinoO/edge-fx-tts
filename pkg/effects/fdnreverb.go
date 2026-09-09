package effects

import (
	"fmt"

	"github.com/cwbudde/algo-dsp/dsp/effects/reverb"
)

func init() {
	Register("fdnreverb", newFDNReverb)
}

// fdnReverbNode wraps an algo-dsp FDN reverb. It is a mono effect: L and R
// each get their own reverb instance so their state stays independent and
// time parameters are interpreted at the true sample rate.
type fdnReverbNode struct {
	left  *reverb.FDNReverb
	right *reverb.FDNReverb
}

func newFDNReverb(sampleRate int, params map[string]any) (Node, error) {
	build := func() (*reverb.FDNReverb, error) {
		r, err := reverb.NewFDNReverb(float64(sampleRate))
		if err != nil {
			return nil, err
		}
		if err := r.SetRT60(getFloat(params, "rt60", 1.8)); err != nil {
			return nil, fmt.Errorf("rt60: %w", err)
		}
		if err := r.SetDamp(getFloat(params, "damp", 0.3)); err != nil {
			return nil, fmt.Errorf("damp: %w", err)
		}
		if err := r.SetPreDelay(getFloat(params, "preDelay", 0.01)); err != nil {
			return nil, fmt.Errorf("preDelay: %w", err)
		}
		if err := r.SetWet(getFloat(params, "wet", 0.2)); err != nil {
			return nil, fmt.Errorf("wet: %w", err)
		}
		if err := r.SetDry(getFloat(params, "dry", 1.0)); err != nil {
			return nil, fmt.Errorf("dry: %w", err)
		}
		return r, nil
	}
	left, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: fdnreverb: %w", err)
	}
	right, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: fdnreverb: %w", err)
	}
	return &fdnReverbNode{left: left, right: right}, nil
}

func (n *fdnReverbNode) ProcessInPlace(buf []float32) error {
	for i := 0; i+1 < len(buf); i += 2 {
		buf[i] = float32(n.left.ProcessSample(float64(buf[i])))
		buf[i+1] = float32(n.right.ProcessSample(float64(buf[i+1])))
	}
	return nil
}

func (n *fdnReverbNode) Close() error { return nil }

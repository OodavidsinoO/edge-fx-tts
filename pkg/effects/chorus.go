package effects

import (
	"fmt"

	"github.com/cwbudde/algo-dsp/dsp/effects/modulation"
)

func init() {
	Register("chorus", newChorus)
}

// chorusNode wraps an algo-dsp multi-voice chorus. It is a mono effect: L
// and R each get their own chorus instance so their state stays independent.
type chorusNode struct {
	left  *modulation.Chorus
	right *modulation.Chorus
}

func newChorus(sampleRate int, params map[string]any) (Node, error) {
	build := func() (*modulation.Chorus, error) {
		c, err := modulation.NewChorus()
		if err != nil {
			return nil, err
		}
		if err := c.SetSampleRate(float64(sampleRate)); err != nil {
			return nil, err
		}
		if err := c.SetSpeedHz(getFloat(params, "speedHz", 0.35)); err != nil {
			return nil, fmt.Errorf("speedHz: %w", err)
		}
		if err := c.SetDepth(getFloat(params, "depth", 0.003)); err != nil {
			return nil, fmt.Errorf("depth: %w", err)
		}
		if err := c.SetBaseDelay(getFloat(params, "baseDelay", 0.018)); err != nil {
			return nil, fmt.Errorf("baseDelay: %w", err)
		}
		if err := c.SetStages(getInt(params, "stages", 3)); err != nil {
			return nil, fmt.Errorf("stages: %w", err)
		}
		if err := c.SetMix(getFloat(params, "mix", 0.18)); err != nil {
			return nil, fmt.Errorf("mix: %w", err)
		}
		return c, nil
	}
	left, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: chorus: %w", err)
	}
	right, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: chorus: %w", err)
	}
	return &chorusNode{left: left, right: right}, nil
}

func (n *chorusNode) ProcessInPlace(buf []float32) error {
	for i := 0; i+1 < len(buf); i += 2 {
		buf[i] = float32(n.left.ProcessSample(float64(buf[i])))
		buf[i+1] = float32(n.right.ProcessSample(float64(buf[i+1])))
	}
	return nil
}

func (n *chorusNode) Close() error { return nil }

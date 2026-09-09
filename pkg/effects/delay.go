package effects

import (
	"fmt"

	algodelay "github.com/cwbudde/algo-dsp/dsp/effects"
)

func init() {
	Register("delay", newDelay)
}

// delayNode wraps an algo-dsp feedback delay. It is a mono effect: L and R
// each get their own delay instance so their state stays independent.
type delayNode struct {
	left  *algodelay.Delay
	right *algodelay.Delay
}

func newDelay(sampleRate int, params map[string]any) (Node, error) {
	build := func() (*algodelay.Delay, error) {
		d, err := algodelay.NewDelay(float64(sampleRate))
		if err != nil {
			return nil, err
		}
		if err := d.SetTime(getFloat(params, "time", 0.1)); err != nil {
			return nil, fmt.Errorf("time: %w", err)
		}
		if err := d.SetFeedback(getFloat(params, "feedback", 0.35)); err != nil {
			return nil, fmt.Errorf("feedback: %w", err)
		}
		if err := d.SetMix(getFloat(params, "mix", 0.25)); err != nil {
			return nil, fmt.Errorf("mix: %w", err)
		}
		return d, nil
	}
	left, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: delay: %w", err)
	}
	right, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: delay: %w", err)
	}
	return &delayNode{left: left, right: right}, nil
}

func (n *delayNode) ProcessInPlace(buf []float32) error {
	for i := 0; i+1 < len(buf); i += 2 {
		buf[i] = float32(n.left.ProcessSample(float64(buf[i])))
		buf[i+1] = float32(n.right.ProcessSample(float64(buf[i+1])))
	}
	return nil
}

func (n *delayNode) Close() error { return nil }

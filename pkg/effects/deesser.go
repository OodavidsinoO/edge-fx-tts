package effects

import (
	"fmt"

	"github.com/cwbudde/algo-dsp/dsp/effects/dynamics"
)

func init() {
	Register("deesser", newDeEsser)
}

// deesserNode wraps an algo-dsp de-esser. It is a mono effect: L and R each
// get their own de-esser instance so their state stays independent.
type deesserNode struct {
	left  *dynamics.DeEsser
	right *dynamics.DeEsser
}

func newDeEsser(sampleRate int, params map[string]any) (Node, error) {
	build := func() (*dynamics.DeEsser, error) {
		d, err := dynamics.NewDeEsser(float64(sampleRate))
		if err != nil {
			return nil, err
		}
		if err := d.SetFrequency(getFloat(params, "frequency", 6000)); err != nil {
			return nil, fmt.Errorf("frequency: %w", err)
		}
		if err := d.SetQ(getFloat(params, "q", 1.5)); err != nil {
			return nil, fmt.Errorf("q: %w", err)
		}
		if err := d.SetThreshold(getFloat(params, "threshold", -20)); err != nil {
			return nil, fmt.Errorf("threshold: %w", err)
		}
		if err := d.SetRatio(getFloat(params, "ratio", 2)); err != nil {
			return nil, fmt.Errorf("ratio: %w", err)
		}
		if err := d.SetAttack(getFloat(params, "attack", 1)); err != nil {
			return nil, fmt.Errorf("attack: %w", err)
		}
		if err := d.SetRelease(getFloat(params, "release", 50)); err != nil {
			return nil, fmt.Errorf("release: %w", err)
		}
		return d, nil
	}
	left, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: deesser: %w", err)
	}
	right, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: deesser: %w", err)
	}
	return &deesserNode{left: left, right: right}, nil
}

func (n *deesserNode) ProcessInPlace(buf []float32) error {
	for i := 0; i+1 < len(buf); i += 2 {
		buf[i] = float32(n.left.ProcessSample(float64(buf[i])))
		buf[i+1] = float32(n.right.ProcessSample(float64(buf[i+1])))
	}
	return nil
}

func (n *deesserNode) Close() error { return nil }

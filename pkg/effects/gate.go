package effects

import (
	"fmt"
	"math"

	"github.com/cwbudde/algo-dsp/dsp/effects/dynamics"
)

func init() {
	Register("gate", newGate)
}

// gateNode wraps an algo-dsp dynamics.Gate per channel. It is a mono effect:
// L and R each get their own gate so their state stays independent. (The
// gate lives in the dsp/effects/dynamics subpackage, not dsp/effects.)
type gateNode struct {
	left  *dynamics.Gate
	right *dynamics.Gate
}

func newGate(sampleRate int, params map[string]any) (Node, error) {
	threshold := getFloat(params, "threshold", -40)
	ratio := getFloat(params, "ratio", 10)
	attack := getFloat(params, "attack", 5)
	release := getFloat(params, "release", 200)
	hold := getFloat(params, "hold", 0)

	// Node-level validation mirrors algo-dsp's ranges so bad values fail
	// with a stable "effects: gate:" prefix; the setters re-check and add
	// their own detail.
	if math.IsNaN(threshold) {
		return nil, fmt.Errorf("effects: gate: threshold must be finite: %g", threshold)
	}
	if math.IsNaN(ratio) || ratio < 1 || ratio > 100 {
		return nil, fmt.Errorf("effects: gate: ratio must be in [1, 100]: %g", ratio)
	}
	if math.IsNaN(attack) || attack < 0.1 || attack > 1000 {
		return nil, fmt.Errorf("effects: gate: attack must be in [0.1, 1000] ms: %g", attack)
	}
	if math.IsNaN(release) || release < 1 || release > 5000 {
		return nil, fmt.Errorf("effects: gate: release must be in [1, 5000] ms: %g", release)
	}
	if math.IsNaN(hold) || hold < 0 || hold > 5000 {
		return nil, fmt.Errorf("effects: gate: hold must be in [0, 5000] ms: %g", hold)
	}

	build := func() (*dynamics.Gate, error) {
		g, err := dynamics.NewGate(float64(sampleRate))
		if err != nil {
			return nil, err
		}
		if err := g.SetThreshold(threshold); err != nil {
			return nil, fmt.Errorf("threshold: %w", err)
		}
		if err := g.SetRatio(ratio); err != nil {
			return nil, fmt.Errorf("ratio: %w", err)
		}
		if err := g.SetAttack(attack); err != nil {
			return nil, fmt.Errorf("attack: %w", err)
		}
		if err := g.SetRelease(release); err != nil {
			return nil, fmt.Errorf("release: %w", err)
		}
		if err := g.SetHold(hold); err != nil {
			return nil, fmt.Errorf("hold: %w", err)
		}
		return g, nil
	}
	left, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: gate: %w", err)
	}
	right, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: gate: %w", err)
	}
	return &gateNode{left: left, right: right}, nil
}

func (n *gateNode) ProcessInPlace(buf []float32) error {
	for i := 0; i+1 < len(buf); i += 2 {
		buf[i] = float32(n.left.ProcessSample(float64(buf[i])))
		buf[i+1] = float32(n.right.ProcessSample(float64(buf[i+1])))
	}
	return nil
}

func (n *gateNode) Close() error {
	n.left.Reset()
	n.right.Reset()
	return nil
}

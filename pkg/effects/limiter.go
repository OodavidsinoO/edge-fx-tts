package effects

import (
	"fmt"

	"github.com/cwbudde/algo-dsp/dsp/effects/dynamics"
)

func init() {
	Register("limiter", newLimiter)
}

// limiterNode wraps an algo-dsp peak limiter. It is a mono effect: L and R
// each get their own limiter instance so their state stays independent. The
// config package guarantees the limiter is the terminal stage.
type limiterNode struct {
	left  *dynamics.Limiter
	right *dynamics.Limiter
}

func newLimiter(sampleRate int, params map[string]any) (Node, error) {
	build := func() (*dynamics.Limiter, error) {
		l, err := dynamics.NewLimiter(float64(sampleRate))
		if err != nil {
			return nil, err
		}
		if err := l.SetThreshold(getFloat(params, "threshold", -1)); err != nil {
			return nil, fmt.Errorf("threshold: %w", err)
		}
		if err := l.SetRelease(getFloat(params, "release", 100)); err != nil {
			return nil, fmt.Errorf("release: %w", err)
		}
		return l, nil
	}
	left, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: limiter: %w", err)
	}
	right, err := build()
	if err != nil {
		return nil, fmt.Errorf("effects: limiter: %w", err)
	}
	return &limiterNode{left: left, right: right}, nil
}

func (n *limiterNode) ProcessInPlace(buf []float32) error {
	for i := 0; i+1 < len(buf); i += 2 {
		buf[i] = float32(n.left.ProcessSample(float64(buf[i])))
		buf[i+1] = float32(n.right.ProcessSample(float64(buf[i+1])))
	}
	return nil
}

func (n *limiterNode) Close() error { return nil }

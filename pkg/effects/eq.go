package effects

import (
	"fmt"

	"github.com/cwbudde/algo-dsp/dsp/filter/biquad"
	"github.com/cwbudde/algo-dsp/dsp/filter/design"
)

func init() {
	Register("eq", newEQ)
}

// eqNode is a biquad EQ built from the algo-dsp RBJ filter designers. L and
// R each get their own Section so their filter state stays independent.
type eqNode struct {
	left  *biquad.Section
	right *biquad.Section
}

func newEQ(sampleRate int, params map[string]any) (Node, error) {
	typ := getString(params, "type", "peaking")
	freq := getFloat(params, "freq", 1000)
	q := getFloat(params, "q", 0.707)
	gain := getFloat(params, "gain", 0)

	if freq <= 0 || freq >= float64(sampleRate)/2 {
		return nil, fmt.Errorf("effects: eq: freq must be in (0, %d): %g", sampleRate/2, freq)
	}
	if q <= 0 {
		return nil, fmt.Errorf("effects: eq: q must be > 0: %g", q)
	}

	var coeffs biquad.Coefficients
	switch typ {
	case "highpass":
		coeffs = design.Highpass(freq, q, float64(sampleRate))
	case "lowpass":
		coeffs = design.Lowpass(freq, q, float64(sampleRate))
	case "peaking":
		coeffs = design.Peak(freq, gain, q, float64(sampleRate))
	default:
		return nil, fmt.Errorf("effects: eq: unknown type %q (supported: highpass, lowpass, peaking)", typ)
	}
	if coeffs.IsZero() {
		return nil, fmt.Errorf("effects: eq: undesignable filter (freq=%g q=%g gain=%g)", freq, q, gain)
	}
	return &eqNode{
		left:  biquad.NewSection(coeffs),
		right: biquad.NewSection(coeffs),
	}, nil
}

func (n *eqNode) ProcessInPlace(buf []float32) error {
	for i := 0; i+1 < len(buf); i += 2 {
		buf[i] = float32(n.left.ProcessSample(float64(buf[i])))
		buf[i+1] = float32(n.right.ProcessSample(float64(buf[i+1])))
	}
	return nil
}

func (n *eqNode) Close() error { return nil }

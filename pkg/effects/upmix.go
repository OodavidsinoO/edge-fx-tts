package effects

import "fmt"

func init() {
	Register("upmix", newUpmix)
}

// upmix is the mono→stereo chain-head conversion. The pipeline carries mono
// audio in the L channel of a stereo-interleaved buffer (R ignored); upmix
// fills R from L so downstream stereo effects see a real stereo signal.
type upmix struct {
	mode string
}

func newUpmix(_ int, params map[string]any) (Node, error) {
	mode := getString(params, "mode", "center")
	switch mode {
	case "center":
		// L == R: the mono source is centered.
	default:
		return nil, fmt.Errorf("effects: upmix: unknown mode %q (supported: center)", mode)
	}
	return &upmix{mode: mode}, nil
}

func (u *upmix) ProcessInPlace(buf []float32) error {
	for i := 0; i+1 < len(buf); i += 2 {
		buf[i+1] = buf[i]
	}
	return nil
}

func (u *upmix) Close() error { return nil }

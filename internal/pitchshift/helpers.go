package pitchshift

import "math"

const (
	// minPitchShifterRatio is the lowest allowed pitch ratio.
	minPitchShifterRatio = 0.25
	// maxPitchShifterRatio is the highest allowed pitch ratio.
	maxPitchShifterRatio = 4.0
	// pitchShifterIdentityEps is the ratio tolerance under which the shifter
	// treats its input as identity and copies it through unchanged.
	pitchShifterIdentityEps = 1e-9
)

func isFinitePositive(v float64) bool {
	return v > 0 && !math.IsInf(v, 0) && !math.IsNaN(v)
}

// SemitonesToRatio converts a semitone offset to a frequency ratio
// (12 semitones = one octave = ratio 2).
func SemitonesToRatio(semitones float64) float64 {
	return math.Exp2(semitones / 12.0)
}

// RatioToSemitones converts a frequency ratio back to semitones.
func RatioToSemitones(ratio float64) float64 {
	return 12.0 * math.Log2(ratio)
}

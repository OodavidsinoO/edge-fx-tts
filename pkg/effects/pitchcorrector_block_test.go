package effects

import (
	"math"
	"testing"

	pitch "github.com/cwbudde/algo-dsp/dsp/effects/pitch"
)

// TestPitchCorrectorBlockSizeClickRegression pins that the ai-modern preset's
// pitchcorrector blockSize (8192) produces strictly fewer block-boundary clicks
// than the 4096 value that was trialled in v0.5.5 and reverted. The stock
// algo-dsp shifter resets phase per Process call; PitchCorrector feeds it one
// Process per correction block, so a smaller block doubles the seam rate and
// raises clicks. Deterministic (pure tone, no network), stdlib only.
func TestPitchCorrectorBlockSizeClickRegression(t *testing.T) {
	sr := 24000
	total := sr * 2 // 2 s of continuous tone
	tone := make([]float64, total)
	for i := range tone {
		tone[i] = 0.5 * math.Sin(2*math.Pi*240*float64(i)/float64(sr))
	}

	count := func(blockSize int) int {
		opts := []pitch.PitchCorrectorOption{
			pitch.WithCorrectionAmount(0.3),
			pitch.WithCorrectionSpeedMs(200),
			pitch.WithCorrectionConfidence(0.5),
			pitch.WithCorrectionBlockSize(blockSize),
			pitch.WithCorrectionScale(pitch.ScaleChromatic(pitch.PitchClassC)),
		}
		c, err := pitch.NewPitchCorrector(float64(sr), opts...)
		if err != nil {
			t.Fatal(err)
		}
		var out []float64
		const chunk = 4096 // the pipeline's chunkSamples
		for s := 0; s < len(tone); s += chunk {
			end := s + chunk
			if end > len(tone) {
				end = len(tone)
			}
			out = append(out, c.Process(tone[s:end])...)
		}
		out = append(out, c.Process(make([]float64, chunk))...)
		clicks := 0
		for i := 1; i < len(out); i++ {
			if math.Abs(out[i]-out[i-1]) > 0.12 {
				clicks++
			}
		}
		return clicks
	}

	c4096 := count(4096)
	c8192 := count(8192)
	t.Logf("clicks: blockSize 4096 = %d, 8192 = %d", c4096, c8192)

	// The committed ai-modern preset uses 8192; it must not be noisier than
	// the 4096 trial would have been.
	if c8192 >= c4096 {
		t.Fatalf("blockSize 8192 produces more clicks (%d) than 4096 (%d); "+
			"revert to 4096 would re-introduce seam artifacts in ai-modern", c8192, c4096)
	}
}

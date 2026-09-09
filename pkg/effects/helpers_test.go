package effects

import (
	"testing"

	"github.com/OodavidsinoO/edge-fx-tts/pkg/config"
)

// stereoFrames returns a stereo-interleaved buffer of n frames (2n samples).
func stereoFrames(n int) []float32 {
	return make([]float32, 2*n)
}

// buildNode constructs a node through the registry factory at 24 kHz.
func buildNode(t *testing.T, name string, params map[string]any) Node {
	t.Helper()
	f, ok := registry[name]
	if !ok {
		t.Fatalf("effect %q not registered", name)
	}
	n, err := f(24000, params)
	if err != nil {
		t.Fatalf("build %s: %v", name, err)
	}
	return n
}

// assertZeroAllocs verifies the hot loop allocates nothing.
func assertZeroAllocs(t *testing.T, n Node, buf []float32) {
	t.Helper()
	allocs := testing.AllocsPerRun(100, func() {
		if err := n.ProcessInPlace(buf); err != nil {
			t.Fatalf("process: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("ProcessInPlace allocated %v bytes/op, want 0", allocs)
	}
}

// chainSpec builds a ChainSpec with the given stages at 24 kHz.
func chainSpec(stages ...config.StageSpec) *config.ChainSpec {
	return &config.ChainSpec{SampleRate: 24000, Stages: stages}
}

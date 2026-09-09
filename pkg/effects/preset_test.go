package effects_test

import (
	"testing"

	"github.com/OodavidsinoO/edge-fx-tts/pkg/config"
	"github.com/OodavidsinoO/edge-fx-tts/pkg/effects"
)

// TestPresetProfilesBuild verifies the A/B/C preset manifests validate through
// the chain build and assemble nodes via the effects registry (init()).
func TestPresetProfilesBuild(t *testing.T) {
	presets := []string{
		"aifake", "doubledelay", "scifiatmo",
		"filmai", "filmai-d2", "filmai-d3", "filmai-d4",
		"broadcast", "broadcast-e1", "broadcast-e2", "broadcast-e3",
	}
	for _, name := range presets {
		spec, err := config.LoadProfile(name, "")
		if err != nil {
			t.Fatalf("LoadProfile(%s): %v", name, err)
		}
		if len(spec.Stages) == 0 {
			t.Fatalf("profile %s has no stages", name)
		}
		if _, err := effects.BuildChain(spec); err != nil {
			t.Fatalf("profile %s BuildChain: %v", name, err)
		}
	}
}

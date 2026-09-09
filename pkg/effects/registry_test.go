package effects

import (
	"testing"

	"github.com/OodavidsinoO/edge-fx-tts/pkg/config"
)

func TestAllCoreEffectsRegistered(t *testing.T) {
	for _, name := range []string{"upmix", "fdnreverb", "chorus", "delay", "compressor", "deesser", "eq", "limiter", "flanger", "gate", "wsola"} {
		if _, ok := registry[name]; !ok {
			t.Errorf("effect %q not registered", name)
		}
	}
}

func TestKnownNamesSorted(t *testing.T) {
	names := KnownNames()
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatalf("KnownNames not sorted: %v", names)
		}
	}
}

func TestBuildChainThroughConfig(t *testing.T) {
	spec := chainSpec(
		config.StageSpec{Name: "upmix", Params: map[string]any{"mode": "center"}},
		config.StageSpec{Name: "delay", Params: map[string]any{"time": 0.1, "mix": 0.25}},
		config.StageSpec{Name: "limiter", Params: map[string]any{"threshold": -1}},
	)
	nodes, err := BuildChain(spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("got %d nodes, want 3", len(nodes))
	}
	buf := stereoFrames(64)
	for i := range buf {
		buf[i] = 0.5
	}
	for _, n := range nodes {
		if err := n.ProcessInPlace(buf); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range nodes {
		if err := n.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBuildChainUnknownEffect(t *testing.T) {
	spec := chainSpec(config.StageSpec{Name: "nope"})
	if _, err := BuildChain(spec); err == nil {
		t.Fatal("want error for unknown effect")
	}
}

func TestBuildChainBadParams(t *testing.T) {
	spec := chainSpec(config.StageSpec{Name: "delay", Params: map[string]any{"time": -1}})
	if _, err := BuildChain(spec); err == nil {
		t.Fatal("want error for invalid params")
	}
}

func TestParamsHelpers(t *testing.T) {
	params := map[string]any{
		"f": 1.5,
		"i": 3,
		"b": true,
		"s": "x",
	}
	if got := getFloat(params, "f", 0); got != 1.5 {
		t.Errorf("getFloat = %v", got)
	}
	if got := getFloat(params, "i", 0); got != 3 {
		t.Errorf("getFloat(int) = %v", got)
	}
	if got := getFloat(params, "missing", 7); got != 7 {
		t.Errorf("getFloat default = %v", got)
	}
	if got := getFloat(params, "s", 7); got != 7 {
		t.Errorf("getFloat non-numeric = %v", got)
	}
	if got := getInt(params, "i", 0); got != 3 {
		t.Errorf("getInt = %v", got)
	}
	if got := getInt(params, "f", 0); got != 0 {
		t.Errorf("getInt non-integral = %v", got)
	}
	if got := getBool(params, "b", false); !got {
		t.Errorf("getBool = %v", got)
	}
	if got := getString(params, "s", ""); got != "x" {
		t.Errorf("getString = %v", got)
	}
}

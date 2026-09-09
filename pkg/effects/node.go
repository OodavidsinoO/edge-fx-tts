// Package effects defines the effect-node seam and chain assembly. The
// pipeline processes stereo-interleaved float32 buffers; each Node mutates
// its input buffer in place. Mono effects (FDN reverb, chorus, delay,
// compressor, de-esser, limiter) process the L and R channels independently
// via the underlying algo-dsp ProcessSample, so the hot loop allocates
// nothing.
package effects

import (
	"fmt"
	"sort"
	"strings"

	"github.com/OodavidsinoO/edge-fx-tts/pkg/config"
)

// Node is one effect in the chain. It processes a stereo-interleaved float32
// buffer (L,R,L,R,...) in place. Implementations hold per-stream mutable
// state and must never be shared across pipeline instances.
type Node interface {
	// ProcessInPlace mutates buf (stereo interleaved) in place.
	ProcessInPlace(buf []float32) error
	// Close releases any resources held by the node.
	Close() error
}

// Factory builds a Node from a resolved stage. sampleRate is the processing
// sample rate (24000 or 48000); params are the stage's validated params.
type Factory func(sampleRate int, params map[string]any) (Node, error)

// registry maps effect names to their factories. It is the single source of
// truth for what the config package's validation accepts.
var registry = map[string]Factory{}

// Register adds an effect factory. It panics on duplicate names so wiring
// mistakes surface at init.
func Register(name string, f Factory) {
	if _, dup := registry[name]; dup {
		panic("effects: duplicate registration for " + name)
	}
	registry[name] = f
}

// KnownNames returns the sorted registered effect names.
func KnownNames() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// BuildChain assembles a []Node from a ChainSpec, in stage order. It returns
// an error if any stage is not registered (the config package should have
// caught this earlier, but the check is defensive).
func BuildChain(spec *config.ChainSpec) ([]Node, error) {
	nodes := make([]Node, 0, len(spec.Stages))
	for i, st := range spec.Stages {
		f, ok := registry[st.Name]
		if !ok {
			return nil, fmt.Errorf("effects: stage %d: unregistered effect %q (known: %s)", i, st.Name, strings.Join(KnownNames(), ", "))
		}
		n, err := f(spec.SampleRate, st.Params)
		if err != nil {
			return nil, fmt.Errorf("effects: stage %d (%s): %w", i, st.Name, err)
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

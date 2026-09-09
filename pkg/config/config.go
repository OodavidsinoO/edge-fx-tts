// Package config loads, validates and builds effect-chain configurations.
// A Config is the user-facing YAML/JSON shape; a ChainSpec is the frozen,
// immutable result the pipeline consumes.
package config

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Defaults applied when fields are omitted.
const (
	DefaultSampleRate   = 24000
	DefaultChannels     = 2
	DefaultChunkSamples = 4096
	DefaultOutputFormat = "wav"
	DefaultBitDepth     = 16
)

// Supported sample rates. The decoded Edge TTS stream is 24 kHz; 48 kHz is
// the reserved upsampled processing rate (resample inserted at the chain
// head).
var supportedSampleRates = map[int]bool{24000: true, 48000: true}

// Config is the user-facing effect-chain configuration.
type Config struct {
	Version      int     `yaml:"version"`
	SampleRate   int     `yaml:"sampleRate"`
	Channels     int     `yaml:"channels"`
	ChunkSamples int     `yaml:"chunkSamples"`
	Stages       []Stage `yaml:"stages"`
	Output       Output  `yaml:"output"`
}

// Stage is one effect in the chain.
type Stage struct {
	Name    string         `yaml:"name"`
	Params  map[string]any `yaml:"params"`
	Enabled *bool          `yaml:"enabled"` // nil means enabled
}

// Output describes the encoded output.
type Output struct {
	Format   string    `yaml:"format"`
	BitDepth int       `yaml:"bitDepth"`
	MP3      MP3Config `yaml:"mp3"`
}

// MP3Config holds MP3-specific output options.
type MP3Config struct {
	Bitrate int `yaml:"bitrate"`
}

// EffectInfo describes a known effect for validation and ordering.
type EffectInfo struct {
	// TimeBased marks effects with cross-sample state (delay/reverb/chorus)
	// that must run after the mono→stereo upmix.
	TimeBased bool
	// Terminal marks the mandatory final stage (limiter/master).
	Terminal bool
}

// registry is the set of known effect names. Ticket 4 wires these to algo-dsp
// implementations; here they drive fail-fast validation and ordering.
var registry = map[string]EffectInfo{
	"decode":         {},
	"upmix":          {},
	"aifake":         {TimeBased: true},
	"doubledelay":    {TimeBased: true},
	"scifiatmo":      {TimeBased: true},
	"filmai":         {TimeBased: true},
	"broadcast":      {TimeBased: true},
	"fdnreverb":      {TimeBased: true},
	"chorus":         {TimeBased: true},
	"delay":          {TimeBased: true},
	"flanger":        {TimeBased: true},
	"compressor":     {},
	"deesser":        {},
	"eq":             {},
	"wsola":          {},
	"pitchcorrector": {},
	"formant":        {},
	"bitcrush":       {},
	"gate":           {},
	"haas":           {TimeBased: true},
	"limiter":        {Terminal: true},
}

// KnownEffects returns the sorted list of registered effect names.
func KnownEffects() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ChainSpec is the frozen, immutable result of building a Config. It holds
// only enabled stages in chain order, with the implicit decode head and
// resample insertion resolved.
type ChainSpec struct {
	SampleRate   int
	Channels     int
	ChunkSamples int
	// Stages are the enabled stages in execution order, excluding the
	// implicit decode head and any inserted resample.
	Stages []StageSpec
	Output Output
	// Resample is true when the processing sample rate differs from the
	// decoded 24 kHz, meaning a resample stage is inserted at the chain head.
	Resample bool
}

// StageSpec is a resolved, enabled stage.
type StageSpec struct {
	Name   string
	Params map[string]any
}

// Load reads a YAML/JSON config from a file and builds a ChainSpec.
func Load(path string) (*ChainSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	return LoadBytes(data)
}

// LoadBytes parses and builds a ChainSpec from YAML/JSON bytes.
func LoadBytes(data []byte) (*ChainSpec, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse: %w", err)
	}
	return Build(&cfg)
}

// Build validates a Config and produces an immutable ChainSpec.
func Build(cfg *Config) (*ChainSpec, error) {
	if cfg.Version != 1 {
		return nil, fmt.Errorf("config: unsupported version %d (want 1)", cfg.Version)
	}
	if cfg.SampleRate == 0 {
		cfg.SampleRate = DefaultSampleRate
	}
	if !supportedSampleRates[cfg.SampleRate] {
		return nil, fmt.Errorf("config: unsupported sampleRate %d (want 24000 or 48000)", cfg.SampleRate)
	}
	if cfg.Channels == 0 {
		cfg.Channels = DefaultChannels
	}
	if cfg.Channels != 1 && cfg.Channels != 2 {
		return nil, fmt.Errorf("config: unsupported channels %d (want 1 or 2)", cfg.Channels)
	}
	if cfg.ChunkSamples == 0 {
		cfg.ChunkSamples = DefaultChunkSamples
	}
	if cfg.ChunkSamples <= 0 {
		return nil, fmt.Errorf("config: chunkSamples must be > 0, got %d", cfg.ChunkSamples)
	}
	if cfg.Output.Format == "" {
		cfg.Output.Format = DefaultOutputFormat
	}
	if cfg.Output.BitDepth == 0 {
		cfg.Output.BitDepth = DefaultBitDepth
	}

	spec := &ChainSpec{
		SampleRate:   cfg.SampleRate,
		Channels:     cfg.Channels,
		ChunkSamples: cfg.ChunkSamples,
		Output:       cfg.Output,
		Resample:     cfg.SampleRate != 24000,
	}

	seenUpmix := false
	seenTerminal := false
	for i, st := range cfg.Stages {
		info, ok := registry[st.Name]
		if !ok {
			return nil, fmt.Errorf("config: stage %d: unknown effect %q (known: %s)", i, st.Name, strings.Join(KnownEffects(), ", "))
		}
		if st.Enabled != nil && !*st.Enabled {
			continue // silently skipped
		}
		if info.Terminal {
			if seenTerminal {
				return nil, fmt.Errorf("config: stage %d: terminal effect %q must be the last stage", i, st.Name)
			}
			if i != len(cfg.Stages)-1 {
				return nil, fmt.Errorf("config: stage %d: terminal effect %q must be the last stage", i, st.Name)
			}
			seenTerminal = true
		}
		if st.Name == "upmix" {
			seenUpmix = true
		}
		if info.TimeBased && !seenUpmix {
			return nil, fmt.Errorf("config: stage %d: time-based effect %q must appear after upmix", i, st.Name)
		}
		spec.Stages = append(spec.Stages, StageSpec{Name: st.Name, Params: st.Params})
	}

	if len(spec.Stages) == 0 {
		return nil, errors.New("config: chain has no enabled stages")
	}
	return spec, nil
}

// ---------------------------------------------------------------------------
// Preset profiles: embedded at compile time, overridable by an external file
// of the same name.
// ---------------------------------------------------------------------------

//go:embed profiles/*.yaml
var profilesFS embed.FS

// LoadProfile loads a named preset profile. It first looks for an external
// override file (configs/profiles/<name>.yaml in the working directory, or
// the path given by overrideDir), then falls back to the embedded copy.
func LoadProfile(name string, overrideDir string) (*ChainSpec, error) {
	if name == "" || name == "none" {
		return nil, errors.New("config: profile 'none' is passthrough, not a chain profile")
	}
	// External override wins.
	if overrideDir != "" {
		path := filepath.Join(overrideDir, name+".yaml")
		if _, err := os.Stat(path); err == nil {
			return Load(path)
		}
	}
	data, err := profilesFS.ReadFile("profiles/" + name + ".yaml")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("config: unknown profile %q (known: %s)", name, strings.Join(ProfileNames(), ", "))
		}
		return nil, fmt.Errorf("config: read embedded profile %q: %w", name, err)
	}
	return LoadBytes(data)
}

// ProfileNames returns the embedded profile names (excluding 'none').
func ProfileNames() []string {
	entries, err := fs.ReadDir(profilesFS, "profiles")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".yaml"))
	}
	sort.Strings(names)
	return names
}

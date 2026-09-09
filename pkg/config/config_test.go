package config

import (
	"strings"
	"testing"
)

func TestBuildDefaults(t *testing.T) {
	cfg := &Config{Version: 1, Stages: []Stage{{Name: "upmix"}, {Name: "limiter"}}}
	spec, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if spec.SampleRate != 24000 {
		t.Fatalf("SampleRate = %d, want 24000 default", spec.SampleRate)
	}
	if spec.Channels != 2 {
		t.Fatalf("Channels = %d, want 2 default", spec.Channels)
	}
	if spec.ChunkSamples != 4096 {
		t.Fatalf("ChunkSamples = %d, want 4096 default", spec.ChunkSamples)
	}
	if spec.Resample {
		t.Fatal("Resample = true, want false at 24 kHz")
	}
	if len(spec.Stages) != 2 {
		t.Fatalf("Stages = %d, want 2", len(spec.Stages))
	}
}

func TestBuildUnknownEffectFailsFast(t *testing.T) {
	cfg := &Config{Version: 1, Stages: []Stage{{Name: "nope"}}}
	_, err := Build(cfg)
	if err == nil {
		t.Fatal("expected error for unknown effect")
	}
	if !strings.Contains(err.Error(), "unknown effect") || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error should name the unknown effect: %v", err)
	}
	if !strings.Contains(err.Error(), "known:") {
		t.Fatalf("error should list known effects: %v", err)
	}
}

func TestBuildDisabledStageSkipped(t *testing.T) {
	disabled := false
	cfg := &Config{Version: 1, Stages: []Stage{
		{Name: "upmix"},
		{Name: "chorus", Enabled: &disabled},
		{Name: "limiter"},
	}}
	spec, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(spec.Stages) != 2 {
		t.Fatalf("Stages = %d, want 2 (disabled skipped)", len(spec.Stages))
	}
	for _, s := range spec.Stages {
		if s.Name == "chorus" {
			t.Fatal("disabled chorus should be skipped")
		}
	}
}

func TestBuildTimeBasedBeforeUpmixRejected(t *testing.T) {
	cfg := &Config{Version: 1, Stages: []Stage{{Name: "chorus"}, {Name: "upmix"}, {Name: "limiter"}}}
	_, err := Build(cfg)
	if err == nil {
		t.Fatal("expected error: time-based effect before upmix")
	}
	if !strings.Contains(err.Error(), "after upmix") {
		t.Fatalf("error should mention upmix ordering: %v", err)
	}
}

func TestBuildLimiterNotLastRejected(t *testing.T) {
	cfg := &Config{Version: 1, Stages: []Stage{{Name: "upmix"}, {Name: "limiter"}, {Name: "eq"}}}
	_, err := Build(cfg)
	if err == nil {
		t.Fatal("expected error: limiter not last")
	}
	if !strings.Contains(err.Error(), "last stage") {
		t.Fatalf("error should mention last stage: %v", err)
	}
}

func TestBuildBadVersionRejected(t *testing.T) {
	cfg := &Config{Version: 2, Stages: []Stage{{Name: "upmix"}}}
	_, err := Build(cfg)
	if err == nil {
		t.Fatal("expected error for bad version")
	}
	if !strings.Contains(err.Error(), "unsupported version") {
		t.Fatalf("error should mention version: %v", err)
	}
}

func TestBuildBadSampleRateRejected(t *testing.T) {
	cfg := &Config{Version: 1, SampleRate: 44100, Stages: []Stage{{Name: "upmix"}}}
	_, err := Build(cfg)
	if err == nil {
		t.Fatal("expected error for unsupported sample rate")
	}
	if !strings.Contains(err.Error(), "sampleRate") {
		t.Fatalf("error should mention sampleRate: %v", err)
	}
}

func TestBuildResampleAt48k(t *testing.T) {
	cfg := &Config{Version: 1, SampleRate: 48000, Stages: []Stage{{Name: "upmix"}, {Name: "limiter"}}}
	spec, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !spec.Resample {
		t.Fatal("Resample = false, want true at 48 kHz")
	}
}

func TestLoadBytesYAML(t *testing.T) {
	data := []byte(`
version: 1
sampleRate: 24000
stages:
  - name: upmix
  - name: limiter
`)
	spec, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	if len(spec.Stages) != 2 {
		t.Fatalf("Stages = %d, want 2", len(spec.Stages))
	}
}

func TestLoadProfileEmbedded(t *testing.T) {
	spec, err := LoadProfile("placeholder", "")
	if err != nil {
		t.Fatalf("LoadProfile(placeholder): %v", err)
	}
	if spec.SampleRate != 24000 {
		t.Fatalf("SampleRate = %d, want 24000", spec.SampleRate)
	}
}

func TestLoadProfileUnknown(t *testing.T) {
	_, err := LoadProfile("does-not-exist", "")
	if err == nil {
		t.Fatal("expected error for unknown profile")
	}
	if !strings.Contains(err.Error(), "unknown profile") {
		t.Fatalf("error should mention unknown profile: %v", err)
	}
}

func TestProfileNames(t *testing.T) {
	names := ProfileNames()
	if len(names) == 0 {
		t.Fatal("ProfileNames returned empty")
	}
	found := false
	for _, n := range names {
		if n == "placeholder" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ProfileNames missing placeholder: %v", names)
	}
}

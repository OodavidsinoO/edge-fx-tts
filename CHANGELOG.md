# Changelog

All notable changes to this project will be documented in this file.

## v0.5.0 - 2026-09-10

This release is a fork of `lib-x/edgetts` (`v0.4.0`) turned into a
post-processing audio-effects engine over Edge TTS output.

### Added
- Effect chain engine: `pkg/effects` Node seam + registry with 13 effects
  (`upmix`, `eq`, `compressor`, `deesser`, `chorus`, `delay`, `fdnreverb`,
  `limiter`, `formant`, `pitchcorrector`, `flanger`, `gate`, `wsola`).
  Formant shifter is self-built cepstral source-filter; `pitchcorrector`
  wraps algo-dsp; `wsola` is a duration-preserving spectral pitch shift.
- Streaming pipeline: `pkg/pipeline` three-goroutine decode -> chain -> WAV
  sink with 16-bit PCM output and RIFF header (placeholder patched on close).
- `internal/decode`: cgo-minimp3 decoder (race-free) for the 24 kHz MPEG-2
  Layer 3 stream Edge TTS emits.
- `pkg/config`: profile system with embedded YAML presets and schema
  validation. Built-in profiles:
  - `aifake`, `doubledelay`, `scifiatmo` (A/B/C demo presets)
  - `filmai`, `filmai-d2` (GLaDOS quantized), `filmai-d3` (HAL), `filmai-d4`
  - `broadcast`, `broadcast-e1`, `broadcast-e2`, `broadcast-e3`
- CLI (`cmd/edgefx`): `--profile`, `--text`, `--output`, `--voice`,
  `--rate`, `--pitch`, `--volume`; SSML via positional `ssml`. `--profile
  none` passes the MP3 stream through verbatim.
- Tests: race-clean decode and pipeline; per-effect behavior tests
  (identity/bypass, L/R isolation, quantization, gating, zero allocations).

### Notes
- E (broadcast) profiles follow report §6.3; the EBU R128 -23 LUFS delivery
  calibration is not implemented in-profile and needs an offline
  measurement/gain stage (see feasibility report §6.4).

## v0.4.0 - 2026-04-22

### Added
- Added a new `Client` API for reusable synthesis configuration.
- Added package-level helper functions for one-off use cases:
  - `Bytes`
  - `BytesSSML`
  - `Save`
  - `SaveSSML`
  - `WriteTo`
  - `WriteSSMLTo`
  - `Stream`
  - `StreamSSML`
- Added first-class `Request` support with symmetric `Text(...)` and `SSML(...)` builders.
- Added structured batch APIs:
  - `Batch`
  - `SaveBatch`
  - `WriteZIP`
- Added voice filtering helpers:
  - `FilterVoices`
  - `FindVoice`
- Added runnable demo under `cmd/demo`.
- Added example coverage for streaming, SSML, request-based usage, batch output, ZIP output, and HTTP streaming.

### Changed
- Reworked the public API around direct synthesis workflows instead of the old task-queue-first model.
- Improved error propagation in the internal streaming path so websocket and writer failures are surfaced to callers.
- Added context-aware streaming support in the internal communication layer.
- Updated README with new API usage, streaming examples, runnable demo commands, and migration guidance.
- Marked the legacy `Speech` API as deprecated while keeping it as a compatibility wrapper.

### Fixed
- Fixed missing `go.sum` state so the module builds and tests cleanly.
- Fixed potential voice parsing panics when deriving `VoiceLangRegion`.
- Fixed file-save behavior to avoid leaving broken output files behind on synthesis failure.

### Verification
- `go test ./...`
- `go vet ./...`

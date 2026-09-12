# Changelog

All notable changes to this project will be documented in this file.

## v0.5.5 - 2026-09-12

### Changed
- Profile-tier deduplication: the `filmai` / `filmai-d2` / `broadcast-e1`
  profiles were duplicate aliases with chains byte-identical to `jarvis` /
  `ai-modern` / `broadcast` (`broadcast-e1` differed only by a comment
  header) and are removed — 14 built-in profiles → 11 (plus the
  `placeholder` stub). Use the canonical names.
- Noise governance across all tuned profiles: a second cascaded 7 kHz
  lowpass (q 0.707) after the de-esser, together with a widened de-esser
  (center 7 kHz, Q 1.5, threshold −28 dB, ratio 2.5), attacks the source's
  3 kHz+ encoding hiss the single 2nd-order LP left (-6 dB @9 kHz) and the
  default 6 kHz de-esser ignored.
  - `jarvis`: +cascaded LP 7 kHz after de-esser; de-esser 7 kHz / −28 dB.
  - `edith`: same as jarvis; presence 3 kHz gain 2.5 → 2.0.
  - `ai-modern`: same as jarvis; pitchcorrector blockSize 8192 → 4096
    (shorter 341 ms block latency, no 2-chunk seam).
  - `filmai-d3`: same de-esser / cascaded LP treatment.
  - `filmai-d4`: LP 7.5 kHz (softer than the family 7 kHz) + de-esser
    6.5 kHz / −28 dB before the FDN — keeps the "barely processed" role.
  - `doubledelay` / `scifiatmo`: LP 7 kHz + wideband de-esser before the
    reverb so the long tails (1.6 s / 3.0 s) do not drag out the hiss;
    `scifiatmo` FDN damp 0.22 → 0.38 (darker tail, −4~−6 dB hiss).
  - `broadcast` / `broadcast-e2`: de-esser retuned to 6.2 kHz / −22 dB
    (subtle, −1~−2 dB) for the E curve; `broadcast-e3` / `aifake` /
    `placeholder` unchanged (already narrowband/optimal / stub).

## v0.5.4 - 2026-09-12

### Fixed
- Residual high-frequency sibilance/"crackle" in the modern film-AI profiles
  after the wsola phase fix. The 8.5 kHz lowpass had largely masked the
  source's high-frequency transients, but user still heard "a little" pop.
  Lowering the lowpass from 8.5 kHz to 7 kHz removes the 7–8.5 kHz source
  transients that the full-band chain exposed: jarvis >4k-LSB jumps 110 → 5
  (>8k → 0), edith 80 → 4, while the 3 kHz presence and ≤6 kHz speech band are
  unchanged (band 3400–6000 stayed ~79 dB). Applied to jarvis/edith/
  ai-modern/filmai/filmai-d2/filmai-d3.

## v0.5.3 - 2026-09-12

### Fixed
- Audible "pop/crackle" in the wsola node on real speech. The stock
  algo-dsp `SpectralPitchShifter` calls `Reset()` (zeroing phase-vocoder
  state) at the top of every `Process` call, so streaming audio through the
  wsola node in 4096-sample chunks restarted phase tracking at every seam
  and emitted a transient 0.34–0.62 full-scale click (~265 in a 10 s clip;
  user heard a repeating "pop" in jarvis/edith). We forked the shifter to
  `internal/pitchshift`, removing the implicit reset so phase state survives
  across chunks; the public `Reset()` is retained. Measured: jarvis clicks
  265 → 0, edith 529 → 0 (the residual few in edith trace to source transients
  amplified by formant, not wsola). Added `TestWsolaStreamClickFree` which
  fails on the stock shifter (9 clicks) and passes on the fork (0).
- `go.mod`/`go.sum`: none changed — the fork imports the same algo-dsp /
  algo-fft deps already used by the repository.

## v0.5.2 - 2026-09-12

### Fixed
- High-frequency noise in the modern film-AI profiles (jarvis/edith,
  ai-modern) and the modernized filmai family. User reported audible hiss.
  Diagnosis: Edge TTS source carries high-frequency artifacts (8–12 kHz)
  that the full-band chain (HPF 100 Hz, no LPF) let through — aifake masked
  them with its 3.4 kHz bandpass. Fixed by adding a gentle 8.5 kHz lowpass
  (q 0.707) after the 3 kHz presence stage in jarvis/edith/ai-modern,
  filmai/filmai-d2/filmai-d3. Spectral measurement: jarvis 9–12 kHz band
  79.0→71.5 dB, edith 83.7→73.7 dB, hiss floor down ~10 dB, speech ≤6 kHz
  unchanged.

## v0.5.1 - 2026-09-10

### Changed
- Modernized the film-AI preset family. The previous chains (narrowband
  300–3400 Hz, flanger, long reverb, hard pitch quantization) read as a
  80s/90s telephone AI; the new chains follow the modern film-AI recipe
  (full-band ~100 Hz–8 kHz, near-field dry reverb, light modulation, subtle
  pitch/formant) per the feasibility report and sound-design research.
  - `filmai` (D1) → JARVIS-style natural near-field chain.
  - `filmai-d2` (D2) → light mechanical edge (formant 1.2 + light pitch
    corrector amount 0.3 / 200 ms / block 8192).
  - `filmai-d3` (D3) → modern calm (dropped flanger/narrowband, kept gate +
    strong compression).
  - `filmai-d4` (D4) unchanged (already near-field/wide-band).

### Added
- `jarvis` / `edith` / `ai-modern` preset profiles (modern film-AI voices).
- `demo/generate.sh`: one-shot synthesis of all 14 profiles to `demo/*.wav`
  (outputs gitignored; script committed).

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

# edge-fx-tts

[![Go](https://img.shields.io/github/go-mod/go-version/OodavidsinoO/edge-fx-tts)](go.mod)
[![Release](https://img.shields.io/github/v/release/OodavidsinoO/edge-fx-tts)](https://github.com/OodavidsinoO/edge-fx-tts/releases)
[![CI](https://github.com/OodavidsinoO/edge-fx-tts/actions/workflows/ci.yml/badge.svg)](https://github.com/OodavidsinoO/edge-fx-tts/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/OodavidsinoO/edge-fx-tts)](LICENSE)

English | [简体中文](README.zh-CN.md)

## What is edge-fx-tts?

edge-fx-tts is a Go post-processing audio-effects engine on top of Microsoft
Edge TTS. It synthesizes speech with Edge TTS (which returns a **24 kHz,
MPEG-2 Layer 3, mono** stream), decodes that stream, and runs it through a
configurable effect chain before writing out **16-bit PCM WAV** — or MP3 via
an optional ffmpeg encode step.

The built-in CLI (`cmd/edgefx`) exposes the whole engine end to end:

- `--profile none` (the default) writes the synthesized MP3 stream **verbatim**
  — the original Edge TTS capability, untouched, no ffmpeg required.
- Any other profile decodes the stream and runs a preset effect chain:
  upmix to stereo → EQ/dynamics → time-based effects → limiter → WAV.

## Features

- **MP3 passthrough** — `--profile none` copies the Edge TTS stream byte for
  byte; a plain text→MP3 workflow needs nothing but the CLI.
- **13-effect chain** — `upmix`, `eq`, `compressor`, `deesser`, `chorus`,
  `delay`, `fdnreverb`, `limiter`, `formant` (self-built cepstral
  source-filter shifter), `pitchcorrector`, `flanger`, `gate`, `wsola`
  (duration-preserving spectral pitch shift).
- **14 built-in preset profiles** — filmai ×4 (D1–D4 movie-AI), broadcast ×4
  (E1–E3 announcer/radio/teleconference), aifake / doubledelay / scifiatmo
  (A/B/C demo presets), jarvis / edith / ai-modern (modern film-AI voices),
  plus a minimal `placeholder` stub.
- **Streaming pipeline** — decode → effect chain → WAV across three
  goroutines with bounded SPSC ring buffers and natural backpressure, plus a
  trailing tail so reverb/delay decays are not hard-cut.
- **Full CLI surface** — `--profile`, `--type`, `--text`, `--file`,
  `--output`, `--voice`, `--rate`, `--pitch`, `--volume`, `--format`.
- **Library API** — a small `tts.Synthesizer` seam keeps the engine
  independent of the synthesis backend; profiles, chain building, and the
  pipeline are standalone `pkg/` packages.

## Install

Install the CLI:

```bash
go install github.com/OodavidsinoO/edge-fx-tts/cmd/edgefx@latest
```

Or use it as a library:

```bash
go get github.com/OodavidsinoO/edge-fx-tts
```

Synthesis talks to the Edge TTS service over the network, so an internet
connection is required.

## CLI usage

```bash
edgefx -h
```

| Flag | Default | Description |
| --- | --- | --- |
| `--profile` | `none` | `none` = raw MP3 passthrough, or a built-in preset name |
| `--type` | `text` | Input type: `text` or `ssml`. The single positional argument `ssml` is still accepted for compatibility (explicit `--type` wins) |
| `--text` | `hello world` | Text (or SSML with `--type ssml`) input |
| `--file` | | Read the whole input (text or SSML) from a file; mutually exclusive with `--text` |
| `--output` | *(required)* | Output audio file path |
| `--voice` | | Voice short name, e.g. `en-US-GuyNeural`, `zh-CN-XiaoxiaoNeural` |
| `--rate` | | Speech rate, e.g. `+10%` |
| `--pitch` | | Speech pitch, e.g. `+5Hz` |
| `--volume` | | Speech volume, e.g. `+10%` |
| `--format` | `auto` | Output format: `mp3` or `wav`. `auto` infers from the output extension — a preset chain defaults to WAV, passthrough stays MP3 |

Output format rules:

- `--profile none` always writes the raw MP3 stream verbatim — `--format` is
  irrelevant and ffmpeg is never invoked.
- A preset chain writes WAV unless you pass `--format mp3` **or** an `.mp3`
  output path. In that case the pipeline renders to a temporary WAV and
  encodes with **ffmpeg** (`ffmpeg -y -i <tmp.wav> -b:a 192k <out.mp3>`).
- If ffmpeg is not installed, MP3 output fails with a clear error:
  `ffmpeg required for MP3 output; install ffmpeg or use .wav`.

### Examples

Plain text to MP3 — no profile, no ffmpeg, the stream passes through raw:

```bash
edgefx --text "Hello, world." --voice en-US-GuyNeural --output hello.mp3
```

SSML input (the positional `ssml` form is also accepted):

```bash
edgefx --type ssml --text '<speak version="1.0" xmlns="http://www.w3.org/2001/10/synthesis" xml:lang="en-US"><voice name="en-US-GuyNeural"><prosody rate="+10%">hello world</prosody></voice></speak>' --output hello.mp3
```

Effect preset to WAV:

```bash
edgefx --profile filmai-d2 --text "Hello from the machine." --output out.wav
```

Effect preset to MP3 (requires ffmpeg):

```bash
edgefx --profile filmai --text "Hello again." --format mp3 --output out.mp3
```

Read input from a file:

```bash
edgefx --file script.txt --profile broadcast-e2 --output out.wav
```

Voice and prosody tuning on top of a preset:

```bash
edgefx --profile broadcast --voice zh-CN-YunxiNeural --rate +10% --pitch +5Hz --text "你好，世界" --output out.wav
```

## Built-in preset profiles

All presets run at 24 kHz stereo and end with a peak limiter. Parameter
values below are the shipped defaults.

| Profile | Flavor | Key chain |
| --- | --- | --- |
| `aifake` | Synthetic-AI feel, intelligibility first (report §3.2 A) | HPF 100 Hz; comp 2:1 / −20 dB; bandpass 300–3400 Hz; chorus 22 ms ×3; FDN RT60 1.0 s wet 0.15 |
| `doubledelay` | Cinematic double + slapback (report §3.2 B) | HPF 80 Hz; comp 3:1 / −18 dB; chorus 25 ms ×2 wet 0.35; slapback 100 ms, zero feedback; FDN RT60 1.6 s |
| `scifiatmo` | Sci-fi atmosphere (report §3.2 C) | HPF 80 Hz; comp 4:1 / −16 dB; wide chorus 30 ms ×3; ambient delay 250 ms, FB 0.25; FDN RT60 3.0 s |
| `filmai` | D1 modern film-AI (JARVIS-style, near-field) | WSOLA −1 st; light chorus 20 ms ×2 / 10%; HPF 100 Hz; presence 3 kHz +1.5 dB; LPF 8.5 kHz; FDN RT60 0.2 s wet 0.1; comp 2.5:1; de-esser |
| `filmai-d2` | D2 modern AI with a light mechanical edge | Light pitch corrector (chromatic, amount 0.3 / 200 ms / block 8192) + formant shift 1.2; then the D1 chain |
| `filmai-d3` | D3 HAL/TARS calm server voice | WSOLA −2.5 st; gate −45 dB 10:1; LPF 8.5 kHz; comp 4:1 fast attack; FDN RT60 0.25 s wet 0.1; de-esser |
| `filmai-d4` | D4 micro-OS, close-mic, barely processed | 100 Hz +1.5 dB; 3 kHz +2.5 dB; gentle comp 1.5:1; FDN RT60 0.2 s wet 0.15 |
| `jarvis` | Modern film-AI, natural near-field (JARVIS-style) | WSOLA −1 st; light chorus 20 ms ×2 / 10%; HPF 100 Hz; presence 3 kHz +1.5 dB; LPF 8.5 kHz; FDN RT60 0.2 s wet 0.1; comp 2.5:1; de-esser |
| `edith` | Modern film-AI, cooler/digital (EDITH-style) | jarvis + formant shift 1.15; presence 3 kHz +2.5 dB; LPF 8.5 kHz; FDN RT60 0.2 s wet 0.08 |
| `ai-modern` | Modern film-AI with a light mechanical edge | jarvis + formant shift 1.2 + light pitch corrector (amount 0.3 / 200 ms / block 8192) |
| `broadcast` / `broadcast-e1` | E1 announcer (report §6.3) | Broadcast EQ curve (HP 85 Hz, +1.5 @250 Hz, −1.5 @800 Hz Q4, +2.5 @3 kHz, +1.5 @5.5 kHz, LP 7 kHz, −1.5 @7 kHz Q4); de-esser; comp 3:1; FDN RT60 0.25 s |
| `broadcast-e2` | E2 radio/DJ, denser | Same EQ with +3 dB @250 Hz; comp 5:1 fast; FDN RT60 0.25 s |
| `broadcast-e3` | E3 teleconference | Bandpass 300–3400 Hz; +1 dB @1 kHz; comp 5:1 very fast attack |

Notes:

- `filmai` is the D1 base chain (fully processed); `broadcast` and
  `broadcast-e1` share the same chain.
- The broadcast family follows feasibility report §6.3; the EBU R128
  **−23 LUFS** delivery calibration is **not** applied in-profile — apply an
  offline measurement/gain stage before broadcast delivery (report §6.4).
- `placeholder` is a minimal reserved stub (upmix + limiter) and is not a
  production flavor.

## Library API

The engine is a set of independent `pkg/` packages. Example
(`examples/edgefx/effects.go`, runnable via `go run ./examples/edgefx`):

```go
package main

import (
	"context"
	"fmt"
	"os"

	edgetts "github.com/OodavidsinoO/edge-fx-tts"
	"github.com/OodavidsinoO/edge-fx-tts/pkg/config"
	"github.com/OodavidsinoO/edge-fx-tts/pkg/effects"
	"github.com/OodavidsinoO/edge-fx-tts/pkg/pipeline"
	"github.com/OodavidsinoO/edge-fx-tts/pkg/tts"
)

func main() {
	ctx := context.Background()

	// 1. Synthesize through the tts.Synthesizer seam (edgetts implementation).
	synth := tts.NewEdgeTTS(edgetts.New(edgetts.WithVoice("en-US-GuyNeural")))
	stream, err := synth.Stream(ctx, "Hello from edge-fx-tts.")
	if err != nil {
		panic(err)
	}
	defer stream.Close()

	// 2. Load a preset effect chain (external override dir can be provided).
	spec, err := config.LoadProfile("filmai-d2", "")
	if err != nil {
		panic(err)
	}

	// 3. Build the effect nodes from the frozen ChainSpec.
	nodes, err := effects.BuildChain(spec)
	if err != nil {
		panic(err)
	}

	// 4. Run the streaming pipeline: decode -> chain -> WAV sink.
	out, err := os.Create("output.wav")
	if err != nil {
		panic(err)
	}
	defer out.Close()

	sink, err := pipeline.NewWAVSink(out, spec.SampleRate, spec.Channels)
	if err != nil {
		panic(err)
	}
	p, err := pipeline.New(stream, nodes, spec.SampleRate, spec.Channels, spec.ChunkSamples, sink)
	if err != nil {
		panic(err)
	}
	if err := p.Run(ctx); err != nil {
		panic(err)
	}
	fmt.Println("wrote output.wav")
}
```

The `tts.Synthesizer` seam (`Stream` / `StreamSSML`, both returning a
streaming MP3 `io.ReadCloser`) keeps the engine independent of the backend.
`tts.NewEdgeTTS` wraps the root package's `edgetts.Client`, which also offers
the familiar one-off helpers (`Save`, `Bytes`, `Stream`, `StreamSSML`, batch
and ZIP output, voice listing/filtering). Custom chains can be defined as
YAML/JSON and loaded with `config.Load` / `config.LoadBytes` instead of a
preset name.

## Development

```bash
go test ./...
go vet ./...
go test -race ./...
```

The test suite covers the CLI, the config schema, per-effect behavior
(identity/bypass, L/R isolation, quantization, gating, zero-alloc hot loops),
and the decoding/pipeline round trip. CI runs `go test ./...` + `go vet
./...` on push to `main` and on every PR.

## License

MIT — see [LICENSE](LICENSE). Copyright (c) 2024 虫子樱桃 (upstream
`lib-x/edgetts`), 2026 OodavidsinoO.

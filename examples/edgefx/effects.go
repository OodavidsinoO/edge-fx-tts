// Command edgefx demonstrates the edge-fx-tts library API end to end:
//
//	edge TTS synthesis -> decode -> effect chain -> WAV
//
// It mirrors exactly what the CLI does for a preset profile, using only the
// public packages (edgetts, pkg/config, pkg/effects, pkg/pipeline, pkg/tts),
// so it doubles as a starting point for embedding the pipeline in your own
// program.
//
// Usage:
//
//	go run ./examples/edgefx
//
// writes "out.wav" in the current directory; an optional first argument sets
// the output path. If the Edge TTS service is unreachable, the example still
// prints the resolved effect chain and exits with a friendly hint.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/OodavidsinoO/edge-fx-tts"
	"github.com/OodavidsinoO/edge-fx-tts/pkg/config"
	"github.com/OodavidsinoO/edge-fx-tts/pkg/effects"
	"github.com/OodavidsinoO/edge-fx-tts/pkg/pipeline"
	"github.com/OodavidsinoO/edge-fx-tts/pkg/tts"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	outPath := "out.wav"
	if len(args) > 0 && args[0] != "" {
		outPath = args[0]
	}
	ctx := context.Background()

	// Step 1 — build the synthesizer.
	//
	// The pipeline only knows the tts.Synthesizer seam (Stream/StreamSSML);
	// the concrete implementation here is the edgetts client, wrapped via
	// tts.NewEdgeTTS. Swap the client for any other Synthesizer to feed the
	// same chain with a different synthesis backend.
	synth := tts.NewEdgeTTS(edgetts.New())

	// Step 2 — load and freeze a preset profile.
	//
	// config.LoadProfile resolves "filmai-d2" into an immutable ChainSpec:
	// sample rate, channels, chunk size and the ordered (validated) stage
	// list. The empty overrideDir means the embedded profile is used; pass a
	// directory to override presets with external YAML files of the same name.
	spec, err := config.LoadProfile("filmai-d2", "")
	if err != nil {
		return fmt.Errorf("load profile: %w", err)
	}

	// Step 3 — build the effect node chain.
	//
	// effects.BuildChain turns the frozen spec into concrete []effects.Node
	// instances in stage order. Each node owns per-stream state, so a chain
	// must never be shared across pipelines.
	nodes, err := effects.BuildChain(spec)
	if err != nil {
		return fmt.Errorf("build chain: %w", err)
	}
	fmt.Printf("resolved chain for %q (%d Hz, %d ch, %d chunk samples):\n",
		"filmai-d2", spec.SampleRate, spec.Channels, spec.ChunkSamples)
	for _, st := range spec.Stages {
		fmt.Printf("  - %-15s %v\n", st.Name, st.Params)
	}

	// Step 4 — synthesize the input.
	//
	// Stream returns the Edge TTS MP3 byte stream (24 kHz mono as produced).
	// Decoding happens inside the pipeline, so the reader is consumed lazily.
	// If the service is unreachable, report it gracefully: the chain above is
	// already built and printed, so the example never dies silently.
	text := "This is the edge fx tts example. Film style dialogue, take two."
	reader, err := synth.Stream(ctx, text)
	if err != nil {
		return fmt.Errorf("synthesize (is the network up?): %w", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			log.Printf("close synth reader: %v", err)
		}
	}()

	// Step 5 — open the WAV output and build the sink.
	//
	// pipeline.NewWAVSink emits 16-bit PCM WAV and needs a seekable writer
	// (it patches the RIFF sizes on Close), hence a real file. The sink is
	// only a valid WAV once Close returns.
	out, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create output %s: %w", outPath, err)
	}
	defer func() {
		if err := out.Close(); err != nil {
			log.Printf("close output: %v", err)
		}
	}()
	sink, err := pipeline.NewWAVSink(out, spec.SampleRate, spec.Channels)
	if err != nil {
		return fmt.Errorf("wav sink: %w", err)
	}

	// Step 6 — wire stream + chain + sink into the pipeline and run it.
	//
	// pipeline.New runs decode -> effect chain -> sink across three
	// goroutines; chunkSamples is the mono block size the decoder produces.
	// Run blocks until EOF or ctx cancellation and joins all goroutines
	// before returning.
	p, err := pipeline.New(reader, nodes, spec.SampleRate, spec.Channels, spec.ChunkSamples, sink)
	if err != nil {
		return fmt.Errorf("pipeline: %w", err)
	}
	if err := p.Run(ctx); err != nil {
		return fmt.Errorf("run pipeline: %w", err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("sync output: %w", err)
	}

	// Step 7 — report the result.
	//
	// out.Close (deferred above) patches the RIFF header in place, so the
	// byte count below is the final WAV size.
	if fi, err := out.Stat(); err == nil {
		fmt.Printf("ok: %s (%d bytes)\n", outPath, fi.Size())
	} else {
		fmt.Printf("ok: %s\n", outPath)
	}
	return nil
}

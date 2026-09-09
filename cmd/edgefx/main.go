// Command edgefx is the edge-fx-tts CLI. --profile none passes the Edge TTS
// MP3 stream through verbatim (the library's original capability); any other
// profile runs the synthesized stream through the decode → effect chain →
// WAV pipeline.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

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

// run parses flags, synthesizes via the TTS backend, and either passes the
// MP3 stream through (profile none) or runs it through the effects pipeline
// (any other profile). It is separated from main for testability.
func run(args []string) error {
	fs := flag.NewFlagSet("edgefx", flag.ContinueOnError)
	var (
		profile = fs.String("profile", "none", "fx profile: none (passthrough) or a built-in preset")
		input   = fs.String("text", "hello world", "text or (with -type ssml) SSML input")
		output  = fs.String("output", "", "output audio file path (required)")
		voice   = fs.String("voice", "", "voice short name, e.g. zh-CN-XiaoxiaoNeural")
		rate    = fs.String("rate", "", "speech rate, e.g. +10%")
		pitch   = fs.String("pitch", "", "speech pitch, e.g. +5Hz")
		volume  = fs.String("volume", "", "speech volume, e.g. +10%")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *output == "" {
		return errors.New("--output is required")
	}

	opts := make([]edgetts.Option, 0, 4)
	if *voice != "" {
		opts = append(opts, edgetts.WithVoice(*voice))
	}
	if *rate != "" {
		opts = append(opts, edgetts.WithRate(*rate))
	}
	if *pitch != "" {
		opts = append(opts, edgetts.WithPitch(*pitch))
	}
	if *volume != "" {
		opts = append(opts, edgetts.WithVolume(*volume))
	}

	synth := tts.NewEdgeTTS(edgetts.New(opts...))
	ctx := context.Background()

	var reader io.ReadCloser
	var err error
	if hasSSMLFlag(fs.Args()) {
		reader, err = synth.StreamSSML(ctx, *input)
	} else {
		reader, err = synth.Stream(ctx, *input)
	}
	if err != nil {
		return err
	}
	defer reader.Close()

	// Passthrough: write the MP3 stream verbatim.
	if *profile == "none" || *profile == "" {
		return writeReaderToFile(reader, *output)
	}

	// Effects profile: load chain, build nodes, run the pipeline to WAV.
	spec, err := config.LoadProfile(*profile, "")
	if err != nil {
		return err
	}
	nodes, err := effects.BuildChain(spec)
	if err != nil {
		return err
	}

	out, err := os.Create(*output)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer out.Close()

	sink, err := pipeline.NewWAVSink(out, spec.SampleRate, spec.Channels)
	if err != nil {
		_ = out.Close()
		_ = os.Remove(*output)
		return fmt.Errorf("wav sink: %w", err)
	}
	p, err := pipeline.New(reader, nodes, spec.SampleRate, spec.Channels, spec.ChunkSamples, sink)
	if err != nil {
		_ = out.Close()
		_ = os.Remove(*output)
		return fmt.Errorf("pipeline: %w", err)
	}
	return p.Run(ctx)
}

// hasSSMLFlag reports whether the first positional argument is "ssml" (the
// input type), mirroring the demo CLI's -type flag via argument position.
func hasSSMLFlag(args []string) bool {
	return len(args) > 0 && strings.EqualFold(args[0], "ssml")
}

func writeReaderToFile(src io.Reader, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	if _, err := io.Copy(file, src); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("copy stream: %w (cleanup failed: %v)", err, removeErr)
		}
		return fmt.Errorf("copy stream: %w", err)
	}
	return nil
}

// Command edgefx is the edge-fx-tts CLI. --profile none passes the Edge TTS
// MP3 stream through verbatim (the library's original capability); any other
// profile runs the synthesized stream through the decode → effect chain →
// WAV pipeline, optionally transcoding to MP3 via ffmpeg (-format mp3).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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

// run parses flags, resolves the input text and output format, synthesizes
// via the TTS backend, and either passes the MP3 stream through (profile
// none) or runs it through the effects chain. It is separated from main for
// testability.
func run(args []string) error {
	fs := flag.NewFlagSet("edgefx", flag.ContinueOnError)
	var (
		profile   = fs.String("profile", "none", "fx profile: none (passthrough) or a built-in preset")
		input     = fs.String("text", "hello world", "text or (with -type ssml) SSML input")
		inputType = fs.String("type", "text", "input type: text or ssml (positional 'ssml' accepted when unset)")
		filePath  = fs.String("file", "", "read the input text from this file (mutually exclusive with -text)")
		output    = fs.String("output", "", "output audio file path (required)")
		format    = fs.String("format", "auto", "output format: auto, mp3, or wav (auto infers from the output extension)")
		voice     = fs.String("voice", "", "voice short name, e.g. zh-CN-XiaoxiaoNeural")
		rate      = fs.String("rate", "", "speech rate, e.g. +10%")
		pitch     = fs.String("pitch", "", "speech pitch, e.g. +5Hz")
		volume    = fs.String("volume", "", "speech volume, e.g. +10%")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *output == "" {
		return errors.New("--output is required")
	}

	// An explicitly set -type flag wins; otherwise the first positional
	// "ssml" argument keeps the demo CLI convention working.
	typeSet := false
	textSet := false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "type":
			typeSet = true
		case "text":
			textSet = true
		}
	})

	isSSML, err := resolveInputType(typeSet, *inputType, fs.Args())
	if err != nil {
		return err
	}
	text, err := resolveInput(*input, textSet, *filePath)
	if err != nil {
		return err
	}

	profileNone := *profile == "none" || *profile == ""
	outFormat, err := resolveFormat(*format, *output, profileNone)
	if err != nil {
		return err
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

	reader, err := runSynthesize(ctx, synth, text, isSSML, opts)
	if err != nil {
		return err
	}
	defer reader.Close()

	return writeOutput(ctx, reader, *profile, *output, outFormat)
}

// resolveInputType decides between text and SSML input: an explicitly set
// -type flag wins; otherwise the first positional "ssml" argument is honored
// for backward compatibility with the demo CLI convention.
func resolveInputType(typeSet bool, typeFlag string, args []string) (bool, error) {
	if typeSet {
		switch strings.ToLower(typeFlag) {
		case "text":
			return false, nil
		case "ssml":
			return true, nil
		default:
			return false, fmt.Errorf("--type must be \"text\" or \"ssml\", got %q", typeFlag)
		}
	}
	return hasSSMLFlag(args), nil
}

// resolveInput combines the -text and -file flags into a single input string.
// The two are mutually exclusive when -text is explicitly set; -file wins
// otherwise (the -text default is only a fallback).
func resolveInput(text string, textSet bool, filePath string) (string, error) {
	if filePath == "" {
		return text, nil
	}
	if textSet {
		return "", errors.New("--text and --file are mutually exclusive")
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read input file: %w", err)
	}
	return string(data), nil
}

// resolveFormat maps the -format flag to a concrete output format. "auto"
// infers it from the output file extension: .wav → wav, .mp3 → mp3, anything
// else → wav for effects profiles and mp3 for passthrough.
func resolveFormat(format, outputPath string, profileNone bool) (string, error) {
	switch strings.ToLower(format) {
	case "auto":
		switch {
		case strings.EqualFold(filepath.Ext(outputPath), ".wav"):
			return "wav", nil
		case strings.EqualFold(filepath.Ext(outputPath), ".mp3"):
			return "mp3", nil
		case profileNone:
			return "mp3", nil
		default:
			return "wav", nil
		}
	case "mp3":
		return "mp3", nil
	case "wav":
		return "wav", nil
	default:
		return "", fmt.Errorf("--format must be \"auto\", \"mp3\", or \"wav\", got %q", format)
	}
}

// hasSSMLFlag reports whether the first positional argument is "ssml" (the
// input type), mirroring the demo CLI's -type flag via argument position.
func hasSSMLFlag(args []string) bool {
	return len(args) > 0 && strings.EqualFold(args[0], "ssml")
}

// runSynthesize streams input through the TTS backend, choosing the SSML or
// plain-text entry point. It is extracted from run for testing with a fake
// Synthesizer.
func runSynthesize(ctx context.Context, synth tts.Synthesizer, input string, isSSML bool, opts []edgetts.Option) (io.ReadCloser, error) {
	if isSSML {
		return synth.StreamSSML(ctx, input, opts...)
	}
	return synth.Stream(ctx, input, opts...)
}

// writeOutput either passes the MP3 stream through verbatim (profile none —
// no decode, no ffmpeg) or renders it through the effects chain to the
// requested format: WAV directly, MP3 via an ffmpeg transcode of a temporary
// WAV.
func writeOutput(ctx context.Context, src io.Reader, profile, output, format string) error {
	if profile == "none" || profile == "" {
		return writeReaderToFile(src, output)
	}

	if format == "mp3" {
		ffmpeg, err := lookupFFmpeg()
		if err != nil {
			return err
		}
		return renderToMP3(ctx, src, output, profile, ffmpeg)
	}
	return renderToWAV(ctx, src, output, profile)
}

// lookupFFmpeg locates the ffmpeg binary, returning a clear error naming the
// missing dependency when it is not installed.
func lookupFFmpeg() (string, error) {
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return "", errors.New("ffmpeg required for MP3 output; install ffmpeg or use .wav")
		}
		return "", fmt.Errorf("find ffmpeg: %w", err)
	}
	return path, nil
}

// renderToWAV runs the synthesized stream through the profile's effect chain
// into a WAV file at output.
func renderToWAV(ctx context.Context, src io.Reader, output, profile string) error {
	spec, err := config.LoadProfile(profile, "")
	if err != nil {
		return err
	}
	nodes, err := effects.BuildChain(spec)
	if err != nil {
		return err
	}

	out, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer out.Close()

	sink, err := pipeline.NewWAVSink(out, spec.SampleRate, spec.Channels)
	if err != nil {
		_ = out.Close()
		_ = os.Remove(output)
		return fmt.Errorf("wav sink: %w", err)
	}
	p, err := pipeline.New(src, nodes, spec.SampleRate, spec.Channels, spec.ChunkSamples, sink)
	if err != nil {
		_ = out.Close()
		_ = os.Remove(output)
		return fmt.Errorf("pipeline: %w", err)
	}
	return p.Run(ctx)
}

// renderToMP3 runs the profile's effect chain into a temporary WAV file and
// transcodes it to MP3 with ffmpeg (already resolved by the caller).
func renderToMP3(ctx context.Context, src io.Reader, output, profile, ffmpeg string) error {
	spec, err := config.LoadProfile(profile, "")
	if err != nil {
		return err
	}
	nodes, err := effects.BuildChain(spec)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp("", "edgefx-*.wav")
	if err != nil {
		return fmt.Errorf("create temp wav: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	sink, err := pipeline.NewWAVSink(tmp, spec.SampleRate, spec.Channels)
	if err != nil {
		_ = tmp.Close()
		return fmt.Errorf("wav sink: %w", err)
	}
	p, err := pipeline.New(src, nodes, spec.SampleRate, spec.Channels, spec.ChunkSamples, sink)
	if err != nil {
		_ = tmp.Close()
		return fmt.Errorf("pipeline: %w", err)
	}
	if err := p.Run(ctx); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp wav: %w", err)
	}

	return convertWAVToMP3(ffmpeg, tmpPath, output)
}

// convertWAVToMP3 runs ffmpeg to transcode tmpWAV into outputMP3, passing
// ffmpeg's stderr through so failures are diagnosable.
func convertWAVToMP3(ffmpeg, tmpWAV, outputMP3 string) error {
	cmd := exec.Command(ffmpeg, "-y", "-i", tmpWAV, "-b:a", "192k", outputMP3)
	combined, err := cmd.CombinedOutput()
	if err != nil {
		if len(combined) > 0 {
			return fmt.Errorf("ffmpeg: %w\n%s", err, combined)
		}
		return fmt.Errorf("ffmpeg: %w", err)
	}
	return nil
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

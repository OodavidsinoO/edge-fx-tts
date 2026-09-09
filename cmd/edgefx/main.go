// Command edgefx is the edge-fx-tts CLI. V1: --profile none passthrough —
// synthesize text/SSML via the Edge TTS backend and write the MP3 stream
// verbatim to a file (the library's original capability). Effects profiles
// and the full flag surface land in later tickets.
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
	"github.com/OodavidsinoO/edge-fx-tts/pkg/tts"
)

func main() {
	var (
		profile = flag.String("profile", "none", "fx profile; only 'none' (passthrough) is implemented in this ticket")
		input   = flag.String("text", "hello world", "text or (with -type ssml) SSML input")
		output  = flag.String("output", "", "output audio file path (required for --profile none)")
		voice   = flag.String("voice", "", "voice short name, e.g. zh-CN-XiaoxiaoNeural")
		rate    = flag.String("rate", "", "speech rate, e.g. +10%")
		pitch   = flag.String("pitch", "", "speech pitch, e.g. +5Hz")
		volume  = flag.String("volume", "", "speech volume, e.g. +10%")
	)
	flag.Parse()

	if *profile != "none" {
		log.Fatalf("--profile %q: only 'none' (passthrough) is implemented in V1; effect profiles arrive in later tickets", *profile)
	}
	if *output == "" {
		log.Fatal("--output is required for --profile none")
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
	if hasSSMLFlag(flag.Args()) {
		reader, err = synth.StreamSSML(ctx, *input)
	} else {
		reader, err = synth.Stream(ctx, *input)
	}
	if err != nil {
		log.Fatal(err)
	}
	defer reader.Close()

	if err := writeReaderToFile(reader, *output); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("saved audio to %s\n", *output)
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

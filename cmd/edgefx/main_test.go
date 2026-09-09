package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OodavidsinoO/edge-fx-tts"
)

func TestWriteReaderToFileWritesBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.mp3")
	input := []byte("ID3\x00\x01fake mp3 bytes")

	if err := writeReaderToFile(bytes.NewReader(input), path); err != nil {
		t.Fatalf("writeReaderToFile returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, input) {
		t.Fatalf("content mismatch: got %d bytes, want %d", len(got), len(input))
	}
}

func TestWriteReaderToFileRemovesPartialOnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.mp3")

	// A reader that fails after emitting bytes.
	errReader := &errorReader{data: []byte("partial"), err: errors.New("boom")}

	err := writeReaderToFile(errReader, path)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("partial output file still exists after error: %v", statErr)
	}
}

func TestHasSSMLFlag(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"ssml"}, true},
		{[]string{"SSML"}, true},
		{[]string{"text"}, false},
		{nil, false},
		{[]string{"ssml", "-o", "x.mp3"}, true},
	}
	for _, c := range cases {
		if got := hasSSMLFlag(c.args); got != c.want {
			t.Fatalf("hasSSMLFlag(%q) = %v, want %v", c.args, got, c.want)
		}
	}
}

type errorReader struct {
	data []byte
	err  error
}

func (r *errorReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.data) == 0 {
		if r.err != nil {
			return 0, r.err
		}
		return 0, io.EOF
	}
	// 一次调用返回全部剩余数据并在之后返回 err。
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 && r.err != nil {
		return n, r.err
	}
	return n, nil
}

// recordingSynth is a fake tts.Synthesizer that records which entry point was
// called and what input it received, returning an in-memory reader.
type recordingSynth struct {
	calledSSML  bool
	calledText  bool
	lastInput   string
	lastOptions []edgetts.Option
}

func (s *recordingSynth) Stream(ctx context.Context, text string, opts ...edgetts.Option) (io.ReadCloser, error) {
	s.calledText = true
	s.lastInput = text
	s.lastOptions = opts
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (s *recordingSynth) StreamSSML(ctx context.Context, ssml string, opts ...edgetts.Option) (io.ReadCloser, error) {
	s.calledSSML = true
	s.lastInput = ssml
	s.lastOptions = opts
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func TestRunSynthesizeRoutesToStreamSSML(t *testing.T) {
	synth := &recordingSynth{}
	src := `<speak version="1.0">hello</speak>`

	reader, err := runSynthesize(context.Background(), synth, src, true, nil)
	if err != nil {
		t.Fatalf("runSynthesize: %v", err)
	}
	_ = reader.Close()

	if !synth.calledSSML {
		t.Fatal("expected StreamSSML for ssml input, got text path")
	}
	if synth.calledText {
		t.Fatal("Stream must not be called for ssml input")
	}
	if synth.lastInput != src {
		t.Fatalf("input = %q, want %q", synth.lastInput, src)
	}
}

func TestRunSynthesizeRoutesToStreamForText(t *testing.T) {
	synth := &recordingSynth{}

	reader, err := runSynthesize(context.Background(), synth, "plain hello", false, nil)
	if err != nil {
		t.Fatalf("runSynthesize: %v", err)
	}
	_ = reader.Close()

	if !synth.calledText {
		t.Fatal("expected Stream for text input, got ssml path")
	}
	if synth.calledSSML {
		t.Fatal("StreamSSML must not be called for text input")
	}
	if synth.lastInput != "plain hello" {
		t.Fatalf("input = %q, want %q", synth.lastInput, "plain hello")
	}
}

func TestRunSynthesizeForwardsOptions(t *testing.T) {
	synth := &recordingSynth{}
	opts := []edgetts.Option{edgetts.WithVoice("zh-CN-XiaoxiaoNeural")}

	reader, err := runSynthesize(context.Background(), synth, "hi", false, opts)
	if err != nil {
		t.Fatalf("runSynthesize: %v", err)
	}
	_ = reader.Close()

	if len(synth.lastOptions) != 1 {
		t.Fatalf("forwarded %d options, want 1", len(synth.lastOptions))
	}
}

func TestResolveInputTypeExplicitFlagWins(t *testing.T) {
	// Explicit -type text overrides a positional "ssml".
	isSSML, err := resolveInputType(true, "text", []string{"ssml"})
	if err != nil || isSSML {
		t.Fatalf("explicit text: isSSML=%v err=%v, want false/nil", isSSML, err)
	}

	isSSML, err = resolveInputType(true, "ssml", []string{"text"})
	if err != nil || !isSSML {
		t.Fatalf("explicit ssml: isSSML=%v err=%v, want true/nil", isSSML, err)
	}

	// No explicit flag: positional "ssml" keeps the demo convention.
	isSSML, err = resolveInputType(false, "text", []string{"SSML"})
	if err != nil || !isSSML {
		t.Fatalf("positional ssml: isSSML=%v err=%v, want true/nil", isSSML, err)
	}
}

func TestResolveInputTypeRejectsBadValue(t *testing.T) {
	_, err := resolveInputType(true, "ogg", nil)
	if err == nil {
		t.Fatal("expected error for invalid -type value")
	}
	if !strings.Contains(err.Error(), `"ogg"`) {
		t.Fatalf("error %q should name the bad value", err)
	}
}

func TestResolveInputReadsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input.txt")
	content := "hello from file"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write input file: %v", err)
	}

	got, err := resolveInput("", false, path)
	if err != nil {
		t.Fatalf("resolveInput: %v", err)
	}
	if got != content {
		t.Fatalf("got %q, want %q", got, content)
	}
}

func TestResolveInputFileAndTextMutuallyExclusive(t *testing.T) {
	_, err := resolveInput("explicit text", true, "/some/file.txt")
	if err == nil {
		t.Fatal("expected error when both -text and -file are set")
	}
}

func TestResolveInputFileError(t *testing.T) {
	_, err := resolveInput("", false, filepath.Join(t.TempDir(), "missing.txt"))
	if err == nil {
		t.Fatal("expected error for unreadable input file")
	}
}

func TestResolveFormatAutoInference(t *testing.T) {
	cases := []struct {
		name        string
		format      string
		output      string
		profileNone bool
		want        string
	}{
		{"wav extension", "auto", "out.wav", false, "wav"},
		{"WAV extension case-insensitive", "auto", "out.WAV", false, "wav"},
		{"mp3 extension", "auto", "out.mp3", false, "mp3"},
		{"MP3 extension case-insensitive", "auto", "out.MP3", false, "mp3"},
		{"unknown extension preset", "auto", "out.ogg", false, "wav"},
		{"unknown extension passthrough", "auto", "out.ogg", true, "mp3"},
		{"no extension preset", "auto", "out", false, "wav"},
		{"no extension passthrough", "auto", "out", true, "mp3"},
		{"explicit mp3", "mp3", "out.wav", false, "mp3"},
		{"explicit wav", "wav", "out.mp3", false, "wav"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveFormat(c.format, c.output, c.profileNone)
			if err != nil {
				t.Fatalf("resolveFormat: %v", err)
			}
			if got != c.want {
				t.Fatalf("format = %q, want %q", got, c.want)
			}
		})
	}
}

func TestResolveFormatRejectsBadValue(t *testing.T) {
	_, err := resolveFormat("flac", "out.mp3", false)
	if err == nil {
		t.Fatal("expected error for invalid -format value")
	}
}

// TestLookupFFmpegMissing verifies the clear missing-dependency error when
// ffmpeg is absent from PATH.
func TestLookupFFmpegMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty dir: nothing resolvable

	_, err := lookupFFmpeg()
	if err == nil {
		t.Fatal("expected error when ffmpeg is missing")
	}
	if err.Error() != "ffmpeg required for MP3 output; install ffmpeg or use .wav" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestLookupFFmpegFound verifies the probe resolves a real executable.
func TestLookupFFmpegFound(t *testing.T) {
	dir := t.TempDir()
	writeFakeFFmpeg(t, dir, "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir)

	path, err := lookupFFmpeg()
	if err != nil {
		t.Fatalf("lookupFFmpeg: %v", err)
	}
	if path != filepath.Join(dir, "ffmpeg") {
		t.Fatalf("path = %q, want %q", path, filepath.Join(dir, "ffmpeg"))
	}
}

// TestWriteOutputPassthroughSkipsFFmpeg pins the --profile none invariant:
// even with an MP3 format request, the stream is copied verbatim with no
// ffmpeg involvement.
func TestWriteOutputPassthroughSkipsFFmpeg(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "out.mp3")
	input := []byte("ID3\x00raw passthrough bytes")
	// A PATH with no ffmpeg: a transcode attempt would fail loudly.
	t.Setenv("PATH", t.TempDir())

	if err := writeOutput(context.Background(), bytes.NewReader(input), "none", output, "mp3"); err != nil {
		t.Fatalf("writeOutput passthrough: %v", err)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, input) {
		t.Fatalf("passthrough output = %d bytes, want %d verbatim", len(got), len(input))
	}
}

// TestConvertWAVToMP3FakeFfmpeg exercises the transcode invocation: the fake
// ffmpeg records its arguments and copies the input to the output.
func TestConvertWAVToMP3FakeFfmpeg(t *testing.T) {
	dir := t.TempDir()
	ffmpegBin := filepath.Join(dir, "ffmpeg")
	argsFile := filepath.Join(dir, "ffmpeg.args")
	script := `#!/bin/sh
in=""
out=""
prev=""
for a in "$@"; do
  [ "$prev" = "-i" ] && in="$a"
  prev="$a"
  out="$a"
done
printf '%s\n' "$@" > "` + argsFile + `"
cp "$in" "$out"
`
	writeFakeFFmpeg(t, dir, script)

	tmpWAV := filepath.Join(dir, "in.wav")
	outMP3 := filepath.Join(dir, "out.mp3")
	if err := os.WriteFile(tmpWAV, []byte("RIFF fake wav"), 0o644); err != nil {
		t.Fatalf("write temp wav: %v", err)
	}

	if err := convertWAVToMP3(ffmpegBin, tmpWAV, outMP3); err != nil {
		t.Fatalf("convertWAVToMP3: %v", err)
	}

	got, err := os.ReadFile(outMP3)
	if err != nil {
		t.Fatalf("read mp3 output: %v", err)
	}
	if string(got) != "RIFF fake wav" {
		t.Fatalf("mp3 output = %q, want copied wav content", got)
	}

	argBytes, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read recorded args: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(argBytes)), "\n")
	wantArgs := []string{"-y", "-i", tmpWAV, "-b:a", "192k", outMP3}
	if len(args) != len(wantArgs) {
		t.Fatalf("ffmpeg args = %v, want %v", args, wantArgs)
	}
	for i := range wantArgs {
		if args[i] != wantArgs[i] {
			t.Fatalf("ffmpeg arg[%d] = %q, want %q (args %v)", i, args[i], wantArgs[i], args)
		}
	}
}

// TestConvertWAVToMP3PropagatesStderr pins that ffmpeg's stderr is surfaced
// when the transcode fails.
func TestConvertWAVToMP3PropagatesStderr(t *testing.T) {
	dir := t.TempDir()
	ffmpegBin := filepath.Join(dir, "ffmpeg")
	writeFakeFFmpeg(t, dir, "#!/bin/sh\necho 'Unknown decoder' >&2\nexit 1\n")

	err := convertWAVToMP3(ffmpegBin, filepath.Join(dir, "in.wav"), filepath.Join(dir, "out.mp3"))
	if err == nil {
		t.Fatal("expected error from failing ffmpeg")
	}
	if !strings.Contains(err.Error(), "ffmpeg:") {
		t.Fatalf("error %q should carry the ffmpeg prefix", err)
	}
	if !strings.Contains(err.Error(), "Unknown decoder") {
		t.Fatalf("error %q should carry ffmpeg stderr", err)
	}
}

func writeFakeFFmpeg(t *testing.T, dir, script string) {
	t.Helper()
	bin := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ffmpeg: %v", err)
	}
}

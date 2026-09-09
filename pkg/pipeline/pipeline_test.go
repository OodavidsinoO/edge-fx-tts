package pipeline

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OodavidsinoO/edge-fx-tts/pkg/config"
	"github.com/OodavidsinoO/edge-fx-tts/pkg/effects"
)

// buildTestChain builds a minimal chain (upmix + limiter) from a ChainSpec.
func buildTestChain(t *testing.T) []effects.Node {
	t.Helper()
	spec, err := config.Build(&config.Config{
		Version: 1,
		Stages: []config.Stage{
			{Name: "upmix"},
			{Name: "limiter"},
		},
	})
	if err != nil {
		t.Fatalf("config.Build: %v", err)
	}
	nodes, err := effects.BuildChain(spec)
	if err != nil {
		t.Fatalf("effects.BuildChain: %v", err)
	}
	return nodes
}

// TestPipelineEndToEnd decodes the committed sample through the chain and
// writes a WAV, then verifies the RIFF header and data size.
func TestPipelineEndToEnd(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "internal", "decode", "testdata", "sample.mp3"))
	if err != nil {
		t.Fatalf("open sample: %v", err)
	}
	defer f.Close()

	out, err := os.CreateTemp(t.TempDir(), "out*.wav")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer out.Close()

	sink, err := NewWAVSink(out, 24000, 2)
	if err != nil {
		t.Fatalf("NewWAVSink: %v", err)
	}
	p, err := New(f, buildTestChain(t), 24000, 2, 4096, sink)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify the WAV header.
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek: %v", err)
	}
	header := make([]byte, 44)
	if _, err := io.ReadFull(out, header); err != nil {
		t.Fatalf("read header: %v", err)
	}
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		t.Fatalf("bad RIFF/WAVE magic")
	}
	if string(header[12:16]) != "fmt " || string(header[36:40]) != "data" {
		t.Fatalf("bad chunk ids")
	}
	if got := binary.LittleEndian.Uint16(header[22:24]); got != 2 {
		t.Fatalf("channels = %d, want 2", got)
	}
	if got := binary.LittleEndian.Uint32(header[24:28]); got != 24000 {
		t.Fatalf("sampleRate = %d, want 24000", got)
	}
	dataSize := binary.LittleEndian.Uint32(header[40:44])
	if dataSize == 0 {
		t.Fatal("data size is 0")
	}
	// Data size must be even (16-bit stereo).
	if dataSize%2 != 0 {
		t.Fatalf("data size %d not even", dataSize)
	}
}

// TestPipelineCancellation verifies ctx cancel stops the pipeline promptly
// with no goroutine leak (Run returns).
func TestPipelineCancellation(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "internal", "decode", "testdata", "sample.mp3"))
	if err != nil {
		t.Fatalf("open sample: %v", err)
	}
	defer f.Close()

	out, err := os.CreateTemp(t.TempDir(), "out*.wav")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer out.Close()

	sink, err := NewWAVSink(out, 24000, 2)
	if err != nil {
		t.Fatalf("NewWAVSink: %v", err)
	}
	p, err := New(f, buildTestChain(t), 24000, 2, 4096, sink)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected ctx error on cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation (goroutine leak)")
	}
}

// TestWAVSinkHeader verifies a non-seekable writer is rejected on Close.
func TestWAVSinkHeader(t *testing.T) {
	var buf bytes.Buffer
	s, err := NewWAVSink(&buf, 24000, 2)
	if err != nil {
		t.Fatalf("NewWAVSink: %v", err)
	}
	if err := s.Write([]float32{0.5, -0.5, 0.25, -0.25}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// bytes.Buffer is not a WriteSeeker, so Close must fail.
	if err := s.Close(); err == nil {
		t.Fatal("expected error: bytes.Buffer not seekable")
	}
}

// TestWAVSinkSeekable verifies the header is patched correctly on a seekable
// writer.
func TestWAVSinkSeekable(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "out*.wav")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer out.Close()
	s, err := NewWAVSink(out, 24000, 2)
	if err != nil {
		t.Fatalf("NewWAVSink: %v", err)
	}
	if err := s.Write([]float32{0.5, -0.5, 0.25, -0.25}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek: %v", err)
	}
	header := make([]byte, 44)
	if _, err := io.ReadFull(out, header); err != nil {
		t.Fatalf("read header: %v", err)
	}
	// 4 float32 = 2 stereo frames = 4 samples * 2 bytes = 8 data bytes.
	if got := binary.LittleEndian.Uint32(header[40:44]); got != 8 {
		t.Fatalf("data size = %d, want 8", got)
	}
	if got := binary.LittleEndian.Uint32(header[4:8]); got != 36+8 {
		t.Fatalf("RIFF size = %d, want %d", got, 36+8)
	}
}

// TestPipelineEmptyChainRejected verifies New rejects an empty chain.
func TestPipelineEmptyChainRejected(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "internal", "decode", "testdata", "sample.mp3"))
	if err != nil {
		t.Fatalf("open sample: %v", err)
	}
	defer f.Close()
	out, err := os.CreateTemp(t.TempDir(), "out*.wav")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer out.Close()
	sink, err := NewWAVSink(out, 24000, 2)
	if err != nil {
		t.Fatalf("NewWAVSink: %v", err)
	}
	if _, err := New(f, nil, 24000, 2, 4096, sink); err == nil {
		t.Fatal("expected error for empty chain")
	}
}

// TestPipelineSinkErrorPropagates verifies a sink write error surfaces from
// Run.
func TestPipelineSinkErrorPropagates(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "internal", "decode", "testdata", "sample.mp3"))
	if err != nil {
		t.Fatalf("open sample: %v", err)
	}
	defer f.Close()
	p, err := New(f, buildTestChain(t), 24000, 2, 4096, &errorSink{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := p.Run(context.Background()); err == nil {
		t.Fatal("expected sink error to propagate")
	}
}

type errorSink struct{}

func (e *errorSink) Write([]float32) error { return errors.New("sink boom") }
func (e *errorSink) Close() error          { return nil }

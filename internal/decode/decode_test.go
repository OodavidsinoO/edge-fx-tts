package decode

import (
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestDecodeSampleStream decodes the committed 24 kHz MPEG-2 MP3 sample and
// checks the produced frames and metadata. The sample is a real Edge TTS
// synthesis of a known sentence; exact RMS is not asserted (MP3 decode is
// deterministic for a fixed library version, so the bounds are stable).
func TestDecodeSampleStream(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "sample.mp3"))
	if err != nil {
		t.Fatalf("open sample: %v", err)
	}
	defer f.Close()

	d, err := NewMinimp3Decoder(f)
	if err != nil {
		t.Fatalf("NewMinimp3Decoder: %v", err)
	}
	defer d.Close()

	if d.SampleRate() != 24000 {
		t.Fatalf("SampleRate = %d, want 24000", d.SampleRate())
	}
	if d.Channels() != 1 {
		t.Fatalf("Channels = %d, want 1 (mono)", d.Channels())
	}

	var total int
	var sumSquares float64
	buf := make([]float32, 512)
	for {
		n, err := d.Read(buf)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("Read: %v", err)
		}
		for i := range n {
			v := float64(buf[i])
			if v < -1.0001 || v > 1.0001 {
				t.Fatalf("sample out of range: %v", v)
				return
			}
			sumSquares += v * v
			total++
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}

	// The file holds 181 MPEG-2 frames x 576 samples = 104256 mono samples
	// (4.344 s at 24 kHz); minimp3 drops the final incomplete/padded frames,
	// so the observed count is slightly lower (177 frames = 101952). The
	// bound catches channel-width regressions: consuming a mono stream as
	// stereo halves the count to ~52k, far below this floor.
	if total < 100000 {
		t.Fatalf("decoded %d frames, want >= 100000 (mono stream; 2x regression would yield ~52k)", total)
	}
	if rms := math.Sqrt(sumSquares / float64(total)); rms < 0.001 {
		t.Fatalf("RMS = %v, sample appears silent", rms)
	}
}

// TestDecodeChunkedReads feeds the sample through a reader that returns tiny
// pieces, exercising the decoder's buffering and downmix state across Read
// boundaries.
func TestDecodeChunkedReads(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "sample.mp3"))
	if err != nil {
		t.Fatalf("open sample: %v", err)
	}
	defer f.Close()

	chunky := &tinyReader{r: f, chunk: 137}
	d, err := NewMinimp3Decoder(chunky)
	if err != nil {
		t.Fatalf("NewMinimp3Decoder: %v", err)
	}
	defer d.Close()

	var total int
	buf := make([]float32, 31) // odd size to stress cross-sample buffering
	for {
		n, err := d.Read(buf)
		total += n
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
	}
	if total == 0 {
		t.Fatal("no frames decoded via chunked reads")
	}
}

// TestDecodeTinyBuffer reads with a 1-sample buffer to force the buffered-PCM
// path to interleave correctly.
func TestDecodeTinyBuffer(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "sample.mp3"))
	if err != nil {
		t.Fatalf("open sample: %v", err)
	}
	defer f.Close()

	d, err := NewMinimp3Decoder(f)
	if err != nil {
		t.Fatalf("NewMinimp3Decoder: %v", err)
	}
	defer d.Close()

	one := make([]float32, 1)
	var n int
	for {
		_, err := d.Read(one)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		n++
	}
	if n < 24000 {
		t.Fatalf("tiny reads decoded %d frames, want >= 24000", n)
	}
}

// TestDecodeClosed returns ErrClosed after Close.
func TestDecodeClosed(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "sample.mp3"))
	if err != nil {
		t.Fatalf("open sample: %v", err)
	}
	defer f.Close()

	d, err := NewMinimp3Decoder(f)
	if err != nil {
		t.Fatalf("NewMinimp3Decoder: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := d.Read(make([]float32, 8)); !errors.Is(err, ErrClosed) {
		t.Fatalf("Read after Close = %v, want ErrClosed", err)
	}
}

type tinyReader struct {
	r     io.Reader
	chunk int
}

func (t *tinyReader) Read(p []byte) (int, error) {
	if len(p) > t.chunk {
		p = p[:t.chunk]
	}
	return t.r.Read(p)
}

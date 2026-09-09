// Package decode turns the Edge TTS MP3 byte stream into mono float32 PCM
// frames, isolating the mp3 implementation behind a seam.
package decode

import (
	"encoding/binary"
	"errors"
	"io"

	"github.com/OodavidsinoO/edge-fx-tts/internal/decode/minimp3"
)

// ErrClosed is returned when reading from a closed decoder.
var ErrClosed = errors.New("decode: decoder is closed")

// Decoder is the decoding seam: streaming MP3 bytes in, mono PCM frames
// (float32, normalized to [-1, 1]) out.
type Decoder interface {
	// SampleRate returns the decoded stream sample rate (0 until the first
	// frame has been decoded; Edge TTS produces 24000).
	SampleRate() int
	// Channels returns the output channel count (1: the decoder downmixes any
	// source channel count to mono).
	Channels() int
	// Read fills p with mono samples; n is the number of samples written.
	// It returns io.EOF once the input stream is exhausted. Implementations
	// must tolerate callers passing arbitrary buffer sizes and must not
	// return io.EOF while the underlying stream is still producing audio.
	Read(p []float32) (n int, err error)
	// Close releases decoder resources.
	Close() error
}

// Minimp3Decoder is the tosone/minimp3 implementation of Decoder.
type Minimp3Decoder struct {
	dec *minimp3.Decoder
	// buf holds leftover interleaved 16-bit PCM bytes from the decoder.
	buf []byte
	// pending holds mono float32 samples decoded during the initial
	// SampleRate probe.
	pending []float32
	// sampleRate is captured once after the first frame decode; the
	// underlying decoder field is written by its producer goroutine, so it
	// is not read directly outside the constructor.
	sampleRate int
	// channels is the source channel count captured with sampleRate; it
	// decides the per-sample byte width in readFromMinimp3 (mono: 2 bytes,
	// stereo: 4 bytes).
	channels int
	// raw is the reusable scratch buffer for one decoder.Read call.
	raw    []byte
	closed bool
}

// NewMinimp3Decoder wraps r (an MP3 byte stream) with a minimp3 decoder. The
// caller must keep r alive and EOF-free until the whole utterance has been
// decoded; a premature EOF permanently terminates the decoder.
func NewMinimp3Decoder(r io.Reader) (*Minimp3Decoder, error) {
	dec, err := minimp3.NewDecoder(r)
	if err != nil {
		return nil, err
	}
	d := &Minimp3Decoder{dec: dec}
	// First-frame probe: decode a handful of samples so SampleRate and
	// Channels are populated before the decoder is handed out.
	probe := make([]float32, 8)
	if n, err := d.readFromMinimp3(probe); err != nil && !errors.Is(err, io.EOF) {
		dec.Close()
		return nil, err
	} else if n > 0 {
		d.pending = append(d.pending, probe[:n]...)
	}
	sr, ch, _, _ := dec.Meta()
	d.sampleRate = sr
	d.channels = ch
	return d, nil
}

// SampleRate implements Decoder.
func (d *Minimp3Decoder) SampleRate() int { return d.sampleRate }

// Channels implements Decoder. The output is always mono.
func (d *Minimp3Decoder) Channels() int { return 1 }

// Read implements Decoder.
func (d *Minimp3Decoder) Read(p []float32) (int, error) {
	if d.closed {
		return 0, ErrClosed
	}
	n := 0
	// Serve any samples decoded by the constructor probe first.
	if len(d.pending) > 0 {
		n = copy(p, d.pending)
		d.pending = d.pending[n:]
		if n == len(p) {
			return n, nil
		}
	}
	for n < len(p) {
		// Consume buffered PCM first (mono: 2 bytes/sample, stereo: 4).
		width := 2
		if d.channels == 2 {
			width = 4
		}
		for len(d.buf) >= width && n < len(p) {
			p[n] = d.sampleFromBytes(d.buf[:width])
			d.buf = d.buf[width:]
			n++
		}
		if n == len(p) {
			return n, nil
		}
		more, err := d.readFromMinimp3(p[n:])
		n += more
		if err != nil {
			if errors.Is(err, io.EOF) {
				if n > 0 {
					return n, nil
				}
				return 0, io.EOF
			}
			return n, err
		}
	}
	return n, nil
}

// sampleFromBytes converts one PCM sample (2 bytes mono or 4 bytes stereo
// interleaved) to a mono float32 in [-1, 1].
func (d *Minimp3Decoder) sampleFromBytes(b []byte) float32 {
	if d.channels == 2 {
		l := int16(binary.LittleEndian.Uint16(b[0:2]))
		r := int16(binary.LittleEndian.Uint16(b[2:4]))
		return (float32(l) + float32(r)) / 2 / 32768
	}
	s := int16(binary.LittleEndian.Uint16(b[0:2]))
	return float32(s) / 32768
}

// readFromMinimp3 pulls one chunk from the underlying decoder, converts it
// into p (mono float32), and buffers any unused tail bytes for later reads.
func (d *Minimp3Decoder) readFromMinimp3(p []float32) (int, error) {
	if d.raw == nil {
		d.raw = make([]byte, 8192)
	}
	// minimp3 returns io.EOF only once its buffer is drained after the
	// original stream EOF, so partial reads with more=0 and err=nil are
	// impossible; the Read loop above relies on that.
	more, err := d.dec.Read(d.raw)
	raw := d.raw[:more]

	// Consume into p and leave the remainder in d.buf for the next Read.
	width := 2
	if d.channels == 2 {
		width = 4
	}
	consumed := 0
	for consumed+width <= len(raw) && consumed/width < len(p) {
		p[consumed/width] = d.sampleFromBytes(raw[consumed : consumed+width])
		consumed += width
	}
	d.buf = append(d.buf, raw[consumed:]...)
	return consumed / width, err
}

// Close implements Decoder.
func (d *Minimp3Decoder) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	d.dec.Close()
	return nil
}

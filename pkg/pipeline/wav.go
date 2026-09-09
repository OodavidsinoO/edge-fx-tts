package pipeline

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// wavSink writes stereo-interleaved float32 frames as 16-bit PCM WAV. The
// RIFF header is written on Close (after the data size is known), so the
// output is only valid once Close returns.
type wavSink struct {
	w          io.Writer
	sampleRate int
	channels   int
	dataBytes  int
	closed     bool
}

// NewWAVSink returns a Sink that writes 16-bit PCM WAV to w. A placeholder
// RIFF header is written at construction so the data chunk starts at offset
// 44 regardless of data size; Close patches the sizes. w must support seeking.
func NewWAVSink(w io.Writer, sampleRate, channels int) (Sink, error) {
	s := &wavSink{w: w, sampleRate: sampleRate, channels: channels}
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], 36) // patch on Close
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16) // fmt chunk size
	binary.LittleEndian.PutUint16(header[20:22], 1)  // PCM
	binary.LittleEndian.PutUint16(header[22:24], uint16(s.channels))
	binary.LittleEndian.PutUint32(header[24:28], uint32(s.sampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(s.sampleRate*s.channels*2)) // byte rate
	binary.LittleEndian.PutUint16(header[32:34], uint16(s.channels*2))              // block align
	binary.LittleEndian.PutUint16(header[34:36], 16)                                // bits per sample
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], 0) // patch on Close
	if _, err := w.Write(header); err != nil {
		return nil, fmt.Errorf("wav: placeholder header: %w", err)
	}
	return s, nil
}

// Write implements Sink. buf is stereo-interleaved float32 in [-1, 1].
func (s *wavSink) Write(buf []float32) error {
	if s.closed {
		return fmt.Errorf("wav: write after close")
	}
	// Convert float32 → 16-bit PCM, clamping to [-1, 1].
	pcm := make([]byte, len(buf)*2)
	for i, v := range buf {
		clamped := v
		if clamped > 1 {
			clamped = 1
		} else if clamped < -1 {
			clamped = -1
		}
		sample := int16(math.Round(float64(clamped) * 32767))
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(sample))
	}
	if _, err := s.w.Write(pcm); err != nil {
		return fmt.Errorf("wav: write: %w", err)
	}
	s.dataBytes += len(pcm)
	return nil
}

// Close implements Sink. It writes the RIFF header (with the now-known data
// size) by seeking back to the start of the stream.
func (s *wavSink) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	// The header is 44 bytes; the data chunk follows. We need to patch the
	// header, so the underlying writer must support seeking.
	seeker, ok := s.w.(io.WriteSeeker)
	if !ok {
		return fmt.Errorf("wav: writer must support seeking to write the header")
	}
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+s.dataBytes))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16) // fmt chunk size
	binary.LittleEndian.PutUint16(header[20:22], 1)  // PCM
	binary.LittleEndian.PutUint16(header[22:24], uint16(s.channels))
	binary.LittleEndian.PutUint32(header[24:28], uint32(s.sampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(s.sampleRate*s.channels*2)) // byte rate
	binary.LittleEndian.PutUint16(header[32:34], uint16(s.channels*2))              // block align
	binary.LittleEndian.PutUint16(header[34:36], 16)                                // bits per sample
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(s.dataBytes))

	if _, err := seeker.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("wav: seek: %w", err)
	}
	if _, err := s.w.Write(header); err != nil {
		return fmt.Errorf("wav: write header: %w", err)
	}
	return nil
}

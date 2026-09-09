// Package tts exposes the TTS backend behind a small seam so the effects
// pipeline never depends on a concrete synthesis implementation.
package tts

import (
	"context"
	"io"

	"github.com/OodavidsinoO/edge-fx-tts"
)

// Synthesizer streams synthesized MP3 audio for an input request. Chunk
// boundaries are internal to the implementation: the returned reader must not
// return io.EOF until the whole utterance has been synthesized.
type Synthesizer interface {
	// Stream synthesizes text and returns a streaming MP3 reader.
	Stream(ctx context.Context, text string, opts ...edgetts.Option) (io.ReadCloser, error)
	// StreamSSML synthesizes an SSML document and returns a streaming MP3 reader.
	StreamSSML(ctx context.Context, ssml string, opts ...edgetts.Option) (io.ReadCloser, error)
}

// EdgeTTS is the lib-x/edgetts implementation of Synthesizer.
type EdgeTTS struct {
	client *edgetts.Client
}

// NewEdgeTTS wraps a Client behind the Synthesizer seam.
func NewEdgeTTS(client *edgetts.Client) *EdgeTTS {
	return &EdgeTTS{client: client}
}

// Stream implements Synthesizer.
func (e *EdgeTTS) Stream(ctx context.Context, text string, opts ...edgetts.Option) (io.ReadCloser, error) {
	return e.client.Stream(ctx, text, opts...)
}

// StreamSSML implements Synthesizer.
func (e *EdgeTTS) StreamSSML(ctx context.Context, ssml string, opts ...edgetts.Option) (io.ReadCloser, error) {
	return e.client.StreamSSML(ctx, ssml, opts...)
}

// Package pipeline wires the streaming decode → effect chain → encode stages
// into a three-goroutine pipeline with bounded SPSC ring buffers. Memory is
// bounded by the ring capacity; backpressure propagates naturally because a
// full ring blocks the producer.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/OodavidsinoO/edge-fx-tts/internal/decode"
	"github.com/OodavidsinoO/edge-fx-tts/pkg/effects"
)

// Sink writes stereo-interleaved float32 frames. Implementations must be
// safe for a single goroutine (the encoder goroutine).
type Sink interface {
	// Write consumes one stereo-interleaved float32 block.
	Write(buf []float32) error
	// Close finalizes the sink (e.g. writes the WAV header).
	Close() error
}

// ring is a bounded SPSC ring buffer of stereo-interleaved float32 blocks.
// It is used by exactly one producer and one consumer goroutine.
type ring struct {
	mu       sync.Mutex
	notFull  *sync.Cond
	notEmpty *sync.Cond
	closed   bool
	items    [][]float32
	head     int
	tail     int
	count    int
}

func newRing(capacity int) *ring {
	r := &ring{items: make([][]float32, capacity)}
	r.notFull = sync.NewCond(&r.mu)
	r.notEmpty = sync.NewCond(&r.mu)
	return r
}

// push adds a block, blocking while the ring is full. It returns false if the
// ring has been closed.
func (r *ring) push(b []float32) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for r.count == len(r.items) && !r.closed {
		r.notFull.Wait()
	}
	if r.closed {
		return false
	}
	r.items[r.tail] = b
	r.tail = (r.tail + 1) % len(r.items)
	r.count++
	r.notEmpty.Signal()
	return true
}

// pop removes a block, blocking while the ring is empty. ok is false when the
// ring is closed and drained.
func (r *ring) pop() (b []float32, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for r.count == 0 && !r.closed {
		r.notEmpty.Wait()
	}
	if r.count == 0 {
		return nil, false
	}
	b = r.items[r.head]
	r.items[r.head] = nil
	r.head = (r.head + 1) % len(r.items)
	r.count--
	r.notFull.Signal()
	return b, true
}

// close wakes all waiters; subsequent push returns false and pop returns
// drained items then ok=false.
func (r *ring) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	r.notFull.Broadcast()
	r.notEmpty.Broadcast()
}

// Pipeline runs decode → chain → sink across three goroutines.
type Pipeline struct {
	decoder      decode.Decoder
	chain        []effects.Node
	sampleRate   int
	channels     int
	chunkSamples int
	sink         Sink
	ring1        *ring // decode → dsp
	ring2        *ring // dsp → sink
	errOnce      sync.Once
	err          error
}

// New builds a pipeline. src is the MP3 byte stream; chain is the effect
// chain (must include upmix as its head, per config ordering); sink receives
// stereo-interleaved float32 blocks. chunkSamples is the mono block size the
// decoder produces.
func New(src io.Reader, chain []effects.Node, sampleRate, channels, chunkSamples int, sink Sink) (*Pipeline, error) {
	if len(chain) == 0 {
		return nil, errors.New("pipeline: empty chain")
	}
	if chunkSamples <= 0 {
		return nil, fmt.Errorf("pipeline: chunkSamples must be > 0, got %d", chunkSamples)
	}
	dec, err := decode.NewMinimp3Decoder(src)
	if err != nil {
		return nil, fmt.Errorf("pipeline: decoder: %w", err)
	}
	return &Pipeline{
		decoder:      dec,
		chain:        chain,
		sampleRate:   sampleRate,
		channels:     channels,
		chunkSamples: chunkSamples,
		sink:         sink,
		ring1:        newRing(16),
		ring2:        newRing(16),
	}, nil
}

// Run processes the stream until EOF or ctx cancellation. It returns the
// first error encountered (or ctx.Err() on cancellation). All goroutines are
// joined before Run returns, so no goroutine leaks.
func (p *Pipeline) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	wg.Add(3)

	go func() { defer wg.Done(); p.produce(ctx) }()
	go func() { defer wg.Done(); p.process(ctx) }()
	go func() { defer wg.Done(); p.consume(ctx) }()

	wg.Wait()
	// Close each chain node once after all stages finish, fulfilling the
	// Node.Close contract.
	for _, node := range p.chain {
		if err := node.Close(); err != nil {
			p.setErr(fmt.Errorf("pipeline: node close: %w", err))
		}
	}
	return p.err
}

// setErr records the first error.
func (p *Pipeline) setErr(err error) {
	if err == nil {
		return
	}
	p.errOnce.Do(func() { p.err = err })
}

// produce reads MP3, decodes to mono float32 blocks, and pushes them to ring1.
func (p *Pipeline) produce(ctx context.Context) {
	defer p.ring1.close()
	// Close the decoder on exit so minimp3's internal decode goroutine stops
	// (it otherwise loops forever on EOF waiting for ctx cancel).
	defer p.decoder.Close()
	mono := make([]float32, p.chunkSamples)
	for {
		select {
		case <-ctx.Done():
			p.setErr(ctx.Err())
			return
		default:
		}
		n, err := p.decoder.Read(mono)
		if n > 0 {
			block := make([]float32, n)
			copy(block, mono[:n])
			if !p.ring1.push(block) {
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			p.setErr(err)
			return
		}
	}
}

// defaultTailSeconds is how much silence to push through the chain after the
// real data drains, so reverb/delay tails decay naturally instead of being
// hard-cut. Covers the longest preset (sci-fi reverb RT60 2.5-3.5s).
const defaultTailSeconds = 4.0

// process pulls mono blocks, upmixes to stereo, runs the chain, and pushes
// stereo blocks to ring2.
func (p *Pipeline) process(ctx context.Context) {
	// Close ring1 (which process consumes) so a produce goroutine blocked on
	// push unblocks; close ring2 (which process produces) so consume sees
	// end-of-data. Both closes are idempotent.
	defer p.ring1.close()
	defer p.ring2.close()
	for {
		select {
		case <-ctx.Done():
			p.setErr(ctx.Err())
			return
		default:
		}
		mono, ok := p.ring1.pop()
		if !ok {
			// ring1 drained (producer closed on EOF/error): push silence so
			// time-based effects decay naturally instead of a hard cut.
			tailBlocks := int(defaultTailSeconds*float64(p.sampleRate)) / p.chunkSamples
			for range tailBlocks {
				if !p.processBlock(ctx, make([]float32, p.chunkSamples)) {
					return
				}
			}
			return
		}
		if !p.processBlock(ctx, mono) {
			return
		}
	}
}

// processBlock upmixes mono, runs the chain, and pushes the stereo result to
// ring2. It returns false if ctx is done or the ring is closed.
func (p *Pipeline) processBlock(ctx context.Context, mono []float32) bool {
	// Build a stereo buffer with mono in L and R=0; the chain's upmix
	// head node fills R from L (center). If the chain has no upmix, R
	// stays 0 (mono carried in L).
	stereo := make([]float32, len(mono)*2)
	for i, s := range mono {
		stereo[i*2] = s
	}
	for _, node := range p.chain {
		if err := node.ProcessInPlace(stereo); err != nil {
			p.setErr(fmt.Errorf("pipeline: effect: %w", err))
			return false
		}
	}
	if !p.ring2.push(stereo) {
		return false
	}
	select {
	case <-ctx.Done():
		p.setErr(ctx.Err())
		return false
	default:
		return true
	}
}

// consume pulls stereo blocks and writes them to the sink.
func (p *Pipeline) consume(ctx context.Context) {
	// Close ring2 on exit so a process goroutine blocked on push unblocks
	// even when consume returns early (sink error or cancellation). Close
	// is idempotent; process also closes it on its own exit.
	defer p.ring2.close()
	defer func() {
		if err := p.sink.Close(); err != nil {
			p.setErr(fmt.Errorf("pipeline: sink close: %w", err))
		}
	}()
	for {
		select {
		case <-ctx.Done():
			p.setErr(ctx.Err())
			return
		default:
		}
		stereo, ok := p.ring2.pop()
		if !ok {
			return
		}
		if err := p.sink.Write(stereo); err != nil {
			p.setErr(fmt.Errorf("pipeline: sink write: %w", err))
			return
		}
	}
}

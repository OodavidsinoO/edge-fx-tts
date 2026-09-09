package effects

import (
	"fmt"
	"math"

	"github.com/cwbudde/algo-dsp/dsp/window"
	algofft "github.com/cwbudde/algo-fft"
)

func init() {
	Register("formant", newFormant)
}

// formantNode shifts the spectral envelope of the signal via cepstral
// analysis, leaving the fine harmonic structure (and therefore f0) intact.
// It is a mono effect: L and R each keep an independent streaming state so
// their processing never cross-contaminates. shift == 1.0 is an exact
// passthrough.
type formantNode struct {
	engine *formantEngine
	left   *cepstralFormant
	right  *cepstralFormant
}

// formantEngine holds the immutable resources shared by both channels: one
// real FFT plan (used sequentially by L then R), the analysis window, and the
// hop-period OLA normalization table.
type formantEngine struct {
	plan         *algofft.PlanReal[float64, complex128]
	win          []float64 // analysis window, len frameSize
	invNormPhase []float64 // OLA emission divisor per hop phase, len hop
	frameSize    int
	half         int
	hop          int
	lifterK      int
	shift        float64
	bypass       bool
}

// cepstralFormant holds the per-channel streaming state. Every buffer is
// preallocated once at construction so the frame loop never allocates.
type cepstralFormant struct {
	inRing     []float64 // input ring, len frameSize
	inLen      int
	frame      []float64    // windowed frame sent to the FFT
	spectrum   []complex128 // half+1
	mag        []float64    // half+1
	logMag     []float64    // half+1
	phase      []float64    // half+1
	lmSpec     []complex128 // half+1, purely real log-magnitude spectrum
	cepstrum   []float64    // frameSize
	envIn      []float64    // frameSize, liftered cepstrum
	envHalf    []complex128 // half+1, envelope spectrum
	logEnv     []float64    // half+1
	newMag     []float64    // half+1, rescaled envelope magnitudes
	outSpec    []complex128 // half+1, rebuilt spectrum
	outFrame   []float64    // frameSize, synthesized frame
	ola        []float64    // frameSize overlap accumulator
	outPending []float64    // frameSize, produced output awaiting drain
	outLen     int
}

func newFormant(sampleRate int, params map[string]any) (Node, error) {
	shift := getFloat(params, "shift", 1.0)
	if shift <= 0 {
		return nil, fmt.Errorf("effects: formant: shift must be > 0, got %v", shift)
	}
	frameSize := getInt(params, "frameSize", 2048)
	if frameSize < 16 || frameSize&(frameSize-1) != 0 {
		return nil, fmt.Errorf("effects: formant: frameSize must be a power of two >= 16, got %d", frameSize)
	}
	hops := getInt(params, "hops", 4)
	if hops < 1 || hops > frameSize || frameSize%hops != 0 {
		return nil, fmt.Errorf("effects: formant: hops must be in [1, frameSize] and divide frameSize, got %d", hops)
	}
	lifter := getFloat(params, "lifter", 0.2)
	if lifter < 0 || lifter > 1 {
		return nil, fmt.Errorf("effects: formant: lifter must be in [0, 1], got %v", lifter)
	}

	win, err := window.Hann(frameSize)
	if err != nil {
		return nil, fmt.Errorf("effects: formant: %w", err)
	}
	plan, err := algofft.NewPlanReal[float64, complex128](frameSize)
	if err != nil {
		return nil, fmt.Errorf("effects: formant: %w", err)
	}

	hop := frameSize / hops
	e := &formantEngine{
		plan:         plan,
		win:          win,
		invNormPhase: make([]float64, hop),
		frameSize:    frameSize,
		half:         frameSize / 2,
		hop:          hop,
		lifterK:      int(math.Ceil(lifter * float64(frameSize) / 2)),
		shift:        shift,
		bypass:       shift == 1.0,
	}
	// Steady-state OLA normalization: the analysis window is baked into every
	// synthesized frame, so divide each output sample by the sum of window
	// samples that cover its hop phase. Stream head/tail have fewer covering
	// frames, which yields a natural fade in/out.
	for i := range e.invNormPhase {
		s := 0.0
		for idx := i; idx < frameSize; idx += hop {
			s += win[idx]
		}
		if s > 1e-12 {
			e.invNormPhase[i] = 1.0 / s
		}
	}

	newCh := func() *cepstralFormant {
		return &cepstralFormant{
			inRing:     make([]float64, frameSize),
			frame:      make([]float64, frameSize),
			spectrum:   make([]complex128, frameSize/2+1),
			mag:        make([]float64, frameSize/2+1),
			logMag:     make([]float64, frameSize/2+1),
			phase:      make([]float64, frameSize/2+1),
			lmSpec:     make([]complex128, frameSize/2+1),
			cepstrum:   make([]float64, frameSize),
			envIn:      make([]float64, frameSize),
			envHalf:    make([]complex128, frameSize/2+1),
			logEnv:     make([]float64, frameSize/2+1),
			newMag:     make([]float64, frameSize/2+1),
			outSpec:    make([]complex128, frameSize/2+1),
			outFrame:   make([]float64, frameSize),
			ola:        make([]float64, frameSize),
			outPending: make([]float64, frameSize),
		}
	}
	return &formantNode{engine: e, left: newCh(), right: newCh()}, nil
}

func (n *formantNode) ProcessInPlace(buf []float32) error {
	if n.engine.bypass {
		return nil // shift == 1.0 is an exact passthrough
	}
	if err := processChannel(n.engine, n.left, buf, 0); err != nil {
		return err
	}
	return processChannel(n.engine, n.right, buf, 1)
}

func (n *formantNode) Close() error { return nil }

// processChannel pushes the channel's samples from buf (stride 2, offset ch),
// runs the streaming cepstral pipeline, and drains produced output back into
// buf, padding with silence. The hot loop only reuses preallocated buffers.
func processChannel(e *formantEngine, c *cepstralFormant, buf []float32, ch int) error {
	nFrames := len(buf) / 2
	off := 0  // input frontier, positions [0, off) already pushed
	wpos := 0 // output frontier, positions [0, wpos) already written

	for off < nFrames {
		room := len(c.inRing) - c.inLen
		take := nFrames - off
		if take > room {
			take = room
		}
		for j := range take {
			c.inRing[c.inLen+j] = float64(buf[2*(off+j)+ch])
		}
		c.inLen += take
		off += take

		for c.inLen >= e.frameSize && c.outLen+e.hop <= len(c.outPending) {
			if err := processFrame(e, c); err != nil {
				return err
			}
			advanceRing(e, c)
		}

		// Drain into positions already consumed from the input.
		d := c.outLen
		if d > off-wpos {
			d = off - wpos
		}
		for j := range d {
			buf[2*(wpos+j)+ch] = float32(c.outPending[j])
		}
		copy(c.outPending, c.outPending[d:c.outLen])
		c.outLen -= d
		wpos += d
	}

	// Final drain into the remaining positions; pad the tail with silence.
	d := c.outLen
	if d > nFrames-wpos {
		d = nFrames - wpos
	}
	for j := range d {
		buf[2*(wpos+j)+ch] = float32(c.outPending[j])
	}
	copy(c.outPending, c.outPending[d:c.outLen])
	c.outLen -= d
	wpos += d
	for j := wpos; j < nFrames; j++ {
		buf[2*j+ch] = 0
	}
	return nil
}

// advanceRing drops the oldest hop samples, which have been fully consumed by
// the frame just processed.
func advanceRing(e *formantEngine, c *cepstralFormant) {
	hop := e.hop
	for j := range c.inLen - hop {
		c.inRing[j] = c.inRing[j+hop]
	}
	c.inLen -= hop
}

// processFrame runs one cepstral analysis/synthesis cycle for the oldest
// complete frame in the ring (inRing[0:frameSize]) and folds the synthesized
// frame into the OLA accumulator, emitting one hop block to outPending.
func processFrame(e *formantEngine, c *cepstralFormant) error {
	half := e.half

	// Window the frame.
	for j := range c.frame {
		c.frame[j] = c.inRing[j] * e.win[j]
	}
	if err := e.plan.Forward(c.spectrum, c.frame); err != nil {
		return err
	}

	// Analysis: magnitude, log-magnitude, phase. Bins 0 and half carry no
	// imaginary part by construction, so their phase is exactly 0 (ignored
	// during reconstruction anyway).
	for k := 0; k <= half; k++ {
		re := real(c.spectrum[k])
		im := imag(c.spectrum[k])
		m := math.Hypot(re, im)
		if m < 1e-9 {
			m = 1e-9
		}
		c.mag[k] = m
		c.logMag[k] = math.Log(m)
		c.phase[k] = math.Atan2(im, re)
		c.lmSpec[k] = complex(c.logMag[k], 0)
	}

	// Cepstrum: inverse FFT of the (purely real) log-magnitude spectrum,
	// which yields an even-symmetric real sequence with the 1/N built in.
	if err := e.plan.Inverse(c.cepstrum, c.lmSpec); err != nil {
		return err
	}

	// Lifter: keep the low-quefrency (smooth spectral envelope) part, which
	// spans both ends of the even-symmetric cepstrum.
	for j := range c.envIn {
		c.envIn[j] = c.cepstrum[j]
	}
	for j := e.lifterK; j < e.frameSize-e.lifterK; j++ {
		c.envIn[j] = 0
	}

	// Log-envelope: FFT back of the liftered cepstrum. The kept cepstrum is
	// even-symmetric, so the envelope spectrum is purely real.
	if err := e.plan.Forward(c.envHalf, c.envIn); err != nil {
		return err
	}
	for j := 0; j <= half; j++ {
		c.logEnv[j] = real(c.envHalf[j])
	}

	// Frequency-axis resampling by 1/shift: shift > 1 moves envelope features
	// up, shift < 1 moves them down. The magnitude is modulated by the
	// envelope ratio in the log domain, so loudness is preserved and shift
	// == 1 leaves magnitudes untouched exactly.
	sh := e.shift
	for j := 0; j <= half; j++ {
		src := float64(j) / sh
		if src > float64(half) {
			src = float64(half)
		}
		i0 := int(src)
		if i0 > half {
			i0 = half
		}
		i1 := i0 + 1
		if i1 > half {
			i1 = half
		}
		frac := src - float64(i0)
		envAt := c.logEnv[i0]*(1-frac) + c.logEnv[i1]*frac
		c.newMag[j] = c.mag[j] * math.Exp(envAt-c.logEnv[j])
	}

	// Rebuild the spectrum: original phase, envelope-modulated magnitude.
	// Bins 0 and half must be purely real for the inverse transform.
	for j := 1; j < half; j++ {
		c.outSpec[j] = complex(c.newMag[j]*math.Cos(c.phase[j]), c.newMag[j]*math.Sin(c.phase[j]))
	}
	c.outSpec[0] = complex(c.newMag[0], 0)
	c.outSpec[half] = complex(c.newMag[half], 0)
	if err := e.plan.Inverse(c.outFrame, c.outSpec); err != nil {
		return err
	}

	// Overlap-add and emit one hop block.
	for j := range c.ola {
		c.ola[j] += c.outFrame[j]
	}
	for j := range e.hop {
		c.outPending[c.outLen+j] = c.ola[j] * e.invNormPhase[j]
	}
	c.outLen += e.hop
	copy(c.ola, c.ola[e.hop:])
	for j := e.frameSize - e.hop; j < e.frameSize; j++ {
		c.ola[j] = 0
	}
	return nil
}

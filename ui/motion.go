package ui

import (
	"math"
	"slices"
	"time"
)

// SpringOptions describes a damped spring. Zero fields use Mass 1,
// Stiffness 170, Damping 26, and Precision 0.001. Precision is the distance
// and velocity below which the spring rests at its target.
type SpringOptions struct {
	Mass, Stiffness, Damping, Precision float64
}

func (o SpringOptions) defaults() SpringOptions {
	if o.Mass == 0 {
		o.Mass = 1
	}
	if o.Stiffness == 0 {
		o.Stiffness = 170
	}
	if o.Damping == 0 {
		o.Damping = 26
	}
	if o.Precision == 0 {
		o.Precision = 0.001
	}
	for _, v := range []float64{o.Mass, o.Stiffness, o.Damping, o.Precision} {
		if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			panic("ui: spring options must be finite and positive")
		}
	}
	if w, a := o.Stiffness/o.Mass, o.Damping/o.Mass; math.IsInf(w, 0) || math.IsInf(a, 0) || w == 0 || a == 0 {
		panic("ui: spring options exceed numerical range")
	}
	return o
}

// Spring follows target with a spring, preserving both position and
// velocity when target or options change, even between animation frames.
// It starts at its first target. Call it in the view on each build, with a
// stable element and key. It requests frames only while moving and snaps
// to target while Preferences.ReduceMotion is set.
func (e *Element) Spring(key any, target float32, options ...SpringOptions) float32 {
	finiteTransform(target)
	if len(options) > 1 {
		panic("ui: Spring takes at most one SpringOptions")
	}
	o := SpringOptions{}
	if len(options) > 0 {
		o = options[0]
	}
	o = o.defaults()
	st := e.st
	if st.springs == nil {
		st.springs = map[any]*spring{}
	}
	s := st.springs[key]
	now := e.c.now
	if s == nil {
		s = &spring{value: float64(target), target: float64(target), at: now, options: o}
		st.springs[key] = s
	}
	s.advance(now)
	s.target, s.options = float64(target), o
	if e.c.rt.preferences().ReduceMotion || (math.Abs(s.value-s.target) <= o.Precision && math.Abs(s.velocity) <= o.Precision) {
		s.value, s.velocity = s.target, 0
	} else {
		e.c.AnimationFrame()
	}
	return float32(s.value)
}

// SpringVelocity returns the velocity, in value units per second, of
// the spring sampled by Spring in this build, or zero before it starts.
func (e *Element) SpringVelocity(key any) float32 {
	if s := e.st.springs[key]; s != nil {
		return float32(s.velocity)
	}
	return 0
}

type spring struct {
	value, velocity, target float64
	at                      time.Time
	options                 SpringOptions
}

// advance solves the oscillator analytically, independent of frame rate.
func (s *spring) advance(now time.Time) {
	dt := now.Sub(s.at).Seconds()
	if dt <= 0 {
		s.at = now
		return
	}
	s.at = now
	x, v := s.value-s.target, s.velocity
	if x == 0 && v == 0 {
		return
	}
	o := s.options
	w := math.Sqrt(o.Stiffness / o.Mass)
	a := (o.Damping / o.Mass) / 2
	switch d := a*a - w*w; {
	case math.Abs(d) < 1e-8*w*w:
		b := v + a*x
		decay := math.Exp(-a * dt)
		s.value = s.target + (x+b*dt)*decay
		s.velocity = (v - a*b*dt) * decay
	case d < 0:
		f := math.Sqrt(-d)
		sn, cs := math.Sincos(f * dt)
		b := (v + a*x) / f
		decay := math.Exp(-a * dt)
		y := x*cs + b*sn
		s.value = s.target + y*decay
		s.velocity = (f*(-x*sn+b*cs) - a*y) * decay
	default:
		f := math.Sqrt(d)
		r1 := -w * w / (a + f)
		r2 := -a - f
		c1 := (v - r2*x) / (r1 - r2)
		c2 := x - c1
		u, z := c1*math.Exp(r1*dt), c2*math.Exp(r2*dt)
		s.value, s.velocity = s.target+u+z, r1*u+r2*z
	}
}

// Keyframe is a value at At since a sequence starts. The first At must be
// zero, subsequent times strictly increase. Ease controls the segment
// arriving at this frame; nil means Linear.
type Keyframe struct {
	At    time.Duration
	Value float32
	Ease  Easing
}

// KeyframeOptions controls playback. Increment Run to replay or interrupt
// the sequence on the same key: its first value becomes the value currently
// shown, so interruption is continuous. Repeat is the number of iterations
// (zero means one, -1 means forever); Alternate reverses every other iteration.
type KeyframeOptions struct {
	Run       uint64
	Repeat    int
	Alternate bool
}

// Keyframes plays a sequence once, holding its last value when done. It
// starts on the first call, with the first frame's value. Keep the element
// and key stable, and change Run when changing the sequence. Only active
// playback asks for animation frames. Reduced motion completes playback at
// its final value, including when the preference changes mid-sequence.
func (e *Element) Keyframes(key any, frames []Keyframe, options ...KeyframeOptions) float32 {
	if len(frames) < 2 || frames[0].At != 0 {
		panic("ui: Keyframes needs at least two frames starting at zero")
	}
	for i, f := range frames {
		finiteTransform(f.Value)
		if i > 0 && f.At <= frames[i-1].At {
			panic("ui: keyframe times must strictly increase")
		}
	}
	if len(options) > 1 {
		panic("ui: Keyframes takes at most one KeyframeOptions")
	}
	o := KeyframeOptions{}
	if len(options) > 0 {
		o = options[0]
	}
	if o.Repeat < -1 {
		panic("ui: keyframe Repeat must be -1 or nonnegative")
	}
	if e.st.sequences == nil {
		e.st.sequences = map[any]*sequence{}
	}
	s := e.st.sequences[key]
	now := e.c.now
	if s == nil || s.options.Run != o.Run {
		from := frames[0].Value
		if s != nil {
			from, _ = s.sample(now)
		}
		s = &sequence{frames: slices.Clone(frames), options: o, start: now, value: from}
		s.frames[0].Value = from
		e.st.sequences[key] = s
	}
	if e.c.rt.preferences().ReduceMotion {
		s.value, s.done = s.final(), true
	} else if !s.done {
		s.value, s.done = s.sample(now)
		if !s.done {
			e.c.AnimationFrame()
		}
	}
	return s.value
}

type sequence struct {
	frames  []Keyframe
	options KeyframeOptions
	start   time.Time
	value   float32
	done    bool
}

func (s *sequence) final() float32 {
	if s.options.Alternate && max(s.options.Repeat, 1)%2 == 0 {
		return s.frames[0].Value
	}
	return s.frames[len(s.frames)-1].Value
}

func (s *sequence) sample(now time.Time) (float32, bool) {
	if s.done {
		return s.value, true
	}
	d := s.frames[len(s.frames)-1].At
	elapsed := max(now.Sub(s.start), 0)
	iteration := int64(elapsed / d)
	if s.options.Repeat != -1 && iteration >= int64(max(s.options.Repeat, 1)) {
		return s.final(), true
	}
	at := elapsed % d
	if s.options.Alternate && iteration%2 != 0 {
		at = d - at
	}
	for i := 1; i < len(s.frames); i++ {
		b := s.frames[i]
		if at > b.At {
			continue
		}
		a := s.frames[i-1]
		t := float32(at-a.At) / float32(b.At-a.At)
		ease := b.Ease
		if ease == nil {
			ease = Linear
		}
		return lerp(a.Value, b.Value, ease(t)), false
	}
	return s.final(), false
}

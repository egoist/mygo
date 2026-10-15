package main

import "math"

// How the hand stirs the air: a click is a blow, whose front runs out
// from the pointer, and a moving pointer warms the air along its path.
const (
	// tapLife and wakeLife are how long a blow and a stretch of the path
	// last, in seconds.
	tapLife, wakeLife = 3.6, 2.4
	// maxTaps and maxWakes are how many of each the air keeps.
	maxTaps, maxWakes = 6, 48
	// stir and ripple are how hard the pointer's moves and clicks stir the
	// air, and slowSpeed and fastSpeed the speeds of the pointer, in DIPs
	// a second, between which a move goes from stirring nothing to
	// stirring it all.
	stir, ripple         = 1, 1
	slowSpeed, fastSpeed = 60, 800
	// A stretch of the path is laid once the pointer went minStep DIPs and
	// minGap seconds from the last; a pause of restAfter seconds starts the
	// path anew; the pointer's speed follows its moves over speedLag
	// seconds.
	minStep, minGap, restAfter, speedLag = 18, 0.035, 0.25, 0.08
	// heartEvery is how many clicks make a heart, a blow that runs out as
	// a heart rather than a circle.
	heartEvery = 3
)

// blow is a click, or a stretch of the pointer's path: where, when and
// how hard; and for a stretch, which way it runs, with half its length.
type blow struct {
	x, y, at, force float32
	wayX, wayY      float32
}

// warmth is a stretch of the path as the air feels it now: its middle,
// how warm, one over the square of how far that reaches, and which way
// it runs (a unit vector) with half its length.
type warmth struct {
	x, y, heat, reach float32
	dirX, dirY, half  float32
}

// hand is what the pointer did, in DIPs of the pane and seconds of the
// texture's clock.
type hand struct {
	taps, wakes []blow
	clicks      int
	// The pointer as it last moved, and as it was where the last stretch
	// ended; speed is its speed, smoothed.
	last, from *blow
	speed      float32
	// As of the frame: its time, and the warmth of the path.
	now  float32
	warm []warmth
}

// move notes that the pointer moved to (x, y) at t, and reports whether
// it laid a stretch of its path.
func (h *hand) move(x, y, t float32) bool {
	if h.last == nil || h.from == nil || t-h.last.at > restAfter {
		h.last = &blow{x: x, y: y, at: t}
		h.from, h.speed = h.last, 0
		return false
	}
	dt := max(t-h.last.at, 1.0/240)
	h.speed += (hypot(x-h.last.x, y-h.last.y)/dt - h.speed) * min(1, dt/speedLag)
	h.last = &blow{x: x, y: y, at: t}
	l := hypot(x-h.from.x, y-h.from.y)
	if l < minStep || t-h.from.at < minGap {
		return false
	}
	force := stir * smoothstep(0, 1, (h.speed-slowSpeed)/(fastSpeed-slowSpeed))
	from := h.from
	h.from = h.last
	if force < 0.04 {
		return false
	}
	h.wakes = append(keepLast(h.wakes, maxWakes-1), blow{
		x: (x + from.x) / 2, y: (y + from.y) / 2, at: t,
		force: force * l / (l + 90),
		wayX:  (x - from.x) / 2, wayY: (y - from.y) / 2,
	})
	return true
}

// tap notes a click at (x, y) at t: every heartEvery-th is a heart.
func (h *hand) tap(x, y, t float32) {
	h.clicks++
	force := float32(ripple)
	if h.clicks%heartEvery == 0 {
		force = -1.15 * ripple
	}
	h.taps = append(keepLast(h.taps, maxTaps-1), blow{x: x, y: y, at: t, force: force})
}

// leave notes that the pointer left: its next move starts a path anew.
func (h *hand) leave() { h.last = nil }

// busy reports whether a blow or a stretch of the path still stirs the
// air at t; rippling whether a blow does.
func (h *hand) busy(t float32) bool {
	return h.rippling(t) || anyAfter(h.wakes, t-wakeLife)
}

func (h *hand) rippling(t float32) bool { return anyAfter(h.taps, t-tapLife) }

// active reports whether the hand stirs the air in the frame.
func (h *hand) active() bool { return h != nil && len(h.taps)+len(h.warm) > 0 }

// frame readies the hand for a frame at t: blows and stretches that died
// down go, and each stretch's warmth is worked out, rising quickly and
// falling slowly, reaching further as it ages, and weaker the older the
// path behind it.
func (h *hand) frame(t float32) {
	h.now = t
	h.taps = keepAfter(h.taps, t-tapLife)
	h.wakes = keepAfter(h.wakes, t-wakeLife)
	h.warm = h.warm[:0]
	older := maxWakes - len(h.wakes)
	for i, w := range h.wakes {
		age := max(0, t-w.at)
		heat := w.force * min(1, float32(i+1+older)/8) * (1 - exp(-age/0.05)) * exp(-age/0.65)
		if heat < 0.002 {
			continue
		}
		reach := 50 + 40*age
		l := hypot(w.wayX, w.wayY)
		d := l
		if d == 0 {
			d = 1
		}
		h.warm = append(h.warm, warmth{x: w.x, y: w.y, heat: heat, reach: 1 / (reach * reach), dirX: w.wayX / d, dirY: w.wayY / d, half: l})
	}
}

// push returns, at (x, y), the air's push from the blows, in DIPs (0 and
// 1), the wind they raise or lower (2), and how roused the air is (3);
// and the color of the ripples there (0), with their afterlight (1).
//
// A blow's front runs out and slows, with a trough and a weaker crest
// behind it, shoving the air outward.
func (h *hand) push(x, y float32) (sum [4]float32, glow [2]float32) {
	for _, tp := range h.taps {
		age := h.now - tp.at
		ax, ay := x-tp.x, y-tp.y
		d := hypot(ax, ay)
		reach := d
		if tp.force < 0 {
			reach = d / heart(ax/max(d, 1), ay/max(d, 1))
		}
		behind := 620*(1-exp(-age/1.3)) - reach
		width := 85 + 60*age
		front := smoothstep(-16, 10, behind) * exp(-behind*behind/(width*width))
		force := abs(tp.force)
		swell := force * exp(-age/1.5) * front
		sum[0] += ax / max(d, 1) * 44 * swell
		sum[1] += ay / max(d, 1) * 44 * swell
		sum[2] += 1.9 * swell * (0.25 + 0.75*cos(behind*(tau/130)))
		sum[3] += swell
		trail := smoothstep(0, 70, behind) * exp(-max(behind, 0)/260)
		glow[0] += force * (exp(-age)*front + 0.7*exp(-age/1.3)*trail)
		glow[1] += force * (1 - exp(-age/0.2)) * exp(-age/1.2) * trail
	}
	return sum, glow
}

// warmth returns how warm the path left the air at (x, y), from 0 to 1:
// however hard it is stirred, it is only ever warmed so far.
func (h *hand) warmth(x, y float32) float32 {
	var w float32
	for _, s := range h.warm {
		ax, ay := x-s.x, y-s.y
		along := min(max(ax*s.dirX+ay*s.dirY, -s.half), s.half)
		ax, ay = ax-s.dirX*along, ay-s.dirY*along
		w += s.heat * exp(-(ax*ax+ay*ay)*s.reach)
	}
	return 1 - exp(-1.5*w)
}

// heart returns how far out a heart's outline lies in direction (x, y), a
// unit vector with y down, against a circle's 1.
func heart(x, y float32) float32 {
	k := float64(-x * x * y * y * y)
	c := math.Copysign(math.Pow(math.Abs(k), 1.0/3), k)
	r := 1.0
	for range 6 {
		r = math.Sqrt(math.Max(0.05, 1+c*math.Pow(r, 5.0/3)))
	}
	return float32(r)
}

func keepLast(b []blow, n int) []blow {
	if len(b) > n {
		b = append(b[:0], b[len(b)-n:]...)
	}
	return b
}

func keepAfter(b []blow, t float32) []blow {
	out := b[:0]
	for _, x := range b {
		if x.at > t {
			out = append(out, x)
		}
	}
	return out
}

func anyAfter(b []blow, t float32) bool {
	for _, x := range b {
		if x.at > t {
			return true
		}
	}
	return false
}

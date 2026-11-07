package terminal

import (
	"math"
	"time"

	"github.com/egoist/mygo/plugins/terminal/internal/vt"
	"github.com/egoist/mygo/ui"
)

// smoothScroll is how the view scrolls the scrollback: by pixels, on the
// frame clock, rather than by whole rows as each event comes.
//
//   - A touchpad's moves are resampled at the frame's time, a little behind
//     the fingers (resampleLag), so that every frame moves as far as the
//     fingers did meanwhile: events and frames that come at nearly the same
//     rate otherwise give frames that move twice and frames that do not
//     move at all, which reads as judder.
//   - A wheel's notch glides over notchGlide, easing out, as browsers and
//     ui's scroll containers scroll notches; at once while the desktop asks
//     for less motion.
//
// The view adds no momentum of its own, as ui's scroll containers add
// none: where the system sends momentum, as macOS does, it comes as more
// moves, which are resampled as any others.
//
// Whole rows move the emulator's viewport; the rest is pull, how far the
// content is pulled down past the viewport's top row, which shows part of
// the row above it. Distances are in DIPs, positive down the content
// (toward the live screen), as InputEvent.DY.
type smoothScroll struct {
	// target is where the input puts the content and shown where the last
	// frame put it, from where the motion began.
	target, shown float64
	// samples are the touchpad's last moves: when, and target after each.
	samples [8]scrollSample
	n       int
	// A notch's glide: from where to where, since when.
	glideFrom, glideTo float64
	glideAt            time.Time
	gliding            bool
	// easedAt is when the content last moved, for its ease to the last
	// move.
	easedAt time.Time
}

type scrollSample struct {
	at time.Time
	to float64
}

const (
	// resampleLag is how far behind the fingers the content follows: about
	// one period of a touchpad's reports, 125 Hz on most.
	resampleLag = 9 * time.Millisecond
	// notchGlide is how long a wheel's notch glides.
	notchGlide = 150 * time.Millisecond
	// fitWindow is how far back the moves resample goes through reach,
	// and easeTime how fast the content catches up once they stop.
	fitWindow = 50 * time.Millisecond
	easeTime  = 12 * time.Millisecond
)

// moving reports whether the content has somewhere to go.
func (s *smoothScroll) moving() bool {
	return s.shown != s.target || s.gliding
}

// stop ends the motion where the content shows.
func (s *smoothScroll) stop() { *s = smoothScroll{} }

// precise takes a touchpad's move of dy at now.
func (s *smoothScroll) precise(now time.Time, dy float64) {
	s.gliding = false
	s.target += dy
	if s.n == len(s.samples) {
		copy(s.samples[:], s.samples[1:])
		s.n--
	}
	s.samples[s.n] = scrollSample{now, s.target}
	s.n++
}

// notch takes a wheel's notch of dy at now, which glides unless still.
func (s *smoothScroll) notch(now time.Time, dy float64, still bool) {
	s.n = 0
	from := s.at(now)
	s.target += dy
	if still {
		s.gliding = false
		return
	}
	s.glideFrom, s.glideTo, s.glideAt, s.gliding = from, s.target, now, true
	s.shown = from
}

// at returns where the content goes at now: along a glide, or resampled
// from the touchpad's moves.
func (s *smoothScroll) at(now time.Time) float64 {
	switch {
	case s.gliding:
		k := float64(now.Sub(s.glideAt)) / float64(notchGlide)
		if k >= 1 {
			s.gliding = false
			return s.target
		}
		return s.glideFrom + (s.target-s.glideFrom)*(1-math.Pow(1-k, 3))
	case s.n > 0:
		return s.resample(now)
	}
	return s.target
}

// resample returns where the touchpad's moves put the content at now,
// resampleLag behind them: on the line through the moves of the last
// fitWindow, which events that come in bursts, as the main thread takes
// them, do not throw off as they do the moves themselves. The content
// never goes past the last move, nor back, and once the moves stop it
// eases the rest of the way.
func (s *smoothScroll) resample(now time.Time) float64 {
	last := s.samples[s.n-1]
	when := now.Add(-resampleLag)
	if when.Sub(last.at) > 2*resampleLag || s.n < 2 {
		// The fingers stopped, or there is one move: ease to it.
		return s.shown + (s.target-s.shown)*(1-math.Exp(-float64(now.Sub(s.easedAt))/float64(easeTime)))
	}
	var n, st, sx, stt, stx float64
	for i := s.n - 1; i >= 0 && last.at.Sub(s.samples[i].at) <= fitWindow; i-- {
		t := s.samples[i].at.Sub(last.at).Seconds()
		x := s.samples[i].to
		n, st, sx, stt, stx = n+1, st+t, sx+x, stt+t*t, stx+t*x
	}
	q := when.Sub(last.at).Seconds()
	to := sx / n
	if d := n*stt - st*st; n >= 2 && d > 1e-12 {
		b := (n*stx - st*sx) / d
		to = (sx-b*st)/n + b*q
	}
	// Neither back, nor past the last move.
	lo, hi := min(s.shown, s.target), max(s.shown, s.target)
	return max(lo, min(to, hi))
}

// advance returns how far the content moves in the frame at now.
func (s *smoothScroll) advance(now time.Time) float64 {
	if !s.moving() {
		return 0
	}
	to := s.at(now)
	d := to - s.shown
	s.shown = to
	if math.Abs(s.target-s.shown) < 0.05 && !s.gliding {
		s.shown = s.target // an ease's tail, under a pixel
	}
	s.easedAt = now
	return d
}

// scrollPixels scrolls the scrollback by d DIPs, positive toward the live
// screen: whole rows move the viewport, the rest stays as v.pull. It
// reports whether the scrollback went as far, false at an end. t.mu is
// held.
func (v *view) scrollPixels(term *vt.Terminal, d float32) bool {
	ch := float32(v.cellH) / v.scale
	if ch <= 0 || term.AltScreen() {
		v.pull = 0
		return false
	}
	// back is how far the content is pulled down past the viewport's top
	// row once moved: whole rows of it move the viewport up, negative
	// ones down, and the rest is pulled.
	back := v.pull - d
	rows := int(math.Floor(float64(back / ch)))
	rest := back - float32(rows)*ch
	// What rounding leaves of a whole row is the row.
	switch {
	case rest > ch-0.01:
		rows, rest = rows+1, 0
	case rest < 0.01:
		rest = 0
	}
	before := term.Scrollbar().Offset
	if rows != 0 {
		term.ScrollBy(-rows)
	}
	after := term.Scrollbar().Offset
	if int(int64(before)-int64(after)) != rows {
		// At the top or the bottom.
		v.pull = 0
		return false
	}
	if after == 0 && rest > 0 {
		// Nothing above the oldest row to pull down.
		v.pull = 0
		return d > 0
	}
	v.pull, v.pullBottom = rest, term.AtBottom()
	return true
}

// stepScroll moves the scrollback to where it goes in the frame at now,
// and reports whether it goes on moving. t.mu is held.
func (v *view) stepScroll(term *vt.Terminal, now time.Time) bool {
	if !v.smooth.moving() {
		// At rest the pull stays where the input left it, also at the
		// bottom as output comes or the cursor blinks. It goes with the
		// alternate screen, which has no scrollback, and when something
		// else (a paste, a Mirror) put the viewport at the bottom from
		// above it, or left nothing above it to show.
		if v.pull != 0 && (term.AltScreen() || term.AtBottom() && !v.pullBottom || term.Scrollbar().Offset == 0) {
			v.pull = 0
		}
		return false
	}
	d := v.smooth.advance(now)
	if d != 0 && !v.scrollPixels(term, float32(d)) {
		// At an end: the rest of a glide goes.
		v.smooth.stop()
		return false
	}
	return v.smooth.moving()
}

// settleScroll ends the scrollback's motion, at the row it shows, as a
// press or a key takes over. t.mu is held.
func (v *view) settleScroll() {
	v.smooth.stop()
	v.pull = 0
}

// smoothInput takes a scroll event that scrolls the scrollback: it moves
// on the frame clock from here. t.mu is held.
func (v *view) smoothInput(c ui.Services, ev ui.InputEvent, dy float32, still bool) {
	if ev.Precise {
		v.smooth.precise(ev.Time, float64(dy))
	} else {
		v.smooth.notch(ev.Time, float64(dy), still)
	}
	c.Invalidate()
}

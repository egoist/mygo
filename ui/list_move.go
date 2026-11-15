package ui

// moveMark is a row as the last layout of a list with a MoveAnimation
// placed it: its item's key, its index, where its top was placed, and how
// far from there it showed, on its way to its place (lag).
type moveMark struct {
	key any
	i   int
	y   float64
	lag channel
}

// moveRows works out where the rows lo to hi of a list with a
// MoveAnimation show, row lo placed at yLo. In a frame in which rows
// came, went or moved, a row the layout places away from where the last
// frame showed it, the list's scrolling aside, shows there, and glides
// to its place: those that a row going closes the gap of, or rows coming
// open one for, whichever way the list keeps its place. A row the last
// frame did not place moves with the nearest one it did, the one above
// it unless it comes above all of them. A list gliding to its end with a
// FollowAnimation glides with that instead. It returns the rows to place,
// with where the first and the last are placed, and leaves how far from
// its place each shows in s.lags, from the first.
func (s *ListState) moveRows(e *node, n, lo, hi int, yLo, yHi, top, bottom float64, ht func(int) float64, ensure func(int) bool) (int, int, float64, float64) {
	rt, now := e.c.rt, e.c.now
	s.lags = s.lags[:0]
	if s.MoveAnimation == nil || s.Key == nil || rt.preferences().ReduceMotion || rt.instant() || e.st.born == rt.frame {
		s.moved, s.movedWas, s.moveFrame = s.moved[:0], s.movedWas[:0], 0
		return lo, hi, yLo, yHi
	}
	if rt.frame != s.moveFrame {
		// Each layout of a frame starts from the last frame's, so that
		// the last, which shows, has the list as it is then.
		s.moveFrame = rt.frame
		s.moved, s.movedWas = s.movedWas[:0], s.moved
	} else {
		s.moved = s.moved[:0]
	}
	was, gap := s.movedWas, s.gap
	// Rows mostly keep their order: each search starts after the row
	// found last.
	at := 0
	find := func(k any) (*moveMark, bool) {
		for d := range was {
			if w := &was[(at+d)%len(was)]; w.key == k {
				at = (at + d + 1) % len(was)
				return w, true
			}
		}
		return nil, false
	}
	known := s.moveKnown[:0]
	changed := n != s.moveN
	y := yLo
	for i := lo; i <= hi; i++ {
		var ch channel
		k := s.Key(i)
		w, ok := find(k)
		if ok {
			ch = w.lag
			ch.advance(now)
			changed = changed || w.i != i
		}
		s.moved = append(s.moved, moveMark{key: k, i: i, y: y, lag: ch})
		known = append(known, ok)
		y += ht(i) + gap
	}
	if changed && !s.moveJump && (s.FollowAnimation == nil || !s.follows()) {
		at = 0
		for k := range s.moved {
			m := &s.moved[k]
			if !known[k] {
				continue
			}
			w, _ := find(m.key)
			// Where it would show had the list only scrolled.
			if d := w.y - s.moveScroll - m.y; d > 0.5 || d < -0.5 {
				m.lag.shift(d)
				m.lag.retarget(0, *s.MoveAnimation)
			}
		}
	}
	s.moveN = n
	s.moveKnown = known
	// Rows new to the list's place move with their neighbors.
	firstKnown := -1
	for k, ok := range known {
		if ok {
			firstKnown = k
			break
		}
	}
	if firstKnown >= 0 {
		for k := range s.moved {
			switch {
			case k < firstKnown:
				s.moved[k].lag = s.moved[firstKnown].lag
			case !known[k]:
				s.moved[k].lag = s.moved[k-1].lag
			}
		}
	}
	for _, m := range s.moved {
		if m.lag.moving || m.lag.pending {
			rt.animating = true
		}
		s.lags = append(s.lags, m.lag.value)
	}
	return lo, hi, yLo, yHi
}

// lagOf returns how far from its place row i shows, of the rows from lo
// that moveRows placed.
func (s *ListState) lagOf(i, lo int) float64 {
	if k := i - lo; k >= 0 && k < len(s.lags) {
		return s.lags[k]
	}
	return 0
}

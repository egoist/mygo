package ui

import (
	"math"
	"slices"
)

// A MasonryView places its items in order, each at the top of the column
// whose items end highest, as far as the heights the app gives them say.
// An item's place follows from the heights of the items before it alone:
// the places are kept, and made anew from the first item whose height
// changed, or for all of them when the count, the columns or the gap
// change. Its List is of bands, all as high, each a stretch of its
// content: the List builds the bands in view, as it does rows, and keeps
// its place among them; a band builds the items that start in it, and
// those reaching into it from above whose own band is not built, so that
// every item in view is built, once. The items are keyed under the view
// rather than under their band, and keep their state as they go from one
// band to another.

// masonry is where a MasonryView placed its items, kept in its GridState.
type masonry struct {
	s *GridState
	// The items were placed for n of them, width wide (the width of the
	// view's rows), in cols columns colW wide and gapX apart, gapY above
	// one another, of which placedGap is the view's Gap. placed tells
	// that they were.
	n           int
	width, colW float32
	gapX, gapY  float32
	placedGap   float32
	cols        int
	placed      bool
	// height is the app's, as this frame gave it.
	height func(i int, width float32) float32
	// top and h are each item's place, from the top of the first band,
	// and height; col its column and pos which of the column's items it
	// is. lanes are the items of each column, in order, and ends where
	// the next item of each would go.
	top      []float64
	h        []float32
	col, pos []int32
	lanes    [][]int32
	ends     []float64
	// The content is cut into bands, band high, the rows of the List.
	band  float32
	bands int
	// layoutGap is the Gap the last layout of the view had, which the next
	// frame places the items with.
	layoutGap float32
	// stale is the first item a band built found the height of changed,
	// -1 for none: the next frame places it and those after anew.
	stale int
	// anchor is the first item in view as the last frame built the view,
	// and anchorKey its key: it stays where it shows as the items are
	// placed anew.
	anchor    int
	anchorKey any
	// pending is where the app asked the view to scroll before it placed
	// the items of this frame.
	pending struct {
		set, near bool
		item      int
		align     Align
	}
	// rt, st and viewH are the view's engine, its element's state and the
	// window's height, as the view was last built, in frame and pass.
	rt    *engine
	st    *state
	viewH float32
	frame uint64
	pass  int
	// done holds a bit for each item built in this pass, and pick the
	// items of the band being built.
	done []uint64
	pick []int32
}

// MasonryView creates a scroll container of n items in columns, as a wall
// of stickers or of photos of any shape, built with item(i): as many
// columns as fit items at least minWidth wide, which share the width left,
// each item at the top of the column whose items end highest, as high as
// height(i, width) says for the columns' width, as a picture's aspect
// ratio does. Only the items in view are built, so walls of thousands are
// as fast as small ones, and an item's place, which follows from the
// heights of those before it, stays as the view scrolls. Gap spaces the
// items in a column further.
//
// s keeps the place and says how items are chosen, as for a GridView, or
// nil: Up and Down move the choice within a column, and Left and Right to
// the nearest item in the next column. Heights are asked again of the
// items in view as they show: one changed places it and those after it
// anew.
//
//	app.stickers.Selected = &app.sticker
//	ui.MasonryView(c, &app.stickers, len(stickers), 140, func(i int, width float32) float32 {
//		return width / stickers[i].Aspect
//	}, func(i int) {
//		ui.Image(c, thumbs[i]).Fit(ui.Cover).Grow(1)
//	}).Grow(1)
func coreMasonryView(c *context, s *GridState, n int, minWidth float32, height func(i int, width float32) float32, item func(i int)) *node {
	t, rt := c.theme, c.rt
	e := coreScroll(c)
	e.widget, e.role = "MasonryView", RoleList
	if s == nil {
		s = coreLocal(e, "masonry", func() GridState { return GridState{} })
	}
	wrapElement(e).Bind(&s.Handle)
	n = max(n, 0)
	m := s.masonry
	if m == nil {
		m = &masonry{stale: -1, anchor: -1}
		s.masonry = m
	}
	if s.scroll == nil {
		s.scroll = m.scrollTo
	}
	m.s, m.rt, m.st, m.viewH, m.frame, m.pass = s, rt, e.st, c.h, rt.frame, rt.pass
	// The columns: those that fit the width the rows had in the last
	// frame, or the window's, first, as a GridView's. The layout builds the
	// next frame again where the width changed.
	gap := t.Space(2)
	width := s.rows.width
	if width <= 0 {
		width = c.w
	}
	cols := max(1, int(math.Floor(float64((width+gap)/(minWidth+gap)))))
	colW := max(0, (width-float32(cols-1)*gap)/float32(cols))
	m.update(n, width, cols, colW, gap, max(float64(colW+gap+m.layoutGap), float64(t.Space(8))), height)
	m.resolve()
	m.remember()
	s.cols, s.started = cols, true
	s.begin(e, n)
	words := (n + 63) / 64
	m.done = slices.Grow(m.done[:0], words)[:words]
	clear(m.done)
	under := mix(e.id, 0x6d61736f6e7279) // the items' keys, apart from the bands'
	var cursor *node
	buildList(c, e, e, &s.rows, m.bands, func(r int) {
		if cell := m.build(c, e, r, item, under); cell != nil {
			cursor = cell
		}
	}, gridList)
	e.gridFit = &gridFit{cols: cols, minW: minWidth, gap: gap, m: m}
	e.afterInput(func() { s.keys(e, n, m.step) })
	s.finish(e, n, gap, cursor)
	return e
}

// update places the items anew where what they were placed for changed:
// n items in cols columns colW wide, gapX apart, in bands about band high.
// The item at the top of the view stays where it shows; one there before a
// resize shows at least whole at the top.
func (m *masonry) update(n int, width float32, cols int, colW, gapX float32, band float64, height func(i int, width float32) float32) {
	m.height = height
	gapY := gapX + m.layoutGap
	from, resized := m.stale, false
	if !m.placed || n != m.n || cols != m.cols || colW != m.colW || gapX != m.gapX || gapY != m.gapY {
		from, resized = 0, m.placed && (cols != m.cols || colW != m.colW)
	} else if u0, u1, ok := m.view(); ok {
		// The heights of the items about to show, which the app may have
		// changed: the first changed in each column, which is its first
		// item past the others.
		reach := 3 * float64(m.band)
		for _, lane := range m.lanes {
			for j := m.reach(lane, u0-reach); j < len(lane); j++ {
				i := int(lane[j])
				if m.top[i] >= u1+reach {
					break
				}
				if heightOf(height, i, colW) != m.h[i] {
					if from < 0 || i < from {
						from = i
					}
					break
				}
			}
		}
	}
	m.stale = -1
	if from < 0 {
		return
	}
	s, l := m.s, &m.s.rows
	a, screen := -1, 0.0
	if u0, _, ok := m.view(); ok && m.anchor >= 0 && m.anchor < m.n {
		a, screen = m.anchor, m.top[m.anchor]-u0
	}
	m.place(n, from, width, cols, colW, gapX, gapY, band)
	if a < 0 || n == 0 {
		return
	}
	// With a Key, the item at the top is found where it went, as a List
	// finds its rows.
	to := min(a, n-1)
	if s.Key != nil && m.anchorKey != nil {
		if j := m.find(m.anchorKey, a, n); j >= 0 {
			to = j
		}
	}
	if resized {
		screen = max(screen, 0)
	}
	l.req = listRequest{set: true, offset: true, y: max(0, m.top[to]-screen+l.padTop-l.border)}
}

// place places items from on, of n, each at the top of the column ending
// highest, the leftmost of those, and cuts the content into bands about
// band high.
func (m *masonry) place(n, from int, width float32, cols int, colW, gapX, gapY float32, band float64) {
	if !m.placed || from == 0 || len(m.lanes) != cols {
		from = 0
		m.lanes = slices.Grow(m.lanes[:0], cols)[:cols]
		for c := range m.lanes {
			m.lanes[c] = m.lanes[c][:0]
		}
	}
	m.ends = slices.Grow(m.ends[:0], cols)[:cols]
	for c, lane := range m.lanes {
		k, _ := slices.BinarySearch(lane, int32(from))
		m.lanes[c] = lane[:k]
		m.ends[c] = float64(gapX) / 2
		if k > 0 {
			i := lane[k-1]
			m.ends[c] = m.top[i] + float64(m.h[i]) + float64(gapY)
		}
	}
	m.top = slices.Grow(m.top[:0], n)[:n]
	m.h = slices.Grow(m.h[:0], n)[:n]
	m.col = slices.Grow(m.col[:0], n)[:n]
	m.pos = slices.Grow(m.pos[:0], n)[:n]
	for i := from; i < n; i++ {
		c := 0
		for k := 1; k < cols; k++ {
			if m.ends[k] < m.ends[c] {
				c = k
			}
		}
		h := heightOf(m.height, i, colW)
		m.top[i], m.h[i], m.col[i], m.pos[i] = m.ends[c], h, int32(c), int32(len(m.lanes[c]))
		m.lanes[c] = append(m.lanes[c], int32(i))
		m.ends[c] += float64(h) + float64(gapY)
	}
	total := 0.0
	if n > 0 {
		total = slices.Max(m.ends) - float64(gapY) + float64(gapX)/2
	}
	bands, b := 0, float32(0)
	if total > 0 {
		bands = int(math.Ceil(total / band))
		b = float32(total / float64(bands))
	}
	if b != m.band {
		// Bands of another height: the List measures them anew.
		m.s.rows.heights.clear()
	}
	m.n, m.width, m.cols, m.colW, m.gapX, m.gapY, m.placedGap, m.placed = n, width, cols, colW, gapX, gapY, gapY-gapX, true
	m.band, m.bands = b, bands
	m.s.rows.guess = float64(b)
}

// heightOf returns the height of item i, width wide, as height says: 0
// for less or for NaN.
func heightOf(height func(i int, width float32) float32, i int, width float32) float32 {
	h := height(i, width)
	if !(h > 0) {
		return 0
	}
	return min(h, 1<<24)
}

// view returns where the view shows, from the top of the first band, as
// the last layout left it, or as the app or the user asked since.
func (m *masonry) view() (top, bottom float64, ok bool) {
	l, st := &m.s.rows, m.st
	if !l.laidOut || st == nil || st.h <= 0 {
		return 0, 0, false
	}
	y := max(0, st.scrollY)
	if l.req.set && l.req.offset {
		y = l.req.y
	}
	top = y + l.border - l.padTop
	return top, top + float64(st.h) - 2*l.border, true
}

// remember notes the first item in view, which stays where it shows when
// the items are placed anew in a later frame.
func (m *masonry) remember() {
	m.anchor, m.anchorKey = -1, nil
	top, bottom, ok := m.view()
	if !ok {
		return
	}
	for _, lane := range m.lanes {
		if j := m.reach(lane, top); j < len(lane) {
			if i := int(lane[j]); m.top[i] < bottom && (m.anchor < 0 || i < m.anchor) {
				m.anchor = i
			}
		}
	}
	if m.anchor >= 0 && m.s.Key != nil {
		m.anchorKey = m.s.Key(m.anchor)
	}
}

// reach returns the first of the items of a column that ends below y, or
// starts there.
func (m *masonry) reach(lane []int32, y float64) int {
	lo, hi := 0, len(lane)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		i := lane[mid]
		if t := m.top[i]; t+float64(m.h[i]) > y || t >= y {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}

// find returns the item of n whose key is k, looking around near, or -1.
func (m *masonry) find(k any, near, n int) int {
	for d := 0; d <= listSearch; d++ {
		lo, hi := near-d, near+d
		if lo < 0 && hi >= n {
			break
		}
		if lo >= 0 && lo < n && m.s.Key(lo) == k {
			return lo
		}
		if d > 0 && hi >= 0 && hi < n && m.s.Key(hi) == k {
			return hi
		}
	}
	return -1
}

// build builds band r: the items that start in it, and those reaching into
// it not yet built, in order. It returns the cell of the item chosen, if
// among them.
func (m *masonry) build(c *context, grid *node, r int, item func(i int), under uint64) (cursor *node) {
	s := m.s
	lo := float64(r) * float64(m.band)
	hi := lo + float64(m.band)
	if r == m.bands-1 {
		hi = math.Inf(1)
	}
	m.pick = m.pick[:0]
	for _, lane := range m.lanes {
		for j := m.reach(lane, lo); j < len(lane); j++ {
			i := lane[j]
			if m.top[i] >= hi {
				break
			}
			if w, bit := i/64, uint64(1)<<(i%64); m.done[w]&bit == 0 {
				m.done[w] |= bit
				m.pick = append(m.pick, i)
			}
		}
	}
	slices.Sort(m.pick)
	band := coreBox(c).AlignSelf(Stretch).Height(m.band).Role(RoleNone)
	band.Children(func() {
		for _, i := range m.pick {
			if heightOf(m.height, int(i), m.colW) != m.h[i] {
				// Placed anew in the next frame.
				if m.stale < 0 || int(i) < m.stale {
					m.stale = int(i)
				}
				m.rt.animating = true
			}
			cell := s.cell(c, grid, int(i), m.n, m.h[i], item, under)
			cell.Absolute().Start(float32(m.col[i]) * (m.colW + m.gapX)).Top(float32(m.top[i] - lo)).Width(m.colW)
			if s.Selected != nil && int(i) == *s.Selected {
				cursor = cell
			}
		}
	})
	return cursor
}

// step returns the item the arrows move to from at, dx columns and dy
// items away, or -1 for none: Up and Down go to the item above or below in
// its column, Left and Right to the nearest in the next column with items.
func (m *masonry) step(at, dx, dy, _ int) int {
	if at < 0 || at >= m.n {
		return -1
	}
	c := int(m.col[at])
	if dy != 0 {
		lane := m.lanes[c]
		if k := int(m.pos[at]) + dy; k >= 0 && k < len(lane) {
			return int(lane[k])
		}
		return -1
	}
	y := m.top[at] + float64(m.h[at])/2
	for k := c + dx; k >= 0 && k < len(m.lanes); k += dx {
		if lane := m.lanes[k]; len(lane) > 0 {
			return m.nearest(lane, y)
		}
	}
	return -1
}

// nearest returns the item of a column nearest y, the upper of two as
// near.
func (m *masonry) nearest(lane []int32, y float64) int {
	j := m.reach(lane, y)
	best, dist := -1, math.Inf(1)
	for k := j - 1; k <= j; k++ {
		if k < 0 || k >= len(lane) {
			continue
		}
		i := lane[k]
		d := 0.0
		if t, b := m.top[i], m.top[i]+float64(m.h[i]); y < t {
			d = t - y
		} else if y > b {
			d = y - b
		}
		if d < dist {
			best, dist = int(i), d
		}
	}
	return best
}

// scrollTo scrolls the view to show item at align, or as little as shows
// it (near): now, if this pass placed the items, or once it does.
func (m *masonry) scrollTo(item int, align Align, near bool) {
	m.pending.set, m.pending.item, m.pending.align, m.pending.near = true, item, align, near
	if rt := m.rt; rt != nil && m.frame == rt.frame && m.pass == rt.pass {
		m.resolve()
	}
}

// resolve asks the List for the offset that shows the item the app asked
// for, gapX/2 from the edge, as a GridView's rows' padding does.
func (m *masonry) resolve() {
	p := m.pending
	if !p.set {
		return
	}
	m.pending.set = false
	if m.n == 0 {
		return
	}
	l := &m.s.rows
	i := max(0, min(p.item, m.n-1))
	top, bottom, pad := m.top[i], m.top[i]+float64(m.h[i]), float64(m.gapX)/2
	view := float64(m.viewH)
	if m.st != nil && m.st.h > 0 {
		view = float64(m.st.h) - 2*l.border
	}
	var y float64
	switch {
	case p.near:
		vt, vb, ok := m.view()
		switch {
		case ok && top-pad >= vt && bottom+pad <= vb:
			return // in view
		case ok && top-pad >= vt && bottom-top+2*pad <= view:
			y = bottom + pad - view // below: at the bottom
		default:
			y = top - pad
		}
	case p.align == Center:
		y = (top+bottom)/2 - view/2
	case p.align == End:
		y = bottom + pad - view
	default:
		y = top - pad
	}
	l.req = listRequest{set: true, offset: true, y: max(0, y+l.padTop-l.border)}
	l.scrolled = true
}

package ui

import (
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// stickerHeight gives item i one of a few aspect ratios, as a wall of
// stickers and pictures has.
func stickerHeight(i int, width float32) float32 {
	aspects := [...]float32{1, 1.6, 0.7, 1.25, 0.5, 0.8, 2.1}
	return width / aspects[(i*5+i/3)%len(aspects)]
}

// masonryPlaces places n items in cols columns colW wide the way a
// MasonryView says it does, each at the top of the column ending highest,
// the leftmost of those, gap apart, the first gap/2 below the top.
func masonryPlaces(n, cols int, colW, gap float32, height func(i int, width float32) float32) (top []float64, col []int) {
	ends := make([]float64, cols)
	for c := range ends {
		ends[c] = float64(gap) / 2
	}
	top, col = make([]float64, n), make([]int, n)
	for i := range n {
		c := 0
		for k := range ends {
			if ends[k] < ends[c] {
				c = k
			}
		}
		top[i], col[i] = ends[c], c
		ends[c] += float64(height(i, colW)) + float64(gap)
	}
	return top, col
}

// checkMasonry checks the view of s, view high, against where items go in
// cols columns colW wide: each placed there, each built shown there, at
// most most built, every item in view built, and none beyond the bands the
// List builds around the view, and slack further, which the List builds
// the frame it is scrolled by as much.
func checkMasonry(t *testing.T, s *GridState, n, cols int, colW float32, view float32, most int, slack float32, height func(int, float32) float32) {
	t.Helper()
	m := s.masonry
	if s.cols != cols || m.colW != colW || m.n != n {
		t.Fatalf("%d columns %v wide for %d items, not %d %v wide for %d", s.cols, m.colW, m.n, cols, colW, n)
	}
	top, col := masonryPlaces(n, cols, colW, 8, height)
	for i := range n {
		if m.top[i] != top[i] || int(m.col[i]) != col[i] {
			t.Fatalf("item %d is at %v in column %d, not at %v in column %d", i, m.top[i], m.col[i], top[i], col[i])
		}
	}
	rt := s.rt
	y := listStateOf(&s.rows).scrollY
	built := map[int]bool{}
	for id, i := range s.cells {
		st := rt.states[id]
		if st == nil || st.seen != rt.frame {
			continue
		}
		if built[i] {
			t.Fatalf("item %d built twice", i)
		}
		built[i] = true
		x, wantY := float32(col[i])*(colW+8), float32(top[i]-y)
		if abs(st.x-x) > 0.01 || abs(st.y-wantY) > 0.01 || st.w != colW || abs(st.h-height(i, colW)) > 0.01 {
			t.Fatalf("item %d shows at %v,%v %vx%v, not at %v,%v %vx%v", i, st.x, st.y, st.w, st.h, x, wantY, colW, height(i, colW))
		}
	}
	if len(built) > most {
		t.Errorf("%d items built, more than %d", len(built), most)
	}
	// Those built are in view, or in the bands beyond it the List builds
	// as it does rows (listOverscan), and the one each side partly shown.
	reach := float64(listOverscan+1)*float64(m.band) + float64(slack)
	for i := range built {
		if top[i] >= y+float64(view)+reach || top[i]+float64(height(i, colW)) <= y-reach {
			t.Errorf("item %d at %v built, scrolled %v", i, top[i], y)
		}
	}
	for i := range n {
		if float32(top[i]-y) < view && float32(top[i]-y)+height(i, colW) > 0 && !built[i] {
			t.Fatalf("item %d in view at %v, scrolled %v, is not built", i, top[i], y)
		}
	}
}

func TestMasonryView(t *testing.T) {
	const n = 10000
	sel, opened, built := -1, -1, 0
	s := GridState{Selected: &sel, Label: func(i int) string { return fmt.Sprintf("Sticker %d", i) }}
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Fill().Children(func() {
			if coreMasonryView(c, &s, n, 100, stickerHeight, func(i int) {
				built++
				coreTextf(c, "Sticker %d", i)
			}).Grow(1).Submitted() {
				opened = sel
			}
		})
	}, 448, 400)
	// (448 + 8) / (100 + 8): 4 columns, each (448 - 3*8) / 4 = 106 wide.
	checkMasonry(t, &s, n, 4, 106, 400, 56, 0, stickerHeight)
	built = 0
	tt.Frame()
	if built == 0 || built > 48 {
		t.Errorf("a frame built %d items of %d", built, n)
	}

	// Down the wall and back up: the items keep their places.
	shown := map[int]Rect{}
	for id, i := range s.cells {
		st := s.rt.states[id]
		shown[i] = Rect{st.x, st.y, st.w, st.h}
	}
	for range 60 {
		built = 0
		tt.Scroll(100, 100, 0, 337)
		if built > 56 {
			t.Fatalf("a frame scrolled built %d items", built)
		}
		checkMasonry(t, &s, n, 4, 106, 400, 56, 337, stickerHeight)
	}
	for range 61 {
		tt.Scroll(100, 100, 0, -337)
		checkMasonry(t, &s, n, 4, 106, 400, 56, 337, stickerHeight)
		tt.Frame()
		checkMasonry(t, &s, n, 4, 106, 400, 56, 0, stickerHeight)
	}
	if y := listStateOf(&s.rows).scrollY; y != 0 {
		t.Fatalf("back up, scrolled %v", y)
	}
	for id, i := range s.cells {
		st := s.rt.states[id]
		if r, ok := shown[i]; ok && r != (Rect{st.x, st.y, st.w, st.h}) {
			t.Errorf("back at the top, item %d shows at %v, not %v", i, Rect{st.x, st.y, st.w, st.h}, r)
		}
	}

	// A click chooses, Enter opens, End shows the last item.
	if err := tt.Click("Sticker 5"); err != nil {
		t.Fatal(err)
	}
	if sel != 5 {
		t.Fatalf("a click chose %d", sel)
	}
	tt.Key(0, KeyEnter)
	if opened != 5 {
		t.Errorf("Enter opened %d", opened)
	}
	tt.Key(0, KeyEnd)
	if sel != n-1 || !tt.HasText(fmt.Sprintf("Sticker %d", n-1)) {
		t.Fatalf("End chose %d, shown: %v", sel, tt.HasText(fmt.Sprintf("Sticker %d", n-1)))
	}
	checkMasonry(t, &s, n, 4, 106, 400, 56, 0, stickerHeight)
	tt.Key(0, KeyHome)
	if sel != 0 || listStateOf(&s.rows).scrollY != 0 {
		t.Errorf("Home chose %d, scrolled %v", sel, listStateOf(&s.rows).scrollY)
	}

	// Assistive technology: a list of n items, the one chosen with the focus.
	tt.Key(0, KeyEnd)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	tree := tt.h.access
	cell := accessNode(t, tree, platform.RoleListItem, fmt.Sprintf("Sticker %d", n-1))
	list := accessNode(t, tree, platform.RoleList, "")
	if list.SetSize != n || list.States&platform.AccessSelectable == 0 || cell.PosInSet != n || cell.SetSize != n || cell.States&platform.AccessChecked == 0 || tree.Focus != cell.ID {
		t.Errorf("the list %+v, the item %+v, the focus on %d", list, cell, tree.Focus)
	}
	if cell.Bounds.W != 106 || abs(float32(cell.Bounds.H)-stickerHeight(n-1, 106)) > 0.01 {
		t.Errorf("the last item is %v", cell.Bounds)
	}
}

// TestMasonryViewResizes keeps the item at the top in view as the window
// takes other widths, and so other columns, and places the items for them.
func TestMasonryViewResizes(t *testing.T) {
	const n = 10000
	s := GridState{Key: func(i int) any { return fmt.Sprint("k", i) }}
	tt := coreNewTester(func(c *context) {
		coreMasonryView(c, &s, n, 100, stickerHeight, func(i int) {
			coreTextf(c, "Sticker %d", i)
		}).Grow(1)
	}, 448, 400)
	for range 40 {
		tt.Scroll(100, 100, 0, 291)
	}
	tt.Frame()
	checkMasonry(t, &s, n, 4, 106, 400, 56, 0, stickerHeight)
	resize := func(w, cols int, colW float32, most int) {
		t.Helper()
		first := s.masonry.anchor
		if first <= 0 {
			t.Fatalf("the first item in view is %d", first)
		}
		tt.SetSize(w, 400)
		checkMasonry(t, &s, n, cols, colW, 400, most, 0, stickerHeight)
		m := s.masonry
		y := listStateOf(&s.rows).scrollY
		if top := m.top[first] - y; top < 0 || top+float64(m.h[first]) > 400 {
			t.Errorf("%d wide, item %d, first in view before, shows from %v to %v", w, first, top, top+float64(m.h[first]))
		}
	}
	// (664 + 8) / 108: 6 columns, (664 - 5*8) / 6 = 104 wide.
	resize(664, 6, 104, 72)
	resize(340, 3, (340-16)/3.0, 40)
	resize(448, 4, 106, 56)
	// Scrolling on from there keeps the places.
	for range 10 {
		tt.Scroll(100, 100, 0, -173)
		checkMasonry(t, &s, n, 4, 106, 400, 56, 173, stickerHeight)
	}
}

// TestMasonryViewChanges places items anew from the first whose height
// changed, and keeps the items in view where they are as items come after
// them.
func TestMasonryViewChanges(t *testing.T) {
	n := 2000
	tall := -1
	height := func(i int, w float32) float32 {
		if i == tall {
			return 3 * w
		}
		return stickerHeight(i, w)
	}
	s := GridState{}
	tt := coreNewTester(func(c *context) {
		coreMasonryView(c, &s, n, 100, height, func(i int) {
			coreTextf(c, "Sticker %d", i)
		}).Grow(1)
	}, 448, 400)
	for range 20 {
		tt.Scroll(100, 100, 0, 250)
	}
	tt.Frame()
	checkMasonry(t, &s, n, 4, 106, 400, 56, 0, height)
	// An item in view grows.
	m := s.masonry
	tall = m.anchor + 2
	tt.Frame()
	checkMasonry(t, &s, n, 4, 106, 400, 56, 0, height)
	if m.h[tall] != 318 {
		t.Errorf("item %d is %v high", tall, m.h[tall])
	}
	// Items come at the end: those in view stay.
	before := map[int]float32{}
	for id, i := range s.cells {
		before[i] = s.rt.states[id].y
	}
	n += 500
	tt.Frame()
	checkMasonry(t, &s, n, 4, 106, 400, 56, 0, height)
	for id, i := range s.cells {
		if y, ok := before[i]; ok && abs(s.rt.states[id].y-y) > 0.01 {
			t.Errorf("item %d moved from %v to %v as items came after it", i, y, s.rt.states[id].y)
		}
	}
}

// TestMasonryViewGap spaces the items in a column further by Gap.
func TestMasonryViewGap(t *testing.T) {
	var s GridState
	coreNewTester(func(c *context) {
		coreMasonryView(c, &s, 50, 100, stickerHeight, func(i int) {
			coreTextf(c, "Sticker %d", i)
		}).Grow(1).Gap(4)
	}, 448, 400)
	m := s.masonry
	lane := m.lanes[0]
	if a, b := lane[0], lane[1]; m.gapY != 12 || m.top[b] != m.top[a]+float64(m.h[a])+12 {
		t.Errorf("Gap(4): items %d and %d of a column are %v apart", a, b, m.top[b]-m.top[a]-float64(m.h[a]))
	}
}

// TestMasonryViewKeys moves the choice up and down within a column, and
// to the nearest item in the next column left and right.
func TestMasonryViewKeys(t *testing.T) {
	// In 3 columns (336 + 8) / 108, the items go:
	//
	//	0 [4, 204]    1 [4, 104]    2 [4, 104]
	//	6 [212, 312]  3 [112, 212]  4 [112, 162]
	//	              7 [220, 320]  5 [170, 220]
	//	                            8 [228, 328]
	heights := []float32{200, 100, 100, 100, 50, 50, 100, 100, 100}
	sel := -1
	var chosen Selection[int]
	s := GridState{Selected: &sel, Selection: &chosen}
	tt := coreNewTester(func(c *context) {
		coreMasonryView(c, &s, len(heights), 100, func(i int, _ float32) float32 { return heights[i] }, func(i int) {
			coreTextf(c, "Sticker %d", i)
		}).Grow(1)
	}, 336, 400)
	if err := tt.Click("Sticker 0"); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		key  Key
		want int
	}{
		{KeyDown, 6}, {KeyDown, 6}, {KeyUp, 0}, {KeyUp, 0}, {KeyRight, 1}, {KeyRight, 2}, {KeyRight, 2},
		{KeyDown, 4}, {KeyDown, 5}, {KeyLeft, 3}, {KeyLeft, 0}, {KeyLeft, 0}, {KeyEnd, 8}, {KeyLeft, 7}, {KeyHome, 0},
	} {
		tt.Key(0, step.key)
		if sel != step.want {
			t.Fatalf("%v chose %d, not %d", step.key, sel, step.want)
		}
	}
	// Shift chooses the items from the one chosen, in order.
	tt.Key(Shift, KeyDown)
	if sel != 6 || chosen.Len() != 7 || !chosen.Has(3) {
		t.Errorf("Shift-Down from 0 chose %d, %d chosen", sel, chosen.Len())
	}
	// Right to left, the first column is at the right.
	tt.SetDirection(RightToLeft)
	r0, _ := tt.Find("Sticker 0")
	r2, _ := tt.Find("Sticker 2")
	if r0.X < 200 || r2.X > 100 {
		t.Errorf("right to left, item 0 is at %v and 2 at %v", r0.X, r2.X)
	}
	tt.Key(0, KeyHome)
	tt.Key(0, KeyLeft)
	if sel != 1 {
		t.Errorf("right to left, Left from 0 chose %d", sel)
	}
}

// TestMasonryViewReorder drags items to another place, as in a GridView:
// before the item dropped on, or after it past its middle.
func TestMasonryViewReorder(t *testing.T) {
	heights := []float32{200, 100, 100, 100, 50, 50, 100, 100, 100}
	var moves [][]int
	sel := -1
	s := GridState{Selected: &sel, Reorder: func(items []int, to int) {
		moves = append(moves, append(slices.Clone(items), to))
	}}
	tt := coreNewTester(func(c *context) {
		coreMasonryView(c, &s, len(heights), 100, func(i int, _ float32) float32 { return heights[i] }, func(i int) {
			coreTextf(c, "Sticker %d", i)
		}).Grow(1)
	}, 336, 400)
	from, _ := tt.Find("Sticker 0")
	five, _ := tt.Find("Sticker 5")
	// On the right half of item 5, in the third column: before item 6.
	drag(tt, from.X, from.Y, five.X+90, five.Y+10)
	// On the left half of item 3, in the second column: before it.
	three, _ := tt.Find("Sticker 3")
	drag(tt, five.X, five.Y, three.X+5, three.Y+10)
	if fmt.Sprint(moves) != "[[0 6] [5 3]]" {
		t.Errorf("moves %v", moves)
	}
}

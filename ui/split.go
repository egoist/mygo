package ui

// Split lays out first and second side by side, with a divider between
// them that the user drags to resize them, or moves with the arrows once
// it has the keyboard focus: *size is the width of first, which the
// divider keeps 40 DIPs from either edge. With less room than that, first
// gives way, keeping *size for when there is room again. Changed reports a
// move. Right to left (Direction), first is on the right. SplitReset makes
// a double click on the divider put first back at a size the app chooses,
// and SplitLimits gives either pane a minimum and a maximum size.
//
//	ui.Split(c, &app.sidebar, app.files, app.editor).Fill()
func coreSplit(c *context, size *float32, first, second func()) *node {
	return split(c, size, first, second, false, false)
}

// SplitVertical is Split with first above second, *size its height.
func coreSplitVertical(c *context, size *float32, first, second func()) *node {
	return split(c, size, first, second, true, false)
}

// SplitPercent is Split with *p the percentage of the room first takes,
// from 0 to 100, so that both panes grow and shrink with the room, in
// proportion.
//
//	ui.SplitPercent(c, &app.sidebarPercent, app.files, app.editor).Fill()
func coreSplitPercent(c *context, p *float32, first, second func()) *node {
	return split(c, p, first, second, false, true)
}

// SplitVerticalPercent is SplitPercent with first above second.
func coreSplitVerticalPercent(c *context, p *float32, first, second func()) *node {
	return split(c, p, first, second, true, true)
}

// splitResetKey is the local that keeps a split's SplitReset settings.
type splitResetKey struct{}

// splitResetSettings are what SplitReset says of a split in this build,
// and what it said in the build before, which the input since applies by:
// the split the user double-clicked.
type splitResetSettings struct {
	now, shown       float32
	nowSet, shownSet bool
}

// SplitReset makes a double click on the divider of a Split put the first
// pane at v, in the split's own unit: DIPs for Split and SplitVertical, a
// percentage for SplitPercent and SplitVerticalPercent. A split without it
// keeps its size on a double click. The value is kept even when the window
// is too small to show it, so the pane takes it back when there is room
// again.
//
//	ui.Split(c, &app.sidebar, app.files, app.editor).SplitReset(220)
func (e *node) SplitReset(v float32) *node {
	if e.widget != "Split" {
		return e
	}
	s := coreLocal(e, splitResetKey{}, func() splitResetSettings { return splitResetSettings{} })
	s.now, s.nowSet = v, true
	return e
}

// splitResetOf returns the SplitReset settings of a split, or nil when it
// has none.
func splitResetOf(e *node) *splitResetSettings {
	if v, ok := e.st.locals[splitResetKey{}]; ok {
		if s, ok := v.(*splitResetSettings); ok {
			return s
		}
	}
	return nil
}

// SplitLimits bounds the sizes of a Split's panes, in DIPs, for all four
// kinds of split: the width of each pane in Split and SplitPercent, its
// height in SplitVertical and SplitVerticalPercent. A zero field sets no
// limit, and a minimum under the 40 DIPs every pane keeps changes nothing.
type SplitLimits struct {
	// FirstMin and FirstMax bound the first pane: the one *size measures,
	// on the left (on the right, right to left) or on top.
	FirstMin, FirstMax float32
	// SecondMin and SecondMax bound the second pane, which takes the rest
	// of the room.
	SecondMin, SecondMax float32
}

// SplitLimits gives the panes of a Split a minimum and a maximum size, in
// DIPs whichever the split's unit: the divider stops where a pane would
// leave its limits, as it stops 40 DIPs from either edge, both as the user
// drags it and as the arrows move it, and the panes keep within them as
// the room changes. Where the limits can't all hold, or don't fill the
// room:
//
//   - a pane's minimum wins over its maximum;
//   - when the room is smaller than both minimums together, both panes
//     keep them and overflow the end of the split, which the window, or
//     a Clip around it, cuts: give the window a minimum size that holds
//     them;
//   - when the room is larger than both maximums together, both panes
//     keep them and the rest of the room, after the second, stays empty.
//
// A size outside the limits is kept, and shown within them, until the
// user moves the divider. As with SplitReset, the input applies the limits
// of the build before, and the layout those of this build.
//
//	// A side panel between 280 and 480 DIPs wide, after a chat no
//	// narrower than 360.
//	ui.Split(c, &app.chatWidth, app.chat, app.panel).SplitLimits(ui.SplitLimits{
//		FirstMin: 360, SecondMin: 280, SecondMax: 480,
//	})
func (e *node) SplitLimits(l SplitLimits) *node {
	if e.widget != "Split" {
		return e
	}
	l.FirstMin, l.FirstMax = max(l.FirstMin, 0), max(l.FirstMax, 0)
	l.SecondMin, l.SecondMax = max(l.SecondMin, 0), max(l.SecondMax, 0)
	s := coreLocal(e, splitLimitsKey{}, func() splitLimitsSettings { return splitLimitsSettings{} })
	s.now, s.set = l, true
	l.applyTo(e)
	return e
}

// splitLimitsKey is the local that keeps a split's SplitLimits settings.
type splitLimitsKey struct{}

// splitLimitsSettings are what SplitLimits says of a split in this build,
// which the next build's input applies by.
type splitLimitsSettings struct {
	now SplitLimits
	set bool
}

// splitLimitsOf returns the SplitLimits settings of a split, or nil when it
// has none.
func splitLimitsOf(e *node) *splitLimitsSettings {
	if v, ok := e.st.locals[splitLimitsKey{}]; ok {
		if s, ok := v.(*splitLimitsSettings); ok {
			return s
		}
	}
	return nil
}

// splitSlack is how much a pane in DIPs grows, against the other's 1, so
// that it takes only the room the other can't, at its maximum: too little
// to move it by a fraction of a DIP otherwise, and a power of two, which
// the flexible lengths divide by exactly.
const splitSlack = 0x1p-64

// applyTo sets the limits on the panes of e, a split this build made, for
// this frame's layout: split builds one stack in it, holding first's box,
// the divider, second's box and the handle.
func (l SplitLimits) applyTo(e *node) {
	in := e.first
	if in == nil || in.first == nil || in.first.next == nil || in.first.next.next == nil {
		return
	}
	a, div := in.first, in.first.next
	b := div.next
	vertical := !in.row
	minPane := e.c.theme.Space(10)
	firstMin, secondMin := max(l.FirstMin, minPane), max(l.SecondMin, minPane)
	firstMax, secondMax := max(l.FirstMax, firstMin), max(l.SecondMax, secondMin)
	main, minMain, maxMain := &a.width, &a.minW, &a.maxW
	bMin, bMax := &b.minW, &b.maxW
	total := in.st.w
	if vertical {
		main, minMain, maxMain = &a.height, &a.minH, &a.maxH
		bMin, bMax = &b.minH, &b.maxH
		total = in.st.h
	}
	if main.u == unitPx {
		// Split and SplitVertical: first is as large as *size, within its
		// limits, and gives way to second's minimum down to its own.
		v := main.v
		if l.FirstMax > 0 {
			v = min(v, firstMax)
		}
		*main = px(max(v, firstMin))
		minMain.setPx(firstMin)
		if l.SecondMax > 0 {
			// First takes the room second can't.
			a.grow = splitSlack
		}
	} else {
		// The percent variants: the panes grow in proportion, and either
		// takes the room the other can't.
		minMain.setPx(firstMin)
		if l.SecondMax > 0 && a.grow == 0 {
			a.grow = splitSlack
		}
		if l.FirstMax > 0 && b.grow == 0 {
			b.grow = splitSlack
		}
	}
	if l.FirstMax > 0 {
		maxMain.setPx(firstMax)
	}
	bMin.setPx(secondMin)
	if l.SecondMax > 0 {
		bMax.setPx(secondMax)
	}
	div.accRange[0] = float64(l.fit(0, total, minPane))
	div.accRange[1] = float64(l.fit(total, total, minPane))
	div.accRange[2] = float64(l.fit(float32(div.accRange[2]), total, minPane))
}

// split shares the room between first and second: *size is first's size
// in DIPs, or with inPercent its percentage of the room.
func split(c *context, size *float32, first, second func(), vertical, inPercent bool) *node {
	t := c.theme
	minPane, grip := t.Space(10), t.Space(1.5)
	stack := func() *node {
		// One element or the other: creating one adds it to the parent.
		if vertical {
			return coreColumn(c)
		}
		return coreRow(c)
	}
	length := func(n *node, v float32) {
		if vertical {
			n.Height(v)
		} else {
			n.Width(v)
		}
	}
	minLength := func(n *node, v float32) {
		if vertical {
			n.MinHeight(v)
		} else {
			n.MinWidth(v)
		}
	}
	e := stack().AlignItems(Stretch)
	e.widget = "Split"
	// reset is what SplitReset said of the split in the build before, which
	// the input since applies by; this build's SplitReset says what the
	// next build's input applies by.
	reset := splitResetOf(e)
	if reset != nil {
		reset.shown, reset.shownSet = reset.now, reset.nowSet
		reset.now, reset.nowSet = 0, false
	}
	// lim is what SplitLimits said of the split in the build before, which
	// the input since applies by, as reset; this build's SplitLimits sets
	// what this frame lays out by.
	var lim SplitLimits
	if s := splitLimitsOf(e); s != nil {
		if s.set {
			lim = s.now
		}
		s.now, s.set = SplitLimits{}, false
	}
	e.Children(func() {
		// The panes meet at a line one DIP thick, with nothing between them
		// and it. The handle the pointer drags spans a few DIPs over either
		// pane, above them: it is placed in this box, which has no padding,
		// whatever the split's.
		in := stack().Grow(1).AlignItems(Stretch)
		minLength(in, 0)
		// The room to share, as the last frame laid it out.
		total := in.st.w
		if vertical {
			total = in.st.h
		}
		share := min(max(*size, 0), 100) / 100
		// shown is first's size as the last frame laid it out. Drags and
		// keys move the divider from there, not from *size, which a split
		// too small for it does not show.
		shown := *size
		switch {
		case inPercent && total > 0:
			shown = lim.fit(share*(total-1), total, minPane)
		case !inPercent && total > 0 && (*size > minPane || lim != SplitLimits{}):
			shown = lim.fit(*size, total, minPane)
		}
		set := func(v float32) {
			v = lim.fit(v, total, minPane)
			if inPercent {
				if total <= 1 {
					return
				}
				v = v * 100 / (total - 1)
			}
			if v != *size {
				*size = v
				e.st.markChanged()
				c.rt.consumed = true
			}
		}
		// resetTo puts first at the size SplitReset chose, in the split's
		// own unit. Unlike set, it keeps a size the room cannot show, as
		// the divider's press does, so that first takes it back when there
		// is room again.
		resetTo := func(v float32) {
			if inPercent {
				v = min(max(v, 0), 100)
			} else {
				v = lim.fit(v, 0, minPane)
			}
			if v != *size {
				*size = v
				e.st.markChanged()
				c.rt.consumed = true
			}
		}
		in.Children(func() {
			// The layout sizes the panes, not shown: shown uses the last
			// frame's room, and the layout uses this frame's.
			a := coreBox(c).Shrink(1).Clip()
			if inPercent {
				a.Grow(share)
				minLength(a, minPane)
			} else {
				length(a, *size)
				minLength(a, min(*size, minPane))
			}
			a.Children(first)

			// The line takes the focus between the panes, in their order.
			div := coreBox(c).Shrink(0).Focusable().Role(RoleSplitter).Label(c.texts().Divider())
			div.widget = "Divider"
			div.flags |= flagOwnRing
			length(div, 1)
			// The arrows move the line the way they point.
			back, forth := div.arrows()
			if vertical {
				back, forth = KeyUp, KeyDown
			}
			div.afterInput(func() {
				if div.Shortcut(0, back) {
					set(shown - t.Space(2.5))
				}
				if div.Shortcut(0, forth) {
					set(shown + t.Space(2.5))
				}
			})
			div.hasRange, div.accRange = true, [3]float64{float64(lim.fit(0, total, minPane)), float64(lim.fit(total, total, minPane)), float64(shown)}
			div.accStep = float64(t.Space(2.5))

			b := coreBox(c).Grow(1).Shrink(1).Clip()
			if inPercent {
				b.Grow(1 - share)
			}
			minLength(b, minPane)
			b.Children(second)

			// placeOn keeps the handle on the divider where this frame lays
			// it out, also when the first pane gives way.
			handle := coreBox(c).Absolute()
			handle.placeOn = div
			handle.flags |= flagDraggable | flagHover
			if vertical {
				handle.Left(0).Right(0).Top(-(grip - 1) / 2).Height(grip).Cursor(CursorResizeNS)
			} else {
				handle.Top(0).Bottom(0).Start(-(grip - 1) / 2).Width(grip).Cursor(CursorResizeEW)
			}
			// A press there focuses the line, as a press on it would. Only a
			// move changes the size, so a press keeps *size for when there is
			// room again. A double click restores the size SplitReset chose.
			dx, dy, held := handle.Dragged()
			handle.afterInput(func() {
				if reset != nil && reset.shownSet && handle.DoubleClicked() {
					div.focus(false)
					resetTo(reset.shown)
					return
				}
				if !held {
					// The next drag starts with the divider under the
					// pointer.
					if o, ok := handle.st.locals[splitOvershoot{}].(*float32); ok {
						*o = 0
					}
					return
				}
				div.focus(false)
				if dx == 0 && dy == 0 {
					return
				}
				d := dx
				switch {
				case vertical:
					d = dy
				case handle.rtl():
					d = -dx
				}
				// The pointer goes on past a pane's limit (its 40 DIPs, or
				// SplitLimits'), where the divider stops: what it went past
				// is kept, so that coming back moves the divider only once
				// the pointer is over it again.
				o, _ := keepLocal(handle.st, splitOvershoot{}, func() float32 { return 0 })
				want := shown + d + *o
				*o = want - lim.fit(want, total, minPane)
				set(want)
			})
			div.Draw(func(p *Painter, r Rect) {
				line := t.Border
				if held || handle.Hovered() {
					line = t.Accent
				}
				p.Fill(r, line, 0)
			})
			handle.Draw(func(p *Painter, r Rect) {
				if div.FocusVisible() {
					p.focusRingOf(div, r, [4]float32{})
				}
			})
		})
	})
	return e
}

// splitOvershoot keys a split's handle's local value: how far the pointer
// dragging the divider went past where the divider stopped, at a pane's
// limit.
type splitOvershoot struct{}

// fit clamps v, the first pane's size, to leave minPane on either side of
// the divider in total, the room as last laid out (0 before the first
// layout, for limits that don't depend on the room), and to keep both panes
// within l, as the layout does: a minimum wins over a maximum and over the
// room.
func (l SplitLimits) fit(v, total, minPane float32) float32 {
	firstMin, secondMin := max(l.FirstMin, minPane), max(l.SecondMin, minPane)
	if l.FirstMax > 0 {
		v = min(v, max(l.FirstMax, firstMin))
	}
	if total > 0 {
		room := total - 1
		if l.SecondMax > 0 {
			// First takes the room second can't, up to its own maximum.
			at := room - max(l.SecondMax, secondMin)
			if l.FirstMax > 0 {
				at = min(at, max(l.FirstMax, firstMin))
			}
			v = max(v, at)
		}
		v = min(v, room-secondMin)
	}
	return max(v, firstMin)
}

package ui

// ListRow describes the current row and its scoped build frame.
type ListRow struct {
	Frame    Frame
	Index    int
	owner    Element
	selected bool
}

func (r ListRow) Selected() bool    { return r.Frame.Valid() && r.selected }
func (r ListRow) ListFocused() bool { return r.owner.FocusWithin() }

// List declares a virtual list. Configure it, then provide Rows. The
// optional callback supports migration from the earlier List signature.
func List(f Frame, s *ListState, n int, row ...func(Frame, int)) Element {
	scope := f.enter()
	defer scope.leave()
	c := scope.c
	if c == nil {
		return Element{}
	}
	if len(row) > 1 {
		panic("ui: List accepts at most one row builder")
	}
	config := &listConfig{state: s, count: n}
	e := declareNode(c, func(c *context) *node {
		return coreList(c, config.prepare(c), n, func(i int) {
			if len(row) > 0 && row[0] != nil {
				row[0](makeFrame(c), i)
				c.rt.flushPending()
			}
		})
	})
	if node := e.unbuilt(); node != nil {
		node.listConfig = config
	}
	return e
}

type listConfig struct {
	state                *ListState
	count                int
	key                  func(int) any
	selection            Selector
	keySet, selectionSet bool
}

func (s *listConfig) prepare(c *context) *ListState {
	if s.state == nil {
		n := c.reuse
		n.st = c.rt.stateFor(n.id)
		s.state = coreLocal(n, "list", func() ListState { return ListState{} })
	}
	if s.keySet {
		s.state.Key = s.key
	}
	if s.selectionSet {
		s.state.Selection = s.selection
	}
	return s.state
}

// ItemKey identifies each row's item so its state follows reordering.
func (e Element) ItemKey(key func(int) any) Element {
	if n := e.unbuilt(); n != nil && n.listConfig != nil {
		n.listConfig.key, n.listConfig.keySet = key, true
	}
	return e
}

// Selection enables selection of items using the app's typed keys.
func (e Element) Selection(selection Selector) Element {
	if n := e.unbuilt(); n != nil && n.listConfig != nil {
		n.listConfig.selection, n.listConfig.selectionSet = selection, true
	}
	return e
}

// Rows supplies a list's row builder after configuration is complete.
func (e Element) Rows(row func(ListRow)) Element {
	n := e.unbuilt()
	if n == nil || n.listConfig == nil {
		return e
	}
	if n.pending == 0 {
		panic("ui: Rows must be configured before the list is built")
	}
	spec := n.listConfig
	p := &n.c.rt.pending[n.pending-1]
	p.build = func(c *context) *node {
		return coreList(c, spec.prepare(c), spec.count, func(i int) {
			if row == nil {
				return
			}
			owner := wrapElement(spec.state.frame.owner)
			selected := false
			if c.row != nil {
				selected = c.row.chosen
			}
			row(ListRow{Frame: makeFrame(c), Index: i, owner: owner, selected: selected})
			c.rt.flushPending()
		})
	}
	return e
}

package ui

// Stateful controls are declared before their constructors consume input.
type pendingNode struct {
	node    *node
	frame   Frame
	theme   *Theme
	buttons buttonStyle
	build   func(*context) *node
	mask    [4]uint64
	flags   uint32
}

type inputOptions struct {
	active      bool
	readOnly    bool
	password    bool
	placeholder string
	lines       [2]int
}

func declareNode(c *context, build func(*context) *node) Element {
	if c == nil || c.parent == nil {
		return Element{}
	}
	n := c.alloc()
	n.c, n.kind = c, kindBox
	n.ordinal = uint64(c.parent.nchild)
	n.id = mix(c.parent.id, n.ordinal+uint64(n.kind)<<56)
	c.parent.add(n)
	n.config.active = true
	n.pending = len(c.rt.pending) + 1
	c.rt.pending = append(c.rt.pending, pendingNode{node: n, frame: makeFrame(c), theme: c.theme, buttons: c.buttons, build: build})
	return wrapElement(n)
}

func (rt *engine) realize(n *node) {
	if n == nil || n.pending == 0 {
		return
	}
	p := rt.pending[n.pending-1]
	style := *n
	*n = node{c: style.c, epoch: style.epoch, id: style.id, serial: style.serial, parent: style.parent, next: style.next,
		depth: style.depth, ordinal: style.ordinal, key: style.key, config: style.config, shrink: 1,
		justify: alignAuto, align: alignAuto, self: alignAuto, alignContent: alignAuto, justifyItems: alignAuto, justifySelf: alignAuto,
		flags: style.flags & p.flags}
	c := &rt.c
	scope := p.frame.enter()
	savedTheme, savedButtons, savedOptions, savedReuse := c.theme, c.buttons, c.inputOptions, c.reuse
	c.theme, c.buttons, c.inputOptions, c.reuse = p.theme, p.buttons, n.config, n
	defer func() {
		c.theme, c.buttons, c.inputOptions, c.reuse = savedTheme, savedButtons, savedOptions, savedReuse
		scope.leave()
	}()
	result := p.build(c)
	if result == nil {
		n.flags |= flagInvisible | flagPassThrough
		n.st = c.rt.stateFor(n.id)
		return
	}
	applyDeclaredStyle(result, &style, p)
	if result != n {
		n.redirect = result
	}
}

func (rt *engine) flushPending() {
	for i := 0; i < len(rt.pending); i++ {
		rt.realize(rt.pending[i].node)
	}
}

func (e Element) Placeholder(s string) Element {
	if n := e.unbuilt(); n != nil {
		if n.pending != 0 {
			n.config.placeholder = s
		} else {
			e.node().Placeholder(s)
		}
	}
	return e
}

func (e Element) ReadOnly(on bool) Element {
	if n := e.unbuilt(); n != nil {
		if n.pending != 0 {
			n.config.readOnly = on
		} else {
			e.node().ReadOnly(on)
		}
	}
	return e
}

func (e Element) Password() Element {
	if n := e.unbuilt(); n != nil {
		if n.pending != 0 {
			n.config.password = true
		} else {
			e.node().Password()
		}
	}
	return e
}

func (e Element) Lines(minimum, maximum int) Element {
	if n := e.unbuilt(); n != nil {
		if n.pending != 0 {
			n.config.lines = [2]int{minimum, maximum}
		} else {
			e.node().Lines(minimum, maximum)
		}
	}
	return e
}

func (ed *editor) declaredOptions(c *context) {
	if o := c.inputOptions; o.active {
		ed.readOnly, ed.password, ed.placeholder, ed.lines = o.readOnly, o.password, o.placeholder, o.lines
	}
}

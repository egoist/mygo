package ui

import "testing"

// A node lives per identity: the element of a keyed control is built
// again in the node it had before, so that what the identity holds does
// not change from frame to frame.
func TestNodeLivesPerIdentity(t *testing.T) {
	var first *node
	tt := NewTester(func(c *Context) {
		c.Key("row")
		e := Box(c).Height(1)
		n := e.lookup()
		if n == nil {
			t.Fatal("the element has no node")
		}
		if first == nil {
			first = n
			return
		}
		if n != first {
			t.Fatal("the element was built in another node")
		}
	}, 100, 50)
	tt.Frame()
	tt.Frame()
}

// An element keyed after it was built is built in a node of its own each
// frame: the identity takes the node the element was built in, so that
// nothing moves and no handle goes stale. What the identity holds does
// not change from frame to frame.
func TestLateKeyedElementIsBuiltInANodeOfItsOwn(t *testing.T) {
	var ids []uint64
	var nodes []*node
	tt := NewTester(func(c *Context) {
		e := Box(c).Key("row").Height(1)
		n := e.lookup()
		if n == nil {
			t.Fatal("the element has no node")
		}
		ids = append(ids, n.id)
		nodes = append(nodes, n)
	}, 100, 50)
	tt.Frame()
	tt.Frame()
	if ids[0] != ids[1] {
		t.Errorf("the element changed its identity: %x, then %x", ids[0], ids[1])
	}
	if nodes[0] == nodes[1] {
		t.Error("the element kept its node, though it was keyed late")
	}
}

// A handle keeps naming the element it was made for, though the element
// is keyed after: the identity takes the node the element was built in,
// so nothing moves.
func TestHandleSurvivesALateKey(t *testing.T) {
	tt := NewTester(func(c *Context) {
		b := Box(c)
		b.Key("x")
		b.Label("kept")
		if b.lookup() == nil {
			t.Fatal("the handle does not name the element any more")
		}
	}, 100, 50)
	tt.Frame()
	tt.Frame()
}

// An element that goes with an exit transition comes back to a node of
// its own: the transition keeps a copy of the element as it showed, not
// the node the element is built in.
func TestElementComesBackAsItsExitEnds(t *testing.T) {
	show := true
	tr := ElementTransition{Exit: &Motion{}}
	var live *node
	tt := NewTester(func(c *Context) {
		if !show {
			return
		}
		live = Box(c).Key("row").Height(1).Transition(tr).lookup()
	}, 100, 50)
	tt.Frame()
	show = false
	tt.Frame()
	var ghost *node
	for _, r := range tt.rt.trans {
		if r.ghost != nil {
			ghost = r.ghost
		}
	}
	if ghost == nil {
		t.Fatal("the exit keeps no copy of the element that went")
	}
	if ghost == live {
		t.Fatal("the exit keeps the node the element is built in")
	}
	show = true
	tt.Frame()
	if live == nil {
		t.Fatal("the element came back with no node")
	}
	if live == ghost {
		t.Fatal("the element came back to the copy of its exit")
	}
}

// A closed window keeps no node: the pool, the states and the
// transitions hold none of the elements of the frame before.
func TestClosedWindowHoldsNoNode(t *testing.T) {
	tr := ElementTransition{Exit: &Motion{}}
	tt := NewTester(func(c *Context) {
		Box(c).Key("row").Height(1).Transition(tr)
	}, 100, 50)
	tt.Frame()
	tt.rt.close()
	if len(tt.rt.c.chunks) != 0 {
		t.Fatal("a closed window keeps the pool of its nodes")
	}
	for id, s := range tt.rt.states {
		if s.node != nil {
			t.Fatalf("the state %d keeps the node of its element", id)
		}
	}
	for id, r := range tt.rt.trans {
		if r.elem != nil || r.ghost != nil {
			t.Fatalf("the transition %d keeps a node of the element that went", id)
		}
	}
}

// Two elements under one parent with the same key share one state, but
// not one node: the second is built in a node of its own, so that it does
// not take the node the first is building in.
func TestDuplicateKeyDoesNotTakeTheNodeBeingBuilt(t *testing.T) {
	var first, second *node
	dupe := false
	tt := NewTester(func(c *Context) {
		if !dupe {
			return
		}
		first = Box(c).Key("same").Height(1).lookup()
		second = Box(c).Key("same").Height(2).lookup()
	}, 100, 50)
	// A Tester panics at a duplicate key; here the two elements are built
	// on purpose.
	tt.rt.strict = false
	dupe = true
	tt.Frame()
	if first == nil || second == nil {
		t.Fatal("an element has no node")
	}
	if first == second {
		t.Fatal("the second element took the node the first was building in")
	}
}

// A duplicate key leaves the tree of the frame whole: the element that
// has the key twice keeps the identity of its place, so that the node it
// is built in stays in the tree, and the elements after it are still there.
func TestDuplicateKeyKeepsTheTreeWhole(t *testing.T) {
	dupe := false
	tt := NewTester(func(c *Context) {
		if !dupe {
			return
		}
		Box(c).Key("same").Height(1)
		Box(c).Key("same").Height(2)
		Box(c).Height(3)
	}, 100, 50)
	tt.rt.strict = false
	dupe = true
	tt.Frame()
	var heights []float32
	for e := tt.rt.c.root.first; e != nil; e = e.next {
		heights = append(heights, e.h)
	}
	if len(heights) != 3 {
		t.Fatalf("the tree of the frame holds %d elements, not 3: %v", len(heights), heights)
	}
	for i, h := range heights {
		if h != float32(i+1) {
			t.Errorf("the element %d of the frame has the height %v", i, h)
		}
	}
}

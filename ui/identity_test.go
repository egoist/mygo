package ui

import "testing"

// A node lives per identity: the element of a keyed control is built
// again in the node it had before, so that what the identity holds does
// not change from frame to frame.
func TestNodeLivesPerIdentity(t *testing.T) {
	var first *node
	tt := NewTester(func(c *Context) {
		e := Box(c).Key("row").Height(1)
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

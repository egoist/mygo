package ui

import "testing"

// TestReferenceNamesOneElement checks that a reference names the element
// it was made to and no other that takes its id later: its incarnation,
// the born of its state, is part of what it names (elementRef).
func TestReferenceNamesOneElement(t *testing.T) {
	var ref, again elementRef
	var first, second *node
	show := true
	tt := coreNewTester(func(c *context) {
		if !show {
			return
		}
		e := coreText(c, "Target").Key("target")
		if first == nil {
			first, ref = e, refTo(e)
		} else {
			second, again = e, refTo(e)
		}
	}, 200, 150)
	tt.Frame()
	if got := tt.rt.c.root.resolve(ref); got != first {
		t.Fatal("the reference did not resolve to the element it named")
	}
	// The element goes, and the state of its id with it.
	show = false
	tt.Frame()
	if got := tt.rt.c.root.resolve(ref); got != nil {
		t.Fatal("the reference kept its element after it went")
	}
	// Another element takes its id: the reference is none of its business.
	show = true
	tt.Frame()
	if ref.id != again.id {
		t.Fatal("the two elements did not take the same id")
	}
	if ref.born == again.born {
		t.Fatal("the two elements were one incarnation")
	}
	if got := tt.rt.c.root.resolve(ref); got != nil {
		t.Fatal("the reference followed its id to another element")
	}
	if got := tt.rt.c.root.resolve(again); got != second {
		t.Fatal("the new element's own reference did not resolve to it")
	}
}

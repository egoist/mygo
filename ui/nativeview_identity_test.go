package ui

import "testing"

// labelID returns the id the last frame gave the element showing or labeled
// text, and 0 for none: what the element keeps from frame to frame (its
// text, its caret, whether it has the focus) hangs on that id.
func labelID(tt *Tester, text string) uint64 {
	for _, n := range tt.rt.labels {
		if n.text == text {
			return n.id
		}
	}
	return 0
}

// TestNativeViewHiddenKeepsKeyedSiblingIdentity pins the identity of the
// sibling after a conditional native view. A native view's node exists only
// while the content builds it, so a sibling after it, built without a key,
// is numbered again once the view goes and takes a new identity with it: the
// focus, the text and the local state of the old one stay behind. A sibling
// built with Context.Key keeps its identity, and so its focus, across the
// hide. Nothing here calls the native view's Focus: hiding the node is the
// only thing that moves.
func TestNativeViewHiddenKeepsKeyedSiblingIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		// input builds the sibling after the conditional native view, keyed
		// or not.
		input func(c *Context, value *string) Element
		// wantLost says whether hiding the native view is expected to take
		// the sibling's focus with its old identity.
		wantLost bool
	}{
		{
			name:     "unkeyed",
			input:    func(c *Context, value *string) Element { return TextInput(c, value) },
			wantLost: true,
		},
		{
			name:     "keyed",
			input:    func(c *Context, value *string) Element { return TextInput(c.Key("after"), value) },
			wantLost: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := &testNativeView{name: "view", view: &testPlatformView{handle: 7}}
			shown := true
			var before, after string
			tt := NewTester(func(c *Context) {
				Column(c).Fill().AlignItems(Stretch).Children(func() {
					TextInput(c.Key("before"), &before).Label("Before")
					if shown {
						NativeView(c, view).Height(60)
					}
					tc.input(c, &after).Label("After")
				})
			}, 200, 300)
			if err := tt.Click("After"); err != nil {
				t.Fatal(err)
			}
			if !tt.Focused("After") {
				t.Fatal("the click did not focus the input After")
			}
			id := labelID(tt, "After")
			if id == 0 || tt.rt.focused == 0 {
				t.Fatalf("the frame gave the input After id %d and the content focus %d: want both", id, tt.rt.focused)
			}

			// The content stops building the native view's node alone: what
			// the next frame leaves is the identity shift and nothing else.
			shown = false
			tt.Frame()

			lost := !tt.Focused("After")
			if lost != tc.wantLost {
				t.Errorf("hiding the native view left the %s input After focused %v: want %v", tc.name, !lost, !tc.wantLost)
			}
			got := labelID(tt, "After")
			switch {
			case tc.wantLost && got == id:
				t.Errorf("hiding the native view left the unkeyed input After its id %d: want the new id that drops its focus", got)
			case !tc.wantLost && got != id:
				t.Errorf("hiding the native view moved the keyed input After from id %d to %d", id, got)
			}
		})
	}
}

// TestNativeViewPlaceholderKeepsSiblingIdentity is the shape the hosting
// example builds (examples/native-view-hosting/main.go:144-157): the content
// shows the native view, or a placeholder in its place, one element either
// way. The sibling after the hole keeps the number it had, and so its
// identity and focus, across the swap without a key of its own.
func TestNativeViewPlaceholderKeepsSiblingIdentity(t *testing.T) {
	view := &testNativeView{name: "view", view: &testPlatformView{handle: 7}}
	shown := true
	var before, after string
	tt := NewTester(func(c *Context) {
		Column(c).Fill().AlignItems(Stretch).Children(func() {
			TextInput(c, &before).Label("Before")
			if shown {
				NativeView(c, view).Grow(1)
			} else {
				Column(c).Fill().Children(func() {
					Text(c, "No native view is shown here.")
				})
			}
			TextInput(c, &after).Label("After")
		})
	}, 200, 300)
	if err := tt.Click("After"); err != nil {
		t.Fatal(err)
	}
	if !tt.Focused("After") {
		t.Fatal("the click did not focus the input After")
	}
	id := labelID(tt, "After")

	shown = false
	tt.Frame()
	if !tt.Focused("After") {
		t.Error("showing the placeholder in the native view's place left the input After unfocused: want the focus kept")
	}
	if got := labelID(tt, "After"); got != id {
		t.Errorf("showing the placeholder in the native view's place moved the input After from id %d to %d", id, got)
	}
}

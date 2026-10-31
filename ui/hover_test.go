package ui

import (
	"reflect"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func TestHoverCallbackEnterLeaveAndNoRebuildRepeat(t *testing.T) {
	var got []bool
	building := false
	tt := NewTester(func(c *Context) {
		building = true
		defer func() { building = false }()
		Box(c).Size(100, 100).OnHover(func(over bool) {
			if building {
				t.Fatal("hover callback ran during construction")
			}
			got = append(got, over)
		})
	}, 200, 200)
	tt.Move(50, 50)
	tt.Frame()
	tt.Frame()
	tt.Move(60, 60)
	tt.Move(150, 150)
	if !reflect.DeepEqual(got, []bool{true, false}) {
		t.Fatalf("hover transitions = %v", got)
	}
}

func TestHoverCallbackMatchesPressedHoverOnWindowLeave(t *testing.T) {
	var got []bool
	hovered := false
	tt := NewTester(func(c *Context) {
		button := Button(c, "Hold").Size(100, 100).OnHover(func(over bool) { got = append(got, over) })
		hovered = button.Hovered()
	}, 200, 200)
	tt.Press(50, 50)
	tt.rt.event(platform.SurfaceEvent{Kind: platform.PointerLeave})
	tt.Frame()
	if !hovered || !reflect.DeepEqual(got, []bool{true}) {
		t.Fatalf("pressed hover = %v, callbacks %v", hovered, got)
	}
	tt.Release(150, 150)
	if !reflect.DeepEqual(got, []bool{true, false}) {
		t.Fatalf("release hover callbacks = %v", got)
	}
}

func TestHoverCallbackDisappearanceAndStateReuse(t *testing.T) {
	show, replacement := true, false
	var got []string
	tt := NewTester(func(c *Context) {
		if show {
			c.Key("original")
			Box(c).Size(100, 100).OnHover(func(over bool) {
				if over {
					got = append(got, "enter")
				} else {
					got = append(got, "leave")
				}
			})
		}
		if replacement {
			c.Key("replacement")
			Box(c).Size(100, 100).OnHover(func(over bool) {
				if over {
					got = append(got, "replacement")
				}
			})
		}
	}, 200, 200)
	tt.Move(50, 50)
	show = false
	tt.Frame()
	replacement = true
	tt.Frame()
	tt.Frame()
	if !reflect.DeepEqual(got, []string{"enter", "leave", "replacement"}) {
		t.Fatalf("disappearance/reuse transitions = %v", got)
	}
}

func TestHoverCallbackDisabledAndLatestCommittedCallback(t *testing.T) {
	disabled := false
	version := 1
	var got []int
	tt := NewTester(func(c *Context) {
		v := version
		Box(c).Size(100, 100).Disabled(disabled).OnHover(func(over bool) {
			if over {
				got = append(got, v)
			} else {
				got = append(got, -v)
			}
		})
	}, 200, 200)
	tt.Move(50, 50)
	version = 2
	tt.Frame()
	tt.Move(150, 150)
	disabled = true
	tt.Frame()
	tt.Move(50, 50)
	disabled = false
	tt.Frame()
	disabled = true
	tt.Frame()
	if !reflect.DeepEqual(got, []int{1, -2, 2, -2}) {
		t.Fatalf("disabled/latest callback transitions = %v", got)
	}
}

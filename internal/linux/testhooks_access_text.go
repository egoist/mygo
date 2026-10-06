//go:build linux && (amd64 || arm64)

package linux

import (
	"github.com/ebitengine/purego"
	"sync"
)

type TestAccessText struct {
	Content, Selected, FirstLine string
	Start, End                   int
	Bounds                       [4]float64
	Visible, Selectable          bool
}

var (
	textTestsOnce                                     sync.Once
	atkTestCount                                      func(ptr) int32
	atkTestSelection                                  func(ptr, int32, *int32, *int32) ptr
	atkTestStringAt                                   func(ptr, int32, int32, *int32, *int32) ptr
	atkTestRangeExtents                               func(ptr, int32, int32, int32, *atkTextRect)
	atkTestAddSelection                               func(ptr, int32, int32) bool
	atkTestRemoveSelection                            func(ptr, int32) bool
	testTextWatches                                   = map[ptr]*[3]int{}
	testTextChanged, testTextSelection, testTextCaret ptr
)

func loadTextTests() {
	a, _ := open("libatk-1.0.so.0")
	mustBind(a, &atkTestCount, "atk_text_get_character_count")
	mustBind(a, &atkTestSelection, "atk_text_get_selection")
	mustBind(a, &atkTestStringAt, "atk_text_get_string_at_offset")
	mustBind(a, &atkTestRangeExtents, "atk_text_get_range_extents")
	mustBind(a, &atkTestAddSelection, "atk_text_add_selection")
	mustBind(a, &atkTestRemoveSelection, "atk_text_remove_selection")
	testTextChanged = purego.NewCallback(func(obj ptr, position, length int32, text, data ptr) {
		if counts := testTextWatches[obj]; counts != nil {
			counts[0]++
		}
	})
	testTextSelection = purego.NewCallback(func(obj, data ptr) {
		if counts := testTextWatches[obj]; counts != nil {
			counts[1]++
		}
	})
	testTextCaret = purego.NewCallback(func(obj ptr, offset int32, data ptr) {
		if counts := testTextWatches[obj]; counts != nil {
			counts[2]++
		}
	})
}

// TestAccessibilityTextEvents observes actual GObject text signals. A
// reference keeps the object valid through removal until stop disconnects.
func TestAccessibilityTextEvents(handle uintptr, label string) (read func() [3]int, stop func(), ok bool) {
	ns, _ := TestAccessibility(handle)
	textTestsOnce.Do(loadTextTests)
	for _, n := range ns {
		if n.Label == label && gTypeCheckInstanceIsA(n.obj, atkTextGetType()) {
			counts := &[3]int{}
			testTextWatches[n.obj] = counts
			gObjectRef(n.obj)
			var handlers []uint64
			for _, s := range []struct {
				name string
				cb   ptr
			}{{"text-insert", testTextChanged}, {"text-remove", testTextChanged}, {"text-selection-changed", testTextSelection}, {"text-caret-moved", testTextCaret}} {
				handlers = append(handlers, gSignalConnectData(n.obj, cs(s.name), s.cb, 0, 0, 0))
			}
			return func() [3]int { return *counts }, func() {
				for _, h := range handlers {
					gSignalHandlerDisconnect(n.obj, h)
				}
				delete(testTextWatches, n.obj)
				gObjectUnref(n.obj)
			}, true
		}
	}
	return nil, nil, false
}

func TestAccessibilityText(handle uintptr, label string) (out TestAccessText, ok bool) {
	ns, _ := TestAccessibility(handle)
	textTestsOnce.Do(loadTextTests)
	for _, n := range ns {
		if n.Label != label {
			continue
		}
		if !gTypeCheckInstanceIsA(n.obj, atkTextGetType()) || atkObjectGetRole(n.obj) == atkRole.password {
			return out, false
		}
		out.Content = takeStr(atkTextGetText(n.obj, 0, -1))
		var start, end int32
		out.Selected = takeStr(atkTestSelection(n.obj, 0, &start, &end))
		out.Start, out.End = int(start), int(end)
		out.FirstLine = takeStr(atkTestStringAt(n.obj, 0, 3, &start, &end))
		var r atkTextRect
		atkTestRangeExtents(n.obj, 0, 1, 0 /* ATK_XY_SCREEN */, &r)
		out.Bounds = [4]float64{float64(r.X), float64(r.Y), float64(r.W), float64(r.H)}
		out.Visible = r.W > 0 && r.H > 0
		if an := accessObjects[n.obj]; an != nil && an.n.Text != nil {
			out.Selectable = an.n.Text.Selectable
		}
		return out, true
	}
	return
}

func TestAccessibilitySelectText(handle uintptr, label string, start, end int) bool {
	ns, _ := TestAccessibility(handle)
	textTestsOnce.Do(loadTextTests)
	for _, n := range ns {
		if n.Label == label && gTypeCheckInstanceIsA(n.obj, atkTextGetType()) {
			atkTestRemoveSelection(n.obj, 0)
			return atkTestAddSelection(n.obj, int32(start), int32(end))
		}
	}
	return false
}

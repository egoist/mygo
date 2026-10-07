//go:build windows && (amd64 || arm64)

package windows

import "unsafe"

type TestAccessText struct {
	Content, Selected, FirstLine string
	Start, End                   int // rune offsets: UIA has no endpoint-offset getter
	Bounds                       [4]float64
	Visible, Selectable          bool
}

var (
	procSafeArrayGetElement = oleaut32.NewProc("SafeArrayGetElement")
	procSafeArrayDestroy    = oleaut32.NewProc("SafeArrayDestroy")
	procSafeArrayGetUBound  = oleaut32.NewProc("SafeArrayGetUBound")
)

func testRangeText(r uintptr) string {
	var b uintptr
	comCall(r, 12, ^uintptr(0), uintptr(unsafe.Pointer(&b)))
	return takeBSTR(b)
}
func testArrayFirst(sa uintptr) uintptr {
	var v uintptr
	index := int32(0)
	procSafeArrayGetElement.Call(sa, uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&v)))
	return v
}

func TestAccessibilityText(handle uintptr, label string) (out TestAccessText, ok bool) {
	ns, _ := TestAccessibility(handle)
	for _, n := range ns {
		if n.Label != label {
			continue
		}
		p := uiaPattern(n.simple, 10014)
		if p == 0 {
			return out, false
		}
		defer release(p)
		var doc uintptr
		if failed(comCall(p, 7, uintptr(unsafe.Pointer(&doc)))) || doc == 0 {
			return out, false
		}
		defer release(doc)
		out.Content = testRangeText(doc)
		var sa uintptr
		comCall(p, 3, uintptr(unsafe.Pointer(&sa)))
		if sa != 0 {
			sel := testArrayFirst(sa)
			if sel != 0 {
				out.Selected = testRangeText(sel)
				if r := uiaRanges[sel]; r != nil {
					out.Start, out.End = r.start, r.end
				}
				release(sel)
			}
			procSafeArrayDestroy.Call(sa)
		}
		var line uintptr
		comCall(doc, 3, uintptr(unsafe.Pointer(&line)))
		if line != 0 {
			comCall(line, 6, 3)
			out.FirstLine = testRangeText(line)
			release(line)
		}
		var bounds uintptr
		comCall(doc, 10, uintptr(unsafe.Pointer(&bounds)))
		if bounds != 0 {
			var upper int32
			procSafeArrayGetUBound.Call(bounds, 1, uintptr(unsafe.Pointer(&upper)))
			if upper >= 3 {
				for i := range out.Bounds {
					index := int32(i)
					procSafeArrayGetElement.Call(bounds, uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&out.Bounds[i])))
				}
			}
			procSafeArrayDestroy.Call(bounds)
		}
		comCall(p, 4, uintptr(unsafe.Pointer(&sa)))
		if sa != 0 {
			var upper int32
			procSafeArrayGetUBound.Call(sa, 1, uintptr(unsafe.Pointer(&upper)))
			out.Visible = upper >= 0
			procSafeArrayDestroy.Call(sa)
		}
		var selection int32
		comCall(p, 8, uintptr(unsafe.Pointer(&selection)))
		out.Selectable = selection == 1
		return out, true
	}
	return
}

func TestAccessibilitySelectText(handle uintptr, label string, start, end int) bool {
	ns, _ := TestAccessibility(handle)
	for _, n := range ns {
		if n.Label == label {
			p := uiaPattern(n.simple, 10014)
			if p == 0 {
				return false
			}
			defer release(p)
			var r uintptr
			if failed(comCall(p, 7, uintptr(unsafe.Pointer(&r)))) || r == 0 {
				return false
			}
			defer release(r)
			// Collapse to the beginning, then move each endpoint by characters.
			comCall(r, 15, 1, r, 0)
			var moved int32
			comCall(r, 14, 1, 0, uintptr(end), uintptr(unsafe.Pointer(&moved)))
			comCall(r, 14, 0, 0, uintptr(start), uintptr(unsafe.Pointer(&moved)))
			return comCall(r, 16) == sOK
		}
	}
	return false
}

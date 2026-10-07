//go:build linux && (amd64 || arm64)

package linux

import (
	"math"
	"unicode/utf8"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo/internal/platform"
)

type atkTextRect struct{ X, Y, W, H int32 }
type atkTextRange struct {
	Bounds     atkTextRect
	Start, End int32
	Content    ptr
}

func (an *accessNode) textQuery(q platform.AccessTextQuery) platform.AccessTextResult {
	return an.n.Text.Ask(q)
}

func (an *accessNode) selectText(start, end int32) bool {
	if an.n.Text == nil || !an.n.Text.Selectable || start < 0 || end < 0 || int(start) > an.n.Text.Length || int(end) > an.n.Text.Length {
		return false
	}
	an.tree.s.send(platform.SurfaceEvent{Kind: platform.AccessAction, Action: platform.AccessSetSelection, ID: an.n.ID, From: int(start), To: int(end)})
	return true
}

func (an *accessNode) textOrigin(coords int32) (float64, float64) {
	if coords == atkXYParent && an.parent != nil {
		return -an.parent.n.Bounds.X, -an.parent.n.Bounds.Y
	}
	var x, y, w, h int32
	atkComponentGetExtents(an.tree.root, &x, &y, &w, &h, coords)
	return float64(x), float64(y)
}

func (an *accessNode) textRect(start, end int, coords int32) atkTextRect {
	rs := an.textQuery(platform.AccessTextQuery{Kind: platform.TextRangeBounds, Start: start, End: end}).Rects
	if len(rs) == 0 {
		return atkTextRect{X: -1, Y: -1}
	}
	r := rs[0]
	for _, s := range rs[1:] {
		right, bottom := max(r.X+r.W, s.X+s.W), max(r.Y+r.H, s.Y+s.H)
		r.X, r.Y = min(r.X, s.X), min(r.Y, s.Y)
		r.W, r.H = right-r.X, bottom-r.Y
	}
	x, y := an.textOrigin(coords)
	return atkTextRect{X: int32(math.Floor(x + r.X)), Y: int32(math.Floor(y + r.Y)), W: int32(math.Ceil(r.W)), H: int32(math.Ceil(r.H))}
}

// The callback set is allocated once at GType registration, never per
// node. ATK owns returned strings, ranges and the null-terminated array.
func accessTextCallbacks(node func(ptr) *accessNode) map[int]ptr {
	cb := purego.NewCallback
	getText := cb(func(obj ptr, start, end int32) ptr {
		if an := node(obj); an != nil {
			if an.n.Text == nil {
				r := an.text()
				a, b := span(start, end, len(r))
				return cString(string(r[a:b]))
			}
			return cString(an.textQuery(platform.AccessTextQuery{Kind: platform.TextSlice, Start: int(start), End: int(end)}).Text)
		}
		return 0
	})
	charAt := cb(func(obj ptr, offset int32) uint32 {
		if an := node(obj); an != nil && an.n.Text != nil && offset >= 0 && int(offset) < an.n.Text.Length {
			s := an.textQuery(platform.AccessTextQuery{Kind: platform.TextSlice, Start: int(offset), End: int(offset) + 1}).Text
			r, _ := utf8.DecodeRuneInString(s)
			return uint32(r)
		}
		return 0
	})
	caret := cb(func(obj ptr) int32 {
		if an := node(obj); an != nil && an.n.Text != nil && an.n.Text.Selectable {
			return int32(an.n.Text.Caret)
		}
		return -1
	})
	count := cb(func(obj ptr) int32 {
		if an := node(obj); an != nil && an.n.Text != nil {
			return int32(an.n.Text.Length)
		}
		if an := node(obj); an != nil {
			return int32(len(an.text()))
		}
		return 0
	})
	selections := cb(func(obj ptr) int32 {
		if an := node(obj); an != nil && an.n.Text != nil && an.n.SelStart != an.n.SelEnd {
			return 1
		}
		return 0
	})
	selection := cb(func(obj ptr, i int32, start, end ptr) ptr {
		setInt(start, 0)
		setInt(end, 0)
		if an := node(obj); an != nil && an.n.Text != nil && i == 0 && an.n.SelStart != an.n.SelEnd {
			setInt(start, an.n.SelStart)
			setInt(end, an.n.SelEnd)
			return cString(an.textQuery(platform.AccessTextQuery{Kind: platform.TextSlice, Start: an.n.SelStart, End: an.n.SelEnd}).Text)
		}
		return 0
	})
	stringAt := func(obj ptr, offset int32, unit platform.AccessTextUnit, start, end ptr) ptr {
		setInt(start, 0)
		setInt(end, 0)
		an := node(obj)
		if an == nil || an.n.Text == nil || offset < 0 || int(offset) > an.n.Text.Length {
			return 0
		}
		r := an.textQuery(platform.AccessTextQuery{Kind: platform.TextUnitRange, Unit: unit, Start: int(offset)})
		setInt(start, r.Start)
		setInt(end, r.End)
		return cString(an.textQuery(platform.AccessTextQuery{Kind: platform.TextSlice, Start: r.Start, End: r.End}).Text)
	}
	granularity := cb(func(obj ptr, offset, gran int32, start, end ptr) ptr {
		// ATK's sentence unit is not implemented: returning NULL lets
		// AT-SPI report that explicitly, rather than call a line a sentence.
		units := map[int32]platform.AccessTextUnit{0: platform.TextCharacter, 1: platform.TextWord, 3: platform.TextLine, 4: platform.TextParagraph}
		u, ok := units[gran]
		if !ok {
			setInt(start, 0)
			setInt(end, 0)
			return 0
		}
		return stringAt(obj, offset, u, start, end)
	})
	atOffset := cb(func(obj ptr, offset, boundary int32, start, end ptr) ptr {
		u := platform.TextCharacter
		switch boundary {
		case 1, 2:
			u = platform.TextWord
		case 5, 6:
			u = platform.TextLine
		case 3, 4:
			setInt(start, 0)
			setInt(end, 0)
			return 0
		}
		return stringAt(obj, offset, u, start, end)
	})
	charExtents := cb(func(obj ptr, offset int32, x, y, w, h ptr, coords int32) {
		r := atkTextRect{X: -1, Y: -1}
		if an := node(obj); an != nil && an.n.Text != nil {
			u := an.textQuery(platform.AccessTextQuery{Kind: platform.TextUnitRange, Unit: platform.TextCharacter, Start: int(offset)})
			r = an.textRect(u.Start, u.End, coords)
		}
		setInt(x, int(r.X))
		setInt(y, int(r.Y))
		setInt(w, int(r.W))
		setInt(h, int(r.H))
	})
	point := cb(func(obj ptr, x, y, coords int32) int32 {
		if an := node(obj); an != nil && an.n.Text != nil {
			ox, oy := an.textOrigin(coords)
			return int32(an.textQuery(platform.AccessTextQuery{Kind: platform.TextOffsetAtPoint, X: float64(x) - ox, Y: float64(y) - oy}).Start)
		}
		return -1
	})
	add := cb(func(obj ptr, start, end int32) bool {
		an := node(obj)
		return an != nil && an.n.SelStart == an.n.SelEnd && an.selectText(start, end)
	})
	remove := cb(func(obj ptr, i int32) bool {
		an := node(obj)
		return an != nil && i == 0 && an.n.Text != nil && an.n.SelStart != an.n.SelEnd && an.selectText(int32(an.n.Text.Caret), int32(an.n.Text.Caret))
	})
	set := cb(func(obj ptr, i, start, end int32) bool {
		an := node(obj)
		return an != nil && i == 0 && an.n.SelStart != an.n.SelEnd && an.selectText(start, end)
	})
	setCaret := cb(func(obj ptr, i int32) bool { an := node(obj); return an != nil && an.selectText(i, i) })
	rangeExtents := cb(func(obj ptr, start, end, coords int32, rect ptr) {
		if rect == 0 {
			return
		}
		r := atkTextRect{X: -1, Y: -1}
		if an := node(obj); an != nil && an.n.Text != nil {
			a, b := span(start, end, an.n.Text.Length)
			r = an.textRect(a, b, coords)
		}
		*(*atkTextRect)(unsafe.Pointer(slot(rect, 0))) = r
	})
	bounded := cb(func(obj, rect ptr, coords, xclip, yclip int32) ptr {
		an := node(obj)
		if an == nil || an.n.Text == nil || rect == 0 {
			return 0
		}
		want := *(*atkTextRect)(unsafe.Pointer(slot(rect, 0)))
		var ranges []atkTextRange
		for _, v := range an.textQuery(platform.AccessTextQuery{Kind: platform.TextVisibleRanges}).Ranges {
			// Filter by characters so a narrow rectangle does not return
			// the entire visible document.
			start := -1
			flush := func(end int) {
				if start >= 0 {
					r := an.textRect(start, end, coords)
					s := an.textQuery(platform.AccessTextQuery{Kind: platform.TextSlice, Start: start, End: end}).Text
					ranges = append(ranges, atkTextRange{Bounds: r, Start: int32(start), End: int32(end), Content: cString(s)})
					start = -1
				}
			}
			for i := v.Start; i < v.End; {
				u := an.textQuery(platform.AccessTextQuery{Kind: platform.TextUnitRange, Unit: platform.TextCharacter, Start: i})
				if u.End <= i {
					break
				}
				r := an.textRect(u.Start, u.End, coords)
				inside := r.W > 0 && r.H > 0 && r.X+r.W > want.X && r.X < want.X+want.W && r.Y+r.H > want.Y && r.Y < want.Y+want.H
				if xclip == 1 || xclip == 3 {
					inside = inside && r.X >= want.X
				}
				if xclip == 2 || xclip == 3 {
					inside = inside && r.X+r.W <= want.X+want.W
				}
				if yclip == 1 || yclip == 3 {
					inside = inside && r.Y >= want.Y
				}
				if yclip == 2 || yclip == 3 {
					inside = inside && r.Y+r.H <= want.Y+want.H
				}
				if inside {
					if start < 0 {
						start = u.Start
					}
				} else {
					flush(i)
				}
				i = u.End
			}
			flush(v.End)
		}
		arr := gMalloc0(uintptr(len(ranges)+1) * unsafe.Sizeof(ptr(0)))
		for i, r := range ranges {
			p := gMalloc0(unsafe.Sizeof(r))
			*(*atkTextRange)(unsafe.Pointer(slot(p, 0))) = r
			*slot(arr, uintptr(i)*unsafe.Sizeof(ptr(0))) = p
		}
		return arr
	})
	fns := map[int]ptr{0: getText, 2: atOffset, 3: charAt, 5: caret, 8: charExtents, 9: count, 10: point, 11: selections, 12: selection, 13: add, 14: remove, 15: set, 16: setCaret, 21: rangeExtents, 22: bounded, 23: granularity}
	if atkGetMajorVersion() > 2 || atkGetMinorVersion() >= 32 {
		fns[24] = cb(func(obj ptr, start, end, how int32) bool {
			an := node(obj)
			if an == nil || an.n.Text == nil || an.n.States&platform.AccessDisabled != 0 {
				return false
			}
			align := 1
			if how == 1 || how == 3 || how == 5 {
				align = -1
			}
			an.tree.s.send(platform.SurfaceEvent{Kind: platform.AccessAction, Action: platform.AccessScrollText, ID: an.n.ID, From: int(start), To: int(end), Caret: align})
			return true
		})
	}
	return fns
}

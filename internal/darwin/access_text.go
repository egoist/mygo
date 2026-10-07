//go:build darwin

package darwin

import (
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/internal/platform"
)

var accessTextAttributeNames = []string{"AXNumberOfCharacters", "AXSelectedText", "AXSelectedTextRange", "AXSelectedTextRanges", "AXVisibleCharacterRange", "AXInsertionPointLineNumber"}
var accessTextParameterNames = []string{"AXStringForRange", "AXRangeForIndex", "AXRangeForPosition", "AXBoundsForRange", "AXLineForIndex", "AXRangeForLine"}

func rangeValue(r nsRange) id {
	return send(class("NSValue"), "valueWithRange:", uintptr(r.Location), uintptr(r.Length))
}
func rangeOfValue(v id) nsRange {
	a, b, _ := purego.SyscallN(msgSendAddr, uintptr(v), uintptr(sel("rangeValue")))
	return nsRange{Location: uint(a), Length: uint(b)}
}

func (el *accessElement) textAttribute(name string) (id, bool) {
	if el.node.Text == nil {
		return 0, false
	}
	switch name {
	case "AXNumberOfCharacters":
		return nsNumberInt(el.textQuery(platform.AccessTextQuery{Kind: platform.TextToUTF16, Start: el.node.Text.Length}).Start), true
	case "AXSelectedText":
		return el.selectedText(), true
	case "AXSelectedTextRange":
		return rangeValue(el.selectionRange()), true
	case "AXSelectedTextRanges":
		return nsArray(rangeValue(el.selectionRange())), true
	case "AXVisibleCharacterRange":
		rs := el.textQuery(platform.AccessTextQuery{Kind: platform.TextVisibleRanges}).Ranges
		if len(rs) > 0 {
			return rangeValue(el.nativeRange(rs[0].Start, rs[len(rs)-1].End)), true
		}
		return rangeValue(nsRange{}), true
	case "AXInsertionPointLineNumber":
		return nsNumberInt(el.textQuery(platform.AccessTextQuery{Kind: platform.TextLineNumber, Start: el.node.Text.Caret}).Start), true
	}
	return 0, false
}

func (el *accessElement) textQuery(q platform.AccessTextQuery) platform.AccessTextResult {
	return el.node.Text.Ask(q)
}

func (el *accessElement) runeRange(r nsRange) (int, int) {
	// NSNotFound and overflowing ranges are invalid, not document-wide.
	if r.Location == ^uint(0) || r.Length > ^uint(0)-r.Location {
		return 0, 0
	}
	a := el.textQuery(platform.AccessTextQuery{Kind: platform.TextFromUTF16, Start: int(r.Location)}).Start
	b := el.textQuery(platform.AccessTextQuery{Kind: platform.TextFromUTF16, Start: int(r.Location + r.Length)}).Start
	return a, max(a, b)
}

func (el *accessElement) nativeRange(a, b int) nsRange {
	start := el.textQuery(platform.AccessTextQuery{Kind: platform.TextToUTF16, Start: a}).Start
	end := el.textQuery(platform.AccessTextQuery{Kind: platform.TextToUTF16, Start: b}).Start
	return nsRange{Location: uint(start), Length: uint(max(0, end-start))}
}

func (el *accessElement) selectionRange() nsRange {
	return el.nativeRange(el.node.SelStart, el.node.SelEnd)
}

func (el *accessElement) selectedText() id {
	if el.node.Text == nil {
		return 0
	}
	return nsString(el.textQuery(platform.AccessTextQuery{Kind: platform.TextSlice, Start: el.node.SelStart, End: el.node.SelEnd}).Text)
}

func (el *accessElement) selectRange(r nsRange) {
	if el.s.updating || el.node.Text == nil || !el.node.Text.Selectable {
		return
	}
	a, b := el.runeRange(r)
	el.s.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: el.node.ID, Action: platform.AccessSetSelection, From: a, To: b})
}

func accessTextMethods() []objc.MethodDef {
	get := func(obj id) *accessElement { return theBackend.accessElementOf(obj) }
	return []objc.MethodDef{
		method("accessibilityParameterizedAttributeNames", func(self id, _ objc.SEL) id {
			var names []id
			if el := get(self); el != nil && el.node.Text != nil {
				for _, s := range accessTextParameterNames {
					names = append(names, nsString(s))
				}
			}
			return nsArray(names...)
		}),
		method("accessibilityAttributeValue:forParameter:", func(self id, _ objc.SEL, attr, param id) id {
			el := get(self)
			if el == nil || el.node.Text == nil {
				return 0
			}
			switch stringOf(attr) {
			case "AXStringForRange":
				r := rangeOfValue(param)
				return send(self, "accessibilityStringForRange:", uintptr(r.Location), uintptr(r.Length))
			case "AXRangeForIndex":
				r1, r2, _ := purego.SyscallN(msgSendAddr, uintptr(self), uintptr(sel("accessibilityRangeForIndex:")), uintptr(sendInt(param, "integerValue")))
				return rangeValue(nsRange{Location: uint(r1), Length: uint(r2)})
			case "AXLineForIndex":
				return nsNumberInt(sendInt(self, "accessibilityLineForIndex:", uintptr(sendInt(param, "integerValue"))))
			case "AXRangeForLine":
				r1, r2, _ := purego.SyscallN(msgSendAddr, uintptr(self), uintptr(sel("accessibilityRangeForLine:")), uintptr(sendInt(param, "integerValue")))
				return rangeValue(nsRange{Location: uint(r1), Length: uint(r2)})
			case "AXBoundsForRange":
				r := rangeOfValue(param)
				f := accessTextFrame(el, r)
				return msgAccessRectValue(class("NSValue"), sel("valueWithRect:"), f)
			case "AXRangeForPosition":
				p := msgPoint(param, sel("pointValue"))
				r := accessTextPosition(el, p)
				return rangeValue(r)
			}
			return 0
		}),
		method("accessibilityNumberOfCharacters", func(self id, _ objc.SEL) int {
			if el := get(self); el != nil && el.node.Text != nil {
				return el.textQuery(platform.AccessTextQuery{Kind: platform.TextToUTF16, Start: el.node.Text.Length}).Start
			}
			return 0
		}),
		method("accessibilitySelectedTextRange", func(self id, _ objc.SEL) nsRange {
			if el := get(self); el != nil {
				return el.selectionRange()
			}
			return nsRange{}
		}),
		method("accessibilitySelectedTextRanges", func(self id, _ objc.SEL) id {
			if el := get(self); el != nil && el.node.Text != nil {
				r := el.selectionRange()
				return nsArray(send(class("NSValue"), "valueWithRange:", uintptr(r.Location), uintptr(r.Length)))
			}
			return nsArray()
		}),
		method("setAccessibilitySelectedTextRange:", func(self id, _ objc.SEL, r nsRange) {
			if el := get(self); el != nil {
				el.selectRange(r)
			}
		}),
		method("setAccessibilitySelectedTextRanges:", func(self id, _ objc.SEL, rs id) {
			if el := get(self); el != nil && sendInt(rs, "count") == 1 {
				v := send(rs, "objectAtIndex:", 0)
				r1, r2, _ := purego.SyscallN(msgSendAddr, uintptr(v), uintptr(sel("rangeValue")))
				el.selectRange(nsRange{Location: uint(r1), Length: uint(r2)})
			}
		}),
		method("accessibilitySelectedText", func(self id, _ objc.SEL) id {
			if el := get(self); el != nil {
				return el.selectedText()
			}
			return 0
		}),
		method("accessibilityVisibleCharacterRange", func(self id, _ objc.SEL) nsRange {
			if el := get(self); el != nil {
				rs := el.textQuery(platform.AccessTextQuery{Kind: platform.TextVisibleRanges}).Ranges
				if len(rs) > 0 {
					return el.nativeRange(rs[0].Start, rs[len(rs)-1].End)
				}
			}
			return nsRange{}
		}),
		method("accessibilityInsertionPointLineNumber", func(self id, _ objc.SEL) int {
			if el := get(self); el != nil && el.node.Text != nil {
				return el.textQuery(platform.AccessTextQuery{Kind: platform.TextLineNumber, Start: el.node.Text.Caret}).Start
			}
			return 0
		}),
		method("accessibilityStringForRange:", func(self id, _ objc.SEL, r nsRange) id {
			if el := get(self); el != nil && el.node.Text != nil {
				a, b := el.runeRange(r)
				return nsString(el.textQuery(platform.AccessTextQuery{Kind: platform.TextSlice, Start: a, End: b}).Text)
			}
			return 0
		}),
		method("accessibilityRangeForIndex:", func(self id, _ objc.SEL, i int) nsRange {
			if el := get(self); el != nil && el.node.Text != nil {
				at := el.textQuery(platform.AccessTextQuery{Kind: platform.TextFromUTF16, Start: i}).Start
				r := el.textQuery(platform.AccessTextQuery{Kind: platform.TextUnitRange, Unit: platform.TextCharacter, Start: at})
				return el.nativeRange(r.Start, r.End)
			}
			return nsRange{}
		}),
		method("accessibilityLineForIndex:", func(self id, _ objc.SEL, i int) int {
			if el := get(self); el != nil && el.node.Text != nil {
				at := el.textQuery(platform.AccessTextQuery{Kind: platform.TextFromUTF16, Start: i}).Start
				return el.textQuery(platform.AccessTextQuery{Kind: platform.TextLineNumber, Start: at}).Start
			}
			return 0
		}),
		method("accessibilityRangeForLine:", func(self id, _ objc.SEL, i int) nsRange {
			if el := get(self); el != nil && el.node.Text != nil {
				r := el.textQuery(platform.AccessTextQuery{Kind: platform.TextLineRange, Start: i})
				if r.OK {
					return el.nativeRange(r.Start, r.End)
				}
			}
			return nsRange{Location: ^uint(0)}
		}),
		method("accessibilityRangeForPosition:", func(self id, _ objc.SEL, p NSPoint) nsRange {
			if el := get(self); el != nil && el.node.Text != nil {
				return accessTextPosition(el, p)
			}
			return nsRange{}
		}),
		method("accessibilityFrameForRange:", func(self id, _ objc.SEL, r nsRange) NSRect {
			if el := get(self); el != nil && el.node.Text != nil {
				return accessTextFrame(el, r)
			}
			return NSRect{}
		}),
	}
}

func accessTextPosition(el *accessElement, p NSPoint) nsRange {
	loadAccess()
	w := msgPointToPoint(el.s.w.win, sel("convertPointFromScreen:"), p)
	v := msgPointFromView(el.s.view, sel("convertPoint:fromView:"), w, 0)
	r := el.textQuery(platform.AccessTextQuery{Kind: platform.TextOffsetAtPoint, X: v.X, Y: v.Y})
	return el.nativeRange(r.Start, r.Start)
}

func accessTextFrame(el *accessElement, r nsRange) NSRect {
	a, b := el.runeRange(r)
	rs := el.textQuery(platform.AccessTextQuery{Kind: platform.TextRangeBounds, Start: a, End: b}).Rects
	if len(rs) == 0 {
		return NSRect{}
	}
	v := rs[0]
	for _, r := range rs[1:] {
		right, bottom := max(v.X+v.W, r.X+r.W), max(v.Y+v.H, r.Y+r.H)
		v.X, v.Y = min(v.X, r.X), min(v.Y, r.Y)
		v.W, v.H = right-v.X, bottom-v.Y
	}
	return el.s.screenRect(v)
}

//go:build windows && (amd64 || arm64)

package windows

import (
	"math"
	"slices"
	"strings"
	"syscall"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

var (
	uiaTextPointCallback                uintptr
	uiaRangeVtbl                        []uintptr
	uiaRanges                           = map[uintptr]*uiaTextRange{}
	iidTextRange                        = guid("5347ad7b-c355-46f8-aff5-909033582f63")
	procUiaGetReservedNotSupportedValue = uiaCore.NewProc("UiaGetReservedNotSupportedValue")
	procSysStringLen                    = oleaut32.NewProc("SysStringLen")
)

func uiaTextPointEntry() uintptr
func uiaTextPointThunk()

func bstrText(p uintptr) string {
	if p == 0 {
		return ""
	}
	n, _, _ := procSysStringLen.Call(p)
	return string(utf16.Decode(unsafe.Slice((*uint16)(native(p)), int(n))))
}

// A range owns an element reference until COM releases it. Its offsets
// follow edits; a removed element makes every outstanding range defunct.
type uiaTextRange struct {
	vtbl       *uintptr
	refs       int32
	e          *uiaElement
	start, end int
}

func (r *uiaTextRange) ptr() uintptr { return uintptr(unsafe.Pointer(r)) }
func (e *uiaElement) textQuery(q platform.AccessTextQuery) platform.AccessTextResult {
	return e.n.Text.Ask(q)
}

func rangeOut(p uintptr, e *uiaElement, start, end int) uintptr {
	*(*uintptr)(native(p)) = 0
	if e == nil {
		return sOK
	}
	r := &uiaTextRange{vtbl: &uiaRangeVtbl[0], refs: 1, e: e, start: start, end: end}
	e.addRef()
	if e.ranges == nil {
		e.ranges = map[*uiaTextRange]bool{}
	}
	e.ranges[r] = true
	uiaRanges[r.ptr()] = r
	*(*uintptr)(native(p)) = r.ptr()
	return sOK
}

func liveTextRange(this uintptr) (*uiaTextRange, uintptr) {
	r := uiaRanges[this]
	if r == nil || r.e.dead || r.e.n.Text == nil || r.e.n.States&platform.AccessPassword != 0 {
		return nil, uiaElementNotAvailable
	}
	r.start = max(0, min(r.start, r.e.n.Text.Length))
	r.end = max(r.start, min(r.end, r.e.n.Text.Length))
	return r, sOK
}

func (e *uiaElement) followTextRanges(before, after platform.AccessNode) {
	if before.Text == nil || after.Text == nil || before.Text.Content == after.Text.Content {
		return
	}
	a, b := before.Text.Content, after.Text.Content
	prefix := 0
	for prefix < len(a) && prefix < len(b) {
		ar, as := utf8.DecodeRuneInString(a[prefix:])
		br, bs := utf8.DecodeRuneInString(b[prefix:])
		if ar != br || as != bs {
			break
		}
		prefix += as
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix {
		ar, as := utf8.DecodeLastRuneInString(a[:len(a)-suffix])
		br, bs := utf8.DecodeLastRuneInString(b[:len(b)-suffix])
		if ar != br || as != bs {
			break
		}
		suffix += as
	}
	start := utf8.RuneCountInString(a[:prefix])
	oldEnd := before.Text.Length - utf8.RuneCountInString(a[len(a)-suffix:])
	newEnd := after.Text.Length - utf8.RuneCountInString(b[len(b)-suffix:])
	adjust := func(i int) int {
		if i <= start {
			return i
		}
		if i >= oldEnd {
			return i + newEnd - oldEnd
		}
		return newEnd
	}
	for r := range e.ranges {
		r.start, r.end = adjust(r.start), adjust(r.end)
	}
}

func uiaUnit(unit uintptr) (platform.AccessTextUnit, bool) {
	switch unit {
	case 0:
		return platform.TextCharacter, true
	case 1, 2:
		return platform.TextWord, true
	case 3:
		return platform.TextLine, true
	case 4:
		return platform.TextParagraph, true
	case 5, 6:
		return platform.TextDocument, true
	}
	return 0, false
}

func unknownArray(p uintptr, ptrs []uintptr) uintptr {
	sa, _, _ := procSafeArrayCreateVector.Call(vtUnknown, 0, uintptr(len(ptrs)))
	if sa == 0 {
		return 0x8007000e
	}
	for i, v := range ptrs {
		index := int32(i)
		procSafeArrayPutElement.Call(sa, uintptr(unsafe.Pointer(&index)), v)
	}
	*(*uintptr)(native(p)) = sa
	return sOK
}

func rangeArray(p uintptr, e *uiaElement, rs []platform.AccessTextRange) uintptr {
	var ptrs []uintptr
	for _, r := range rs {
		var v uintptr
		rangeOut(uintptr(unsafe.Pointer(&v)), e, r.Start, r.End)
		ptrs = append(ptrs, v)
	}
	hr := unknownArray(p, ptrs)
	for _, v := range ptrs {
		release(v)
	}
	return hr
}

func (r *uiaTextRange) selectText(start, end int) uintptr {
	e := r.e
	if e.n.States&platform.AccessDisabled != 0 {
		return uiaElementNotEnabled
	}
	if !e.n.Text.Selectable {
		return uiaInvalidOperation
	}
	e.tree.s.send(platform.SurfaceEvent{Kind: platform.AccessAction, Action: platform.AccessSetSelection, ID: e.n.ID, From: start, To: end})
	return sOK
}

func initUIAText(unknown []uintptr) {
	cb := syscall.NewCallback
	live := func(this uintptr) (*uiaElement, uintptr) {
		e := uiaOf(this)
		if e.dead || e.n.Text == nil {
			return nil, uiaElementNotAvailable
		}
		return e, sOK
	}
	provider := append(slices.Clone(unknown),
		cb(func(this, p uintptr) uintptr { // GetSelection
			e, hr := live(this)
			*(*uintptr)(native(p)) = 0
			if e == nil {
				return hr
			}
			var rs []platform.AccessTextRange
			if e.n.Text.Selectable {
				rs = append(rs, platform.AccessTextRange{Start: e.n.SelStart, End: e.n.SelEnd})
			}
			return rangeArray(p, e, rs)
		}),
		cb(func(this, p uintptr) uintptr {
			e, hr := live(this)
			*(*uintptr)(native(p)) = 0
			if e == nil {
				return hr
			}
			return rangeArray(p, e, e.textQuery(platform.AccessTextQuery{Kind: platform.TextVisibleRanges}).Ranges)
		}),
		cb(func(this, child, p uintptr) uintptr { // RangeFromChild: inline links carry their rune range
			e, hr := live(this)
			*(*uintptr)(native(p)) = 0
			if e == nil {
				return hr
			}
			for _, c := range e.children {
				if c.ptr(ifaceSimple) == child && c.n.TextEnd > c.n.TextStart {
					return rangeOut(p, e, c.n.TextStart, c.n.TextEnd)
				}
			}
			return 0x80070057
		}),
		uiaTextPointEntry(),
		cb(func(this, p uintptr) uintptr {
			e, hr := live(this)
			*(*uintptr)(native(p)) = 0
			if e == nil {
				return hr
			}
			return rangeOut(p, e, 0, e.n.Text.Length)
		}),
		cb(func(this, p uintptr) uintptr {
			e, hr := live(this)
			*(*int32)(native(p)) = 0
			if e == nil {
				return hr
			}
			if e.n.Text.Selectable {
				*(*int32)(native(p)) = 1
			}
			return sOK
		}),
	)
	uiaTextPointCallback = cb(func(this, xb, yb, p uintptr) uintptr {
		e, hr := live(this)
		*(*uintptr)(native(p)) = 0
		if e == nil {
			return hr
		}
		var origin point
		procClientToScreen.Call(e.tree.s.hwnd, uintptr(unsafe.Pointer(&origin)))
		scale := float64(e.tree.s.dpi()) / 96
		x, y := math.Float64frombits(uint64(xb)), math.Float64frombits(uint64(yb))
		at := e.textQuery(platform.AccessTextQuery{Kind: platform.TextOffsetAtPoint, X: (x - float64(origin.X)) / scale, Y: (y - float64(origin.Y)) / scale}).Start
		return rangeOut(p, e, at, at)
	})
	uiaVtbls[ifaceText] = provider
	uiaVtbls[ifaceText2] = append(slices.Clone(provider),
		cb(func(this, annotation, p uintptr) uintptr { *(*uintptr)(native(p)) = 0; return uiaInvalidOperation }),
		cb(func(this, active, p uintptr) uintptr {
			e, hr := live(this)
			*(*uintptr)(native(p)) = 0
			setBool(active, false)
			if e == nil {
				return hr
			}
			setBool(active, e.tree.focus == e.n.ID)
			return rangeOut(p, e, e.n.Text.Caret, e.n.Text.Caret)
		}),
	)
	getAttr := func(r *uiaTextRange, attr uintptr, v *variant) uintptr {
		*v = variant{}
		if attr == 40015 {
			*v = boolVariant(r.e.n.States&platform.AccessReadOnly != 0)
			return sOK
		}
		var reserved uintptr
		hr, _, _ := procUiaGetReservedNotSupportedValue.Call(uintptr(unsafe.Pointer(&reserved)))
		if failed(hr) {
			return hr
		}
		*v = variant{VT: vtUnknown, Val: uint64(reserved)}
		return sOK
	}
	target := func(r *uiaTextRange, p uintptr) (*uiaTextRange, bool) {
		t, hr := liveTextRange(p)
		return t, hr == sOK && t.e == r.e
	}
	endpoint := func(r *uiaTextRange, e uintptr) int {
		if e == 0 {
			return r.start
		}
		return r.end
	}
	putEndpoint := func(r *uiaTextRange, e uintptr, i int) {
		if e == 0 {
			r.start = i
			r.end = max(r.end, i)
		} else {
			r.end = i
			r.start = min(r.start, i)
		}
	}
	uiaRangeVtbl = []uintptr{
		cb(func(this, iid, p uintptr) uintptr {
			*(*uintptr)(native(p)) = 0
			if *(*GUID)(native(iid)) != iidIUnknown && *(*GUID)(native(iid)) != iidTextRange {
				return eNoInterface
			}
			r := uiaRanges[this]
			if r == nil {
				return uiaElementNotAvailable
			}
			r.refs++
			*(*uintptr)(native(p)) = this
			return sOK
		}),
		cb(func(this uintptr) uintptr {
			r := uiaRanges[this]
			if r == nil {
				return 0
			}
			r.refs++
			return uintptr(r.refs)
		}),
		cb(func(this uintptr) uintptr {
			r := uiaRanges[this]
			if r == nil {
				return 0
			}
			r.refs--
			if r.refs == 0 {
				delete(uiaRanges, this)
				delete(r.e.ranges, r)
				r.e.release()
			}
			return uintptr(r.refs)
		}),
		cb(func(this, p uintptr) uintptr {
			r, hr := liveTextRange(this)
			*(*uintptr)(native(p)) = 0
			if r == nil {
				return hr
			}
			return rangeOut(p, r.e, r.start, r.end)
		}),
		cb(func(this, other, p uintptr) uintptr {
			r, hr := liveTextRange(this)
			setBool(p, false)
			if r == nil {
				return hr
			}
			t, ok := target(r, other)
			if !ok {
				return 0x80070057
			}
			setBool(p, r.start == t.start && r.end == t.end)
			return sOK
		}),
		cb(func(this, ep, other, tep, p uintptr) uintptr {
			r, hr := liveTextRange(this)
			*(*int32)(native(p)) = 0
			if r == nil {
				return hr
			}
			t, ok := target(r, other)
			if !ok || ep > 1 || tep > 1 {
				return 0x80070057
			}
			*(*int32)(native(p)) = int32(endpoint(r, ep) - endpoint(t, tep))
			return sOK
		}),
		cb(func(this, unit uintptr) uintptr {
			r, hr := liveTextRange(this)
			if r == nil {
				return hr
			}
			u, ok := uiaUnit(unit)
			if !ok {
				return 0x80070057
			}
			at := r.start
			if u == platform.TextCharacter && at > 0 && at == r.e.n.Text.Length {
				at--
			}
			q := r.e.textQuery(platform.AccessTextQuery{Kind: platform.TextUnitRange, Unit: u, Start: at})
			r.start, r.end = q.Start, q.End
			return sOK
		}),
		cb(func(this, attr, value, backward, p uintptr) uintptr { // FindAttribute, VARIANT by reference on both supported ABIs
			r, hr := liveTextRange(this)
			*(*uintptr)(native(p)) = 0
			if r == nil {
				return hr
			}
			v := (*variant)(native(value))
			if attr == 40015 && v.VT == vtBool && (v.Val != 0) == (r.e.n.States&platform.AccessReadOnly != 0) {
				return rangeOut(p, r.e, r.start, r.end)
			}
			return sOK
		}),
		cb(func(this, text, backward, ignoreCase, p uintptr) uintptr {
			r, hr := liveTextRange(this)
			*(*uintptr)(native(p)) = 0
			if r == nil {
				return hr
			}
			needle := bstrText(text)
			if needle == "" {
				return 0x80070057
			}
			s := r.e.textQuery(platform.AccessTextQuery{Kind: platform.TextSlice, Start: r.start, End: r.end}).Text
			// Match at rune boundaries, preserving offsets even when case
			// folding changes byte lengths (e.g. K and the Kelvin sign).
			n := utf8.RuneCountInString(needle)
			best := -1
			rn := 0
			for at := range s {
				end := at
				for range n {
					if end >= len(s) {
						break
					}
					_, size := utf8.DecodeRuneInString(s[end:])
					end += size
				}
				part := s[at:end]
				match := part == needle
				if ignoreCase != 0 {
					match = strings.EqualFold(part, needle)
				}
				if match {
					best = rn
					if backward == 0 {
						break
					}
				}
				rn++
			}
			if best >= 0 {
				return rangeOut(p, r.e, r.start+best, r.start+best+n)
			}
			return sOK
		}),
		cb(func(this, attr, p uintptr) uintptr {
			r, hr := liveTextRange(this)
			*(*variant)(native(p)) = variant{}
			if r == nil {
				return hr
			}
			return getAttr(r, attr, (*variant)(native(p)))
		}),
		cb(func(this, p uintptr) uintptr {
			r, hr := liveTextRange(this)
			*(*uintptr)(native(p)) = 0
			if r == nil {
				return hr
			}
			rs := r.e.textQuery(platform.AccessTextQuery{Kind: platform.TextRangeBounds, Start: r.start, End: r.end}).Rects
			var origin point
			procClientToScreen.Call(r.e.tree.s.hwnd, uintptr(unsafe.Pointer(&origin)))
			scale := float64(r.e.tree.s.dpi()) / 96
			sa, _, _ := procSafeArrayCreateVector.Call(vtR8, 0, uintptr(len(rs)*4))
			for i, b := range rs {
				vals := []float64{float64(origin.X) + b.X*scale, float64(origin.Y) + b.Y*scale, b.W * scale, b.H * scale}
				for j, v := range vals {
					index := int32(i*4 + j)
					procSafeArrayPutElement.Call(sa, uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&v)))
				}
			}
			*(*uintptr)(native(p)) = sa
			return sOK
		}),
		cb(func(this, p uintptr) uintptr {
			r, hr := liveTextRange(this)
			*(*uintptr)(native(p)) = 0
			if r == nil {
				return hr
			}
			return out(p, r.e, ifaceSimple)
		}),
		cb(func(this, maxLength, p uintptr) uintptr {
			r, hr := liveTextRange(this)
			*(*uintptr)(native(p)) = 0
			if r == nil {
				return hr
			}
			n := int(int32(maxLength))
			if n < -1 {
				return 0x80070057
			}
			end := r.end
			if n >= 0 {
				units := r.e.textQuery(platform.AccessTextQuery{Kind: platform.TextToUTF16, Start: r.start}).Start
				end = min(end, r.e.textQuery(platform.AccessTextQuery{Kind: platform.TextFromUTF16, Start: units + n}).Start)
			}
			*(*uintptr)(native(p)) = bstr(r.e.textQuery(platform.AccessTextQuery{Kind: platform.TextSlice, Start: r.start, End: end}).Text)
			return sOK
		}),
		cb(func(this, unit, count, p uintptr) uintptr { // Move: normalize nondegenerate ranges to one unit
			r, hr := liveTextRange(this)
			*(*int32)(native(p)) = 0
			if r == nil {
				return hr
			}
			u, ok := uiaUnit(unit)
			if !ok {
				return 0x80070057
			}
			n := int(int32(count))
			if n == 0 {
				return sOK
			}
			degenerate := r.start == r.end
			start := r.start
			if !degenerate {
				q := r.e.textQuery(platform.AccessTextQuery{Kind: platform.TextUnitRange, Unit: u, Start: start})
				start = q.Start
			}
			q := r.e.textQuery(platform.AccessTextQuery{Kind: platform.TextMoveOffset, Unit: u, Start: start, Count: n})
			if !degenerate && q.Start == r.e.n.Text.Length && q.Count > 0 {
				q.Count--
				q.Start = start
				if q.Count > 0 {
					q = r.e.textQuery(platform.AccessTextQuery{Kind: platform.TextMoveOffset, Unit: u, Start: start, Count: q.Count})
				}
			}
			r.start, r.end = q.Start, q.Start
			if !degenerate {
				v := r.e.textQuery(platform.AccessTextQuery{Kind: platform.TextUnitRange, Unit: u, Start: r.start})
				r.start, r.end = v.Start, v.End
			}
			*(*int32)(native(p)) = int32(q.Count)
			return sOK
		}),
		cb(func(this, ep, unit, count, p uintptr) uintptr {
			r, hr := liveTextRange(this)
			*(*int32)(native(p)) = 0
			if r == nil {
				return hr
			}
			u, ok := uiaUnit(unit)
			if !ok || ep > 1 {
				return 0x80070057
			}
			q := r.e.textQuery(platform.AccessTextQuery{Kind: platform.TextMoveOffset, Unit: u, Start: endpoint(r, ep), Count: int(int32(count))})
			putEndpoint(r, ep, q.Start)
			*(*int32)(native(p)) = int32(q.Count)
			return sOK
		}),
		cb(func(this, ep, other, tep uintptr) uintptr {
			r, hr := liveTextRange(this)
			if r == nil {
				return hr
			}
			t, ok := target(r, other)
			if !ok || ep > 1 || tep > 1 {
				return 0x80070057
			}
			putEndpoint(r, ep, endpoint(t, tep))
			return sOK
		}),
		cb(func(this uintptr) uintptr {
			r, hr := liveTextRange(this)
			if r == nil {
				return hr
			}
			return r.selectText(r.start, r.end)
		}),
		cb(func(this uintptr) uintptr {
			r, hr := liveTextRange(this)
			if r == nil {
				return hr
			}
			if r.e.n.SelStart != r.e.n.SelEnd && (r.e.n.SelStart != r.start || r.e.n.SelEnd != r.end) {
				return uiaInvalidOperation
			}
			return r.selectText(r.start, r.end)
		}),
		cb(func(this uintptr) uintptr {
			r, hr := liveTextRange(this)
			if r == nil {
				return hr
			}
			if r.start == r.end {
				return r.selectText(r.start, r.end)
			}
			if r.e.n.SelStart != r.start || r.e.n.SelEnd != r.end {
				return uiaInvalidOperation
			}
			return r.selectText(r.e.n.Text.Caret, r.e.n.Text.Caret)
		}),
		cb(func(this, align uintptr) uintptr {
			r, hr := liveTextRange(this)
			if r == nil {
				return hr
			}
			if r.e.n.States&platform.AccessDisabled != 0 {
				return uiaElementNotEnabled
			}
			a := 1
			if align == 0 {
				a = -1
			}
			r.e.tree.s.send(platform.SurfaceEvent{Kind: platform.AccessAction, Action: platform.AccessScrollText, ID: r.e.n.ID, From: r.start, To: r.end, Caret: a})
			return sOK
		}),
		cb(func(this, p uintptr) uintptr {
			r, hr := liveTextRange(this)
			*(*uintptr)(native(p)) = 0
			if r == nil {
				return hr
			}
			var ptrs []uintptr
			for _, c := range r.e.children {
				if c.n.TextEnd > c.n.TextStart && c.n.TextStart < r.end && c.n.TextEnd > r.start {
					ptrs = append(ptrs, c.ptr(ifaceSimple))
				}
			}
			return unknownArray(p, ptrs)
		}),
	}
}

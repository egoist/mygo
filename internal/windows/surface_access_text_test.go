//go:build windows && (amd64 || arm64)

package windows

import (
	"testing"
	"unicode/utf16"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

func TestTextRangeCOMProvider(t *testing.T) {
	uiaOnce.Do(initUIA)
	text := "A😀e\u0301\x00K"
	runes := []rune(text)
	doc := &platform.AccessText{Content: text, Length: len(runes), Selectable: true}
	// A small contract fixture lets this test exercise real COM vtables,
	// BSTRs, VARIANTs and SAFEARRAY ownership without a native window.
	doc.Query = func(q platform.AccessTextQuery) platform.AccessTextResult {
		o := platform.AccessTextResult{OK: true, Start: q.Start, End: q.End}
		switch q.Kind {
		case platform.TextSlice:
			o.Text = string(runes[q.Start:q.End])
		case platform.TextToUTF16:
			o.Start = len(utf16.Encode(runes[:q.Start]))
		case platform.TextFromUTF16:
			o.Start = 0
			units := 0
			for _, r := range runes {
				n := 1
				if r > 0xffff {
					n++
				}
				if units+n > q.Start {
					break
				}
				units += n
				o.Start++
			}
		case platform.TextUnitRange:
			if q.Start < 1 {
				o.Start, o.End = 0, 1
			} else if q.Start < 2 {
				o.Start, o.End = 1, 2
			} else {
				o.Start, o.End = 2, 4
			}
		case platform.TextMoveOffset:
			if q.Start == 0 && q.Count == 1 {
				o.Start, o.Count = 1, 1
			} else {
				o.Start, o.Count = q.Start, 0
			}
		}
		return o
	}
	e := newUIAElement(&uiaTree{})
	e.n = platform.AccessNode{Text: doc, States: platform.AccessReadOnly, SelStart: 1, SelEnd: 2}
	defer e.release()
	provider := queryInterface(e.ptr(ifaceSimple), &uiaIIDs[ifaceText2])
	if provider == 0 {
		t.Fatal("missing Text2 interface")
	}
	defer release(provider)
	var r uintptr
	if hr := comCall(provider, 7, uintptr(unsafe.Pointer(&r))); hr != sOK || r == 0 {
		t.Fatalf("DocumentRange: %#x", hr)
	}
	defer release(r)
	var b uintptr
	if hr := comCall(r, 12, ^uintptr(0), uintptr(unsafe.Pointer(&b))); hr != sOK {
		t.Fatalf("GetText %#x", hr)
	}
	units, _, _ := procSysStringLen.Call(b)
	if got := string(utf16.Decode(unsafe.Slice((*uint16)(native(b)), int(units)))); got != text {
		t.Fatalf("BSTR %q", got)
	}
	procSysFreeString.Call(b)
	var clone uintptr
	comCall(r, 3, uintptr(unsafe.Pointer(&clone)))
	defer release(clone)
	comCall(clone, 6, 0) // ExpandToEnclosingUnit(Character)
	var moved int32
	comCall(clone, 13, 0, 1, uintptr(unsafe.Pointer(&moved)))
	if moved != 1 || testRangeText(clone) != "😀" {
		t.Fatalf("Move count %d, text %q", moved, testRangeText(clone))
	}
	comCall(clone, 12, 1, uintptr(unsafe.Pointer(&b)))
	if units, _, _ := procSysStringLen.Call(b); units != 0 {
		t.Fatal("GetText split a surrogate pair")
	}
	procSysFreeString.Call(b)
	var same int32
	comCall(r, 4, clone, uintptr(unsafe.Pointer(&same)))
	if same != 0 {
		t.Fatal("Compare returned true for unequal ranges")
	}
	var attr variant
	comCall(r, 9, 40015, uintptr(unsafe.Pointer(&attr)))
	if attr.VT != vtBool || attr.Val == 0 {
		t.Fatalf("IsReadOnly: %+v", attr)
	}
	comCall(r, 9, 40005, uintptr(unsafe.Pointer(&attr)))
	if attr.VT != vtUnknown || attr.Val == 0 {
		t.Fatalf("unsupported attribute: %+v", attr)
	}
	procVariantClear.Call(uintptr(unsafe.Pointer(&attr)))
	var selections uintptr
	comCall(provider, 3, uintptr(unsafe.Pointer(&selections)))
	sel := testArrayFirst(selections)
	if testRangeText(sel) != "😀" {
		t.Fatalf("selection %q", testRangeText(sel))
	}
	release(sel)
	procSafeArrayDestroy.Call(selections)
	e.n.States |= platform.AccessDisabled
	if hr := comCall(clone, 16); hr != uiaElementNotEnabled {
		t.Fatalf("disabled Select %#x", hr)
	}
	e.dead = true
	e.n.Text = nil
	if hr := comCall(r, 12, ^uintptr(0), uintptr(unsafe.Pointer(&b))); hr != uiaElementNotAvailable {
		t.Fatalf("defunct GetText %#x", hr)
	}
}

func TestTextRangeFollowsEditsAndPassword(t *testing.T) {
	uiaOnce.Do(initUIA)
	e := newUIAElement(&uiaTree{})
	defer e.release()
	before := platform.AccessNode{Text: &platform.AccessText{Content: "abc😀def", Length: 7}}
	e.n = before
	var r uintptr
	rangeOut(uintptr(unsafe.Pointer(&r)), e, 4, 7)
	rangeObj := uiaRanges[r]
	after := platform.AccessNode{Text: &platform.AccessText{Content: "abXXc😀def", Length: 9}}
	e.followTextRanges(before, after)
	e.n = after
	if rangeObj.start != 6 || rangeObj.end != 9 {
		t.Fatalf("range after insertion: %d..%d", rangeObj.start, rangeObj.end)
	}
	e.n.States = platform.AccessPassword
	if e.supports(ifaceText) || e.supports(ifaceText2) {
		t.Fatal("password supports text")
	}
	if _, hr := liveTextRange(r); hr != uiaElementNotAvailable {
		t.Fatal("range reads a password")
	}
	release(r)
	if len(e.ranges) != 0 {
		t.Fatal("released range retained")
	}
}

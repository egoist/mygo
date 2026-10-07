//go:build windows && (amd64 || arm64)

package windows

import (
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

var testSafeArrayUBound = oleaut32.NewProc("SafeArrayGetUBound")
var testSafeArrayElement = oleaut32.NewProc("SafeArrayGetElement")
var testSafeArrayDestroy = oleaut32.NewProc("SafeArrayDestroy")

// TestAccessibilityCollectionLifetime retains a COM provider beyond window
// disposal and exercises its unavailable-element HRESULT path.
func TestAccessibilityCollectionLifetime(handle uintptr, label string, row, column int, destroy func()) bool {
	el := testCollection(handle, label)
	if el == nil {
		return false
	}
	grid := uiaPattern(el.ptr(ifaceSimple), 10006)
	if grid == 0 {
		return false
	}
	defer release(grid)
	var cell uintptr
	if comCall(grid, 3, uintptr(row), uintptr(column), uintptr(unsafe.Pointer(&cell))) != sOK || cell == 0 {
		return false
	}
	defer release(cell)
	destroy()
	var value variant
	return comCall(cell, 5, uiaNameProperty, uintptr(unsafe.Pointer(&value))) == uiaElementNotAvailable
}

func testCollection(handle uintptr, label string) *uiaElement {
	s := surfaceByHandle(handle)
	if s == nil {
		return nil
	}
	s.accessRoot()
	for _, n := range s.access.order {
		if n.n.Collection != nil && n.n.Label == label {
			return n
		}
	}
	return nil
}

func testArrayLength(array uintptr) int {
	if array == 0 {
		return 0
	}
	var bound int32
	testSafeArrayUBound.Call(array, 1, uintptr(unsafe.Pointer(&bound)))
	return int(bound + 1)
}

// TestAccessibilityCollection reads actual UIA vtables, rather than cached
// Go metadata, and releases every COM/SafeArray reference it receives.
func TestAccessibilityCollection(handle uintptr, label string, row, column int) (out platform.AccessCollectionProbe, ok bool) {
	el := testCollection(handle, label)
	if el == nil {
		return
	}
	grid := uiaPattern(el.ptr(ifaceSimple), 10006)
	if grid == 0 {
		return
	}
	defer release(grid)
	var rows, cols int32
	if comCall(grid, 4, uintptr(unsafe.Pointer(&rows))) != sOK || comCall(grid, 5, uintptr(unsafe.Pointer(&cols))) != sOK {
		return
	}
	out.Rows, out.Columns = int(rows), int(cols)
	var cell uintptr
	if comCall(grid, 3, uintptr(row), uintptr(column), uintptr(unsafe.Pointer(&cell))) != sOK || cell == 0 {
		return
	}
	defer release(cell)
	item := uiaPattern(cell, 10007)
	if item == 0 {
		return
	}
	defer release(item)
	var values [4]int32
	for i := range values {
		if comCall(item, 3+i, uintptr(unsafe.Pointer(&values[i]))) != sOK {
			return
		}
	}
	out.Row, out.Column, out.RowSpan, out.ColumnSpan = int(values[0]), int(values[1]), int(values[2]), int(values[3])
	v := uiaProperty(cell, uiaNameProperty)
	if v.VT == vtBSTR {
		out.Label = takeBSTR(uintptr(v.Val))
	}
	if table := uiaPattern(cell, 10013); table != 0 {
		var hs uintptr
		comCall(table, 4, uintptr(unsafe.Pointer(&hs)))
		if testArrayLength(hs) > 0 {
			var header uintptr
			index := int32(0)
			testSafeArrayElement.Call(hs, uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&header)))
			if header != 0 {
				v := uiaProperty(header, uiaNameProperty)
				if v.VT == vtBSTR {
					out.Header = takeBSTR(uintptr(v.Val))
				}
				release(header)
			}
		}
		if hs != 0 {
			testSafeArrayDestroy.Call(hs)
		}
		release(table)
	}
	if virtual := uiaPattern(cell, 10020); virtual != 0 {
		out.Virtualized = true
		release(virtual)
	}
	if selection := uiaPattern(el.ptr(ifaceSimple), 10001); selection != 0 {
		var array uintptr
		comCall(selection, 3, uintptr(unsafe.Pointer(&array)))
		out.Selected = testArrayLength(array)
		if array != 0 {
			testSafeArrayDestroy.Call(array)
		}
		release(selection)
	}
	ok = true
	return
}

func TestAccessibilityCollectionPerform(handle uintptr, label string, row, column int, action string) bool {
	el := testCollection(handle, label)
	if el == nil {
		return false
	}
	if action == "scroll" {
		p := uiaPattern(el.ptr(ifaceSimple), 10004)
		if p == 0 {
			return false
		}
		defer release(p)
		return comCall(p, 3, 2, 3) == sOK
	}
	var item *uiaElement
	if action == "realize" || el.n.Role == platform.RoleList && el.n.Collection.Grid {
		item = el.query(platform.AccessQuery{Kind: platform.AccessQueryCell, Row: row, Column: column})
	} else {
		item = el.query(platform.AccessQuery{Index: row})
	}
	if item == nil {
		return false
	}
	item.addRef()
	defer item.release()
	pattern, method := 10010, 3
	switch action {
	case "realize":
		pattern = 10020
	case "select":
	case "add":
		method = 4
	case "remove":
		method = 5
	default:
		return false
	}
	p := uiaPattern(item.ptr(ifaceSimple), pattern)
	if p == 0 {
		return action == "realize" && item.n.States&platform.AccessVirtualized == 0
	}
	defer release(p)
	return comCall(p, method) == sOK
}

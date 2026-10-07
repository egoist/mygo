//go:build darwin

package darwin

import (
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/internal/platform"
)

var testCollectionOnce sync.Once
var testCollectionRange func(id, objc.SEL) nsRange

// TestAccessibilityCollectionLifetime keeps native AX references across
// disposal, then checks that their lazy queries no longer reach the view.
func TestAccessibilityCollectionLifetime(handle uintptr, label string, row, column int, destroy func()) (ok bool) {
	withPool(func() {
		el := testCollection(handle, label)
		if el == nil {
			return
		}
		cell := retain(send(el.obj, "accessibilityCellForColumn:row:", uintptr(column), uintptr(row)))
		if cell == 0 {
			return
		}
		defer release(cell)
		rows := retain(send(el.obj, "accessibilityRows"))
		defer release(rows)
		destroy()
		frame := msgRect(cell, sel("accessibilityFrame"))
		ok = sendInt(rows, "count") == 0 && frame.Size.Width == 0 && frame.Size.Height == 0 && send(cell, "accessibilityParent") == 0
	})
	return
}

func testCollection(handle uintptr, label string) *accessElement {
	w := theBackend.byNSWindow[id(handle)]
	if w == nil || w.surface == nil {
		return nil
	}
	w.surface.accessRequested()
	for _, n := range w.surface.access.Nodes {
		if n.Label == label && n.Collection != nil {
			return w.surface.elements[n.ID]
		}
	}
	return nil
}

// TestAccessibilityCollection reads counts, coordinates, headers and the
// selection through AppKit, including a cell beyond the current viewport.
func TestAccessibilityCollection(handle uintptr, label string, row, column int) (out platform.AccessCollectionProbe, ok bool) {
	withPool(func() {
		el := testCollection(handle, label)
		if el == nil {
			return
		}
		testCollectionOnce.Do(func() { purego.RegisterFunc(&testCollectionRange, msgSendAddr) })
		out.Rows, out.Columns = sendInt(el.obj, "accessibilityRowCount"), sendInt(el.obj, "accessibilityColumnCount")
		out.Selected = sendInt(send(el.obj, "accessibilitySelectedRows"), "count")
		if el.isGrid() {
			out.Selected = sendInt(send(el.obj, "accessibilitySelectedCells"), "count")
		}
		cell := send(el.obj, "accessibilityCellForColumn:row:", uintptr(column), uintptr(row))
		if cell == 0 {
			return
		}
		r, c := testCollectionRange(cell, sel("accessibilityRowIndexRange")), testCollectionRange(cell, sel("accessibilityColumnIndexRange"))
		out.Row, out.RowSpan, out.Column, out.ColumnSpan = int(r.Location), int(r.Length), int(c.Location), int(c.Length)
		out.Label = stringOf(send(cell, "accessibilityLabel"))
		hs := send(cell, "accessibilityColumnHeaderUIElements")
		if sendInt(hs, "count") > 0 {
			out.Header = stringOf(send(send(hs, "objectAtIndex:", 0), "accessibilityLabel"))
		}
		frame := msgRect(cell, sel("accessibilityFrame"))
		out.Virtualized = frame.Size.Width == 0 || frame.Size.Height == 0
		// Read one distant row using NSArray's primitives, never a whole copy.
		rows := send(el.obj, "accessibilityRows")
		if sendInt(rows, "count") != out.Rows {
			return
		}
		if send(rows, "objectAtIndex:", uintptr(row)) == 0 {
			return
		}
		ok = true
	})
	return
}

// TestAccessibilityCollectionPerform exercises native actions of a collection.
func TestAccessibilityCollectionPerform(handle uintptr, label string, row, column int, action string) (ok bool) {
	withPool(func() {
		el := testCollection(handle, label)
		if el == nil {
			return
		}
		switch action {
		case "realize":
			cell := send(el.obj, "accessibilityCellForColumn:row:", uintptr(column), uintptr(row))
			if cell == 0 {
				return
			}
			send(cell, "accessibilityPerformAction:", uintptr(nsString("AXScrollToVisible")))
			ok = true
		case "select", "add", "remove":
			rows := send(el.obj, "accessibilityRows")
			target := send(rows, "objectAtIndex:", uintptr(row))
			getter, setter := "accessibilitySelectedRows", "setAccessibilitySelectedRows:"
			if el.isGrid() {
				target = send(el.obj, "accessibilityCellForColumn:row:", uintptr(column), uintptr(row))
				getter, setter = "accessibilitySelectedCells", "setAccessibilitySelectedCells:"
			}
			if target == 0 {
				return
			}
			var chosen []id
			if action != "select" {
				chosen = arrayItems(send(el.obj, getter))
			}
			if action == "remove" {
				var next []id
				for _, obj := range chosen {
					if obj != target {
						next = append(next, obj)
					}
				}
				chosen = next
			} else {
				chosen = append(chosen, target)
			}
			send(el.obj, setter, uintptr(nsArray(chosen...)))
			ok = true
		case "scroll":
			ok = sendBool(el.obj, "accessibilityPerformScrollDown")
		}
	})
	return
}

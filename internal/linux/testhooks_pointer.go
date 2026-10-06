//go:build linux && (amd64 || arm64)

package linux

import (
	"sync"
	"unsafe"
)

// Synthetic GDK touch/gesture events exercise GTK dispatch and the native
// translator without requiring a digitizer on an Xvfb runner. Like the
// other test hooks, these functions run on the main thread.
var testPointer struct {
	once        sync.Once
	newEvent    func(typ int32) ptr
	widgetEvent func(widget, event ptr) bool
	setDevice   func(event, device ptr)
	alloc       func(size uintptr) ptr
	sequences   map[[2]uint64]ptr
}

func testPointerEvent(s *surface, typ int32, fill func(ptr)) bool {
	testPointer.once.Do(func() {
		mustBind(libGDK, &testPointer.newEvent, "gdk_event_new")
		mustBind(libGTK, &testPointer.widgetEvent, "gtk_widget_event")
		mustBind(libGDK, &testPointer.setDevice, "gdk_event_set_device")
		mustBind(libGLib, &testPointer.alloc, "g_malloc0")
		testPointer.sequences = make(map[[2]uint64]ptr)
	})
	e := testPointer.newEvent(typ)
	defer gdkEventFree(e)
	*(*ptr)(testEventAddress(e, 8)) = gObjectRef(s.eventWindow())
	*(*int8)(testEventAddress(e, 16)) = 1
	testPointer.setDevice(e, gdkSeatGetPointer(gdkDisplayGetDefaultSeat(gtkWidgetGetDisplay(s.area))))
	fill(e)
	return testPointer.widgetEvent(s.area, e)
}

// TestTouchSurface sends a GDK touch event. phase is 0 begin, 1 update,
// 2 end, 3 cancel; contact is a test ID mapped to an opaque GDK sequence.
func TestTouchSurface(handle uintptr, contact uint64, phase int, x, y float64) bool {
	w := windowByHandle(handle)
	if w == nil || w.surface == nil || phase < 0 || phase > 3 {
		return false
	}
	key := [2]uint64{uint64(handle), contact}
	result := testPointerEvent(w.surface, 37+int32(phase), func(e ptr) {
		sequence := testPointer.sequences[key]
		if phase == 0 {
			sequence = testPointer.alloc(1)
			testPointer.sequences[key] = sequence
		}
		*(*float64)(testEventAddress(e, 24)) = x
		*(*float64)(testEventAddress(e, 32)) = y
		*(*ptr)(testEventAddress(e, 56)) = sequence
	})
	if phase >= 2 {
		gFree(testPointer.sequences[key])
		delete(testPointer.sequences, key)
	}
	return result
}

// TestPinchSurface sends a GDK pinch, whose scale is cumulative and angle
// delta clockwise radians. phase uses GDK's begin/update/end/cancel values.
func TestPinchSurface(handle uintptr, phase int, x, y, dx, dy, scale, rotation float64) bool {
	w := windowByHandle(handle)
	if w == nil || w.surface == nil {
		return false
	}
	return testPointerEvent(w.surface, 42, func(e ptr) {
		*(*int8)(testEventAddress(e, 17)) = int8(phase)
		*(*int8)(testEventAddress(e, 18)) = 2
		for i, v := range []float64{x, y, dx, dy, rotation, scale} {
			*(*float64)(testEventAddress(e, 24+ptr(i)*8)) = v
		}
	})
}

// As field does for reads, recover a pointer to native-owned memory and
// add the ABI offset without a stored uintptr-to-pointer conversion.
func testEventAddress(event ptr, offset uintptr) unsafe.Pointer {
	base := *(*unsafe.Pointer)(unsafe.Pointer(&event))
	return unsafe.Add(base, offset)
}

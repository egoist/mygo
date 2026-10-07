package platform

// PointerDevice describes the source of pointer input. Mouse is the zero
// value, including legacy events without extended information.
type PointerDevice uint8

const (
	PointerMouse PointerDevice = iota
	PointerTouch
	PointerPen
	// PointerTouchpad contacts are indirect, without a screen position.
	PointerTouchpad
)

// PointerInfo describes one contact, or a hovering mouse/pen. ID is stable
// during a contact's lifetime, local to a surface; mouse ID is zero.
type PointerInfo struct {
	ID               uint64
	Device           PointerDevice
	Primary, Contact bool
	// Pressure is 0..1, tilt is -90..90 degrees along the screen axes.
	// Availability flags distinguish an unsupported axis from a zero value.
	Pressure, TiltX, TiltY float32
	HasPressure, HasTilt   bool
	Eraser                 bool
	// NormalizedX/Y are 0..1, top-left coordinates on an indirect device.
	// X/Y on SurfaceEvent remain the cursor anchor for these contacts.
	NormalizedX, NormalizedY float32
}

// GestureKind is a set of transformations recognized together.
type GestureKind uint8

const (
	GesturePan GestureKind = 1 << iota
	GesturePinch
	GestureRotation
)

// GesturePhase is the lifetime of a recognized gesture.
type GesturePhase uint8

const (
	GestureNone GesturePhase = iota
	GestureBegin
	GestureUpdate
	GestureEnd
	GestureCancel
)

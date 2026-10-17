//go:build darwin

package darwin

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// This file drives AVFoundation's capture side: the devices it offers, the
// question macOS asks before an app may use them, and the session that hands
// over the pictures a camera takes.
//
// The session calls back on a queue of its own rather than on the main
// thread, which is where a picture must be taken apart: what the callback does
// with a sample buffer is the caller's, and it must be quick.

// What AVCaptureDevice answers about permission.
const (
	CameraNotDetermined = 0
	CameraRestricted    = 1
	CameraDenied        = 2
	CameraAuthorized    = 3
)

var (
	cameraOnce sync.Once
	cameraErr  error

	dispatchQueueCreate func(label *byte, attr uintptr) uintptr
	sendInt1            func(id, sel, a uintptr) int

	cameraQueue uintptr

	delegateOnce  sync.Once
	delegateClass objc.Class
	delegateErr   error

	// cameraRoutes finds the callback a delegate object stands for: the
	// object the session calls back on is the key, and it is the only thing
	// the callback is told about the camera whose picture it holds.
	cameraRoutes sync.Map // uintptr -> func(sample uintptr)
)

// LoadCamera loads what capturing pictures needs on top of playing them: the
// dispatch queue the system calls back on.
func LoadCamera() error {
	cameraOnce.Do(func() {
		if cameraErr = Load(); cameraErr != nil {
			return
		}
		msgSend, err := purego.Dlsym(libObj, "objc_msgSend")
		if err != nil {
			cameraErr = err
			return
		}
		purego.RegisterFunc(&sendInt1, msgSend)
		lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			cameraErr = err
			return
		}
		addr, err := purego.Dlsym(lib, "dispatch_queue_create")
		if err != nil {
			cameraErr = err
			return
		}
		purego.RegisterFunc(&dispatchQueueCreate, addr)
		// One queue for every camera's pictures: the callback copies a
		// picture and returns, so a second camera waits on the copy of the
		// first, which costs no frame of either.
		cameraQueue = dispatchQueueCreate(cstr("mygo media capture"), 0)
		if cameraQueue == 0 {
			cameraErr = fmt.Errorf("media: no dispatch queue for camera pictures")
		}
	})
	return cameraErr
}

// CaptureQueue is the dispatch queue the platform calls capture callbacks on.
// Cameras and screen capture share it: each callback copies one picture and
// returns, so a second source waits on that copy rather than losing a frame.
func CaptureQueue() (uintptr, error) {
	if err := LoadCamera(); err != nil {
		return 0, err
	}
	return cameraQueue, nil
}

// VideoMediaType is AVFoundation's name for video, which is what asks about
// cameras and what a session takes pictures in.
func VideoMediaType() uintptr { return Global("AVMediaTypeVideo") }

// A VideoDevice is a camera AVFoundation offers.
type VideoDevice struct {
	// ID is the device's uniqueID: the same across launches.
	ID string
	// Name is the device's localizedName: what a person picks from.
	Name string

	ref uintptr
}

// VideoDevices lists the cameras AVFoundation offers, without opening one and
// without asking for permission.
func VideoDevices() ([]VideoDevice, error) {
	if err := LoadCamera(); err != nil {
		return nil, err
	}
	var out []VideoDevice
	WithPool(func() {
		array := SendID1(Class("AVCaptureDevice"), Sel("devicesWithMediaType:"), VideoMediaType())
		if array == 0 {
			return
		}
		n := ArrayCount(array)
		out = make([]VideoDevice, 0, n)
		for i := 0; i < n; i++ {
			ref := ArrayAt(array, i)
			id := String(SendID(ref, Sel("uniqueID")))
			name := String(SendID(ref, Sel("localizedName")))
			out = append(out, VideoDevice{ID: id, Name: name, ref: ref})
		}
	})
	if out == nil {
		return nil, fmt.Errorf("media: macOS lists no cameras")
	}
	return out, nil
}

// CameraAuthorizationStatus is what macOS says about this app using the camera,
// without asking.
func CameraAuthorizationStatus() int {
	if err := LoadCamera(); err != nil {
		return CameraNotDetermined
	}
	return sendInt1(Class("AVCaptureDevice"), Sel("authorizationStatusForMediaType:"), VideoMediaType())
}

// RequestCameraAccess asks the person to let the app use cameras, where macOS
// has not asked yet, and hands the answer to fn. fn runs on a thread of the
// system's own, not on the main thread; asking again once there is an answer
// only hands the same answer over.
func RequestCameraAccess(fn func(granted bool)) {
	if err := LoadCamera(); err != nil {
		fn(false)
		return
	}
	block := objc.NewBlock(func(b objc.Block, granted bool) { fn(granted) })
	handler := block.Copy() // the system keeps the handler; it copies it too
	SendVoid2(Class("AVCaptureDevice"), Sel("requestAccessForMediaType:completionHandler:"), VideoMediaType(), uintptr(handler))
	handler.Release()
	block.Release()
}

// A Capture is a running session that hands over the pictures a camera takes:
// fn is called with each sample buffer, on the queue the session calls back
// on.
type Capture struct {
	session  uintptr
	output   uintptr
	input    uintptr
	delegate uintptr
	fn       func(sample uintptr)

	mu     sync.Mutex
	closed bool
}

// OpenCapture starts capturing from the camera whose uniqueID is id, and calls
// fn with every sample buffer the camera makes.
//
// fn runs on a queue of the system's own, never on the main thread, and must
// return quickly: the sample buffer it is given is good only until it returns,
// and the next picture waits for it.
func OpenCapture(id string, fn func(sample uintptr)) (*Capture, error) {
	if err := LoadCamera(); err != nil {
		return nil, err
	}
	class, err := cameraDelegate()
	if err != nil {
		return nil, err
	}
	devices, err := VideoDevices()
	if err != nil {
		return nil, err
	}
	var device uintptr
	for _, d := range devices {
		if d.ID == id {
			device = d.ref
			break
		}
	}
	if device == 0 {
		return nil, fmt.Errorf("media: no camera with id %q", id)
	}

	c := &Capture{fn: fn}
	var openErr error
	WithPool(func() {
		var errRef uintptr
		c.input = SendID2(Class("AVCaptureDeviceInput"), Sel("deviceInputWithDevice:error:"), device, uintptr(unsafe.Pointer(&errRef)))
		if c.input == 0 {
			openErr = fmt.Errorf("media: the camera refused an input: %s", ErrorString(errRef))
			return
		}
		Retain(c.input)

		c.session = SendID(Alloc(Class("AVCaptureSession")), Sel("init"))
		c.output = SendID(Alloc(Class("AVCaptureVideoDataOutput")), Sel("init"))
		c.delegate = SendID(Alloc(uintptr(class)), Sel("init"))
		if c.session == 0 || c.output == 0 || c.delegate == 0 {
			openErr = fmt.Errorf("media: macOS refused a capture session")
			return
		}

		// No picture format is asked for: a capture output does not convert
		// one for the app, and asking this one for 32-bit RGB makes macOS
		// abort inside its own renderer. The camera delivers what it has, and
		// this package converts that to the RGBA it hands out.
		//
		// A picture the app was too slow to take is dropped here, where the
		// camera can still keep up, rather than in the app's queue.
		SendVoid1(c.output, Sel("setAlwaysDiscardsLateVideoFrames:"), 1)

		cameraRoutes.Store(c.delegate, fn)
		SendVoid2(c.output, Sel("setSampleBufferDelegate:queue:"), c.delegate, cameraQueue)

		SendVoid(c.session, Sel("beginConfiguration"))
		if !SendBool1(c.session, Sel("canAddInput:"), c.input) || !SendBool1(c.session, Sel("canAddOutput:"), c.output) {
			// The session was made for neither: freeing what was made above
			// is Close's, which the caller reaches on the error.
			SendVoid(c.session, Sel("commitConfiguration"))
			openErr = fmt.Errorf("media: macOS refused the camera's session")
			return
		}
		SendVoid1(c.session, Sel("addInput:"), c.input)
		SendVoid1(c.session, Sel("addOutput:"), c.output)
		SendVoid(c.session, Sel("commitConfiguration"))
	})
	if openErr != nil {
		c.Close()
		return nil, openErr
	}

	// Starting runs the camera's own thread into the app's queue: it takes a
	// moment, and must not happen on the main thread, which is where the
	// caller is not.
	WithPool(func() { SendVoid(c.session, Sel("startRunning")) })
	return c, nil
}

// Close stops the session, waits for its thread, and frees what it held.
func (c *Capture) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	session, output, input, delegate := c.session, c.output, c.input, c.delegate
	c.mu.Unlock()

	cameraRoutes.Delete(delegate)
	WithPool(func() {
		if output != 0 {
			// No more pictures, and no delegate to call: the session does
			// not retain it, so it goes with the release below.
			SendVoid2(output, Sel("setSampleBufferDelegate:queue:"), 0, 0)
		}
		if session != 0 {
			SendVoid(session, Sel("stopRunning"))
		}
		for _, p := range []uintptr{input, output, session, delegate} {
			if p != 0 {
				Release(p)
			}
		}
	})
	return nil
}

// cameraDelegate is the class the session calls back into, made once: the
// runtime has no way to free a class, so every camera shares one.
func cameraDelegate() (objc.Class, error) {
	delegateOnce.Do(func() {
		delegateClass, delegateErr = objc.RegisterClass("MyGoCameraOutput", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
			{
				Cmd: objc.RegisterName("captureOutput:didOutputSampleBuffer:fromConnection:"),
				Fn: func(self objc.ID, _cmd objc.SEL, output objc.ID, sample uintptr, connection objc.ID) {
					if fn, ok := cameraRoutes.Load(uintptr(self)); ok {
						fn.(func(uintptr))(sample)
					}
				},
			},
		})
	})
	return delegateClass, delegateErr
}

// OpenCameraSettings shows the part of System Settings where the person can
// change the app's answer about the camera.
func OpenCameraSettings() error {
	if err := Load(); err != nil {
		return err
	}
	workspace := SendID(Class("NSWorkspace"), Sel("sharedWorkspace"))
	if workspace == 0 {
		return fmt.Errorf("media: no system settings to show")
	}
	SendBool1(workspace, Sel("openURL:"), URL("x-apple.systempreferences:com.apple.preference.security?Privacy_Camera"))
	return nil
}

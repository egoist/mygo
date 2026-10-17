//go:build linux && (amd64 || arm64)

package linux

import (
	"errors"
	"unsafe"
)

// A VideoDevice is a camera as GStreamer's device monitor lists it: what it
// calls itself, and the path an element opens it by.
type VideoDevice struct {
	// ID is the device's own path, such as /dev/video0.
	ID string
	// Name is what the device calls itself, such as Built-in Front
	// Camera.
	Name string
}

// VideoDevices lists the machine's cameras, as its GStreamer knows them. A
// machine whose GStreamer has no device provider reports no cameras rather
// than an error: what a camera needs is said when one is opened.
//
// Only the devices a pipeline here opens by path are listed, which today
// means the Video4Linux ones: a camera some other plugin would read is not
// one this version opens.
func VideoDevices() ([]VideoDevice, error) {
	if err := Load(); err != nil {
		return nil, err
	}
	monitor := gstDeviceMonitorNew()
	if monitor == 0 {
		return nil, errors.New("gstreamer: no device monitor")
	}
	defer gstObjectUnref(monitor)
	added := withCStringRet("Video/Source", func(classes *byte) int32 {
		return gstDeviceMonitorAddFilter(monitor, classes, 0)
	})
	if added == 0 {
		return nil, errors.New("gstreamer: the device monitor knows no video sources")
	}
	if gstDeviceMonitorStart(monitor) == 0 {
		// No device provider is installed, which is a machine with
		// nothing this version lists rather than a failure.
		return nil, nil
	}
	defer gstDeviceMonitorStop(monitor)
	list := gstDeviceMonitorGetDevices(monitor)
	if list == 0 {
		return nil, nil
	}
	defer gListFree(list)
	var devices []VideoDevice
	for i, n := uint32(0), gListLength(list); i < n; i++ {
		device := gListNthData(list, i)
		if device == 0 {
			continue
		}
		// The monitor hands over one reference per device, and the
		// list itself is freed above.
		what := videoDevice(device)
		gstObjectUnref(device)
		if what.ID == "" {
			continue
		}
		devices = append(devices, what)
	}
	return devices, nil
}

// videoDevice reads a device's name and the path a pipeline opens it by.
func videoDevice(device uintptr) VideoDevice {
	var out VideoDevice
	if name := gstDeviceGetDisplayName(device); name != nil {
		out.Name = goString(name)
		gFree(unsafe.Pointer(name))
	}
	properties := gstDeviceGetProperties(device)
	if properties == 0 {
		return out
	}
	defer gstStructureFree(properties)
	// Video4Linux devices are the ones a pipeline here opens, and this
	// is the property that says which of them a device is.
	if path := withCStringRet("api.v4l2.path", func(field *byte) *byte {
		return gstStructureGetString(properties, field)
	}); path != nil {
		out.ID = goString(path)
	}
	return out
}

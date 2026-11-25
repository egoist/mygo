//go:build darwin && !ios

package gpusurface

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

var cg struct {
	once sync.Once
	err  error
	// pqName is kCGColorSpaceITUR_2100_PQ, a CFStringRef.
	pqName                                 uintptr
	createWithName, retainSpace, releaseCF uintptr
}

func loadCG() error {
	cg.once.Do(func() {
		h, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			cg.err = fmt.Errorf("gpusurface: %w", err)
			return
		}
		sym := func(name string) uintptr {
			p, err := purego.Dlsym(h, name)
			if err != nil && cg.err == nil {
				cg.err = fmt.Errorf("gpusurface: %w", err)
			}
			return p
		}
		name := sym("kCGColorSpaceITUR_2100_PQ")
		cg.createWithName = sym("CGColorSpaceCreateWithName")
		cg.retainSpace = sym("CGColorSpaceRetain")
		cg.releaseCF = sym("CGColorSpaceRelease")
		if cg.err == nil {
			cg.pqName = **(**uintptr)(unsafe.Pointer(&name))
		}
	})
	return cg.err
}

// retainSpace retains a CGColorSpaceRef, which may be NULL.
func retainSpace(cs uintptr) uintptr {
	if cs == 0 || loadCG() != nil {
		return cs
	}
	purego.SyscallN(cg.retainSpace, cs)
	return cs
}

func releaseSpace(cs *uintptr) {
	if *cs != 0 && loadCG() == nil {
		purego.SyscallN(cg.releaseCF, *cs)
	}
	*cs = 0
}

// ErrHDRNeeds10Bit is SetHDR10's error on a surface whose drawables are
// 8-bit: make it with Options.HDR10 or Drawable10Bit.
var ErrHDRNeeds10Bit = errors.New("gpusurface: HDR10 needs BGR10A2 drawables")

// HDR10 reports whether the surface presents BT.2020 PQ.
func (s *Surface) HDR10() bool { return s.hdr.Load() }

// SetHDR10 presents the frames as BT.2020 PQ, on: the layer wants
// extended dynamic range in kCGColorSpaceITUR_2100_PQ, YUV frames convert
// with the BT.2020 matrix and the overlay maps to PQ; off gives the layer
// its colour space and standard range back, as the surface was made. Call
// it from the thread that presents, between presents, or before the
// first.
func (s *Surface) SetHDR10(on bool) error {
	if on == s.hdr.Load() {
		return nil
	}
	if s.pixelFormat != pixelFormatBGR10A2Unorm {
		return ErrHDRNeeds10Bit
	}
	if err := loadCG(); err != nil {
		return err
	}
	space := s.sdrSpace
	if on {
		r, _, _ := purego.SyscallN(cg.createWithName, cg.pqName)
		if r == 0 {
			return errors.New("gpusurface: no ITU-R BT.2100 PQ colour space")
		}
		defer releaseSpace(&r)
		space = r
	}
	send(s.layer, "setWantsExtendedDynamicRangeContent:", on)
	send(s.layer, "setColorspace:", space)
	s.hdr.Store(on)
	return nil
}

// layerHDR reads back what SetHDR10 set on the layer: whether it wants
// extended dynamic range, and its colour space. For tests.
func (s *Surface) layerHDR() (edr bool, space uintptr) {
	return send(s.layer, "wantsExtendedDynamicRangeContent") != 0, send(s.layer, "colorspace")
}

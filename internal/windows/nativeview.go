//go:build windows && (amd64 || arm64)

package windows

import (
	"errors"
	"math"
	"syscall"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

const nativeViewClass = "MyGoHostedView"
const wmNativeFocus = wmApp + 0x40

var (
	hostedViews                  = map[uintptr]*nativeView{}
	nativeClassRegistered        bool
	nativeFocusHook              uintptr
	nativeFocusCallback          uintptr
	procGetFocus                 = user32.NewProc("GetFocus")
	procIsChild                  = user32.NewProc("IsChild")
	procIsWindow                 = user32.NewProc("IsWindow")
	procGetParent                = user32.NewProc("GetParent")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procSetWindowsHookExW        = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx           = user32.NewProc("CallNextHookEx")
)

type nativeView struct {
	s          *surface
	h          platform.NativeViewHandler
	clip, view uintptr
	p          platform.NativeViewPlacement
	closed     bool
}

func registerNativeViewClass() {
	if nativeClassRegistered {
		return
	}
	nativeClassRegistered = true
	wc := wndClassEx{WndProc: wndProcCallback, Instance: instance(), ClassName: u16(nativeViewClass)}
	wc.Size = uint32(unsafe.Sizeof(wc))
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	// One thread hook routes focus of arbitrary descendants, including
	// children created later by a compound control. CBT fires before
	// SetFocus completes: notify after it, without recursive SetFocus.
	nativeFocusCallback = syscall.NewCallback(func(code int32, wp, lp uintptr) uintptr {
		if code == 9 { // HCBT_SETFOCUS
			if n := nativeHostFor(wp); n != nil {
				postMessage(n.clip, wmNativeFocus, 0, 0)
			}
		}
		r, _, _ := procCallNextHookEx.Call(nativeFocusHook, uintptr(code), wp, lp)
		return r
	})
	thread := uintptr(currentThreadID())
	nativeFocusHook, _, _ = procSetWindowsHookExW.Call(5, nativeFocusCallback, 0, thread) // WH_CBT
}

func (b *Backend) NewNativeView(s platform.Surface, h platform.NativeViewHandler) (platform.NativeViewHost, error) {
	area, ok := s.(*surface)
	if !ok || area.w.closed {
		return nil, platform.ErrUnsupported
	}
	registerNativeViewClass()
	if nativeFocusHook == 0 {
		return nil, errors.New("cannot install native view focus routing")
	}
	n := &nativeView{s: area, h: h}
	n.clip = createWindow(0, nativeViewClass, "", wsChild|wsClipChildren|wsClipSiblings, 0, 0, 1, 1, area.hwnd)
	if n.clip == 0 {
		return nil, errors.New("cannot create native view container")
	}
	hostedViews[n.clip] = n
	area.nativeViews = append(area.nativeViews, n)
	return n, nil
}

func (n *nativeView) Parent() uintptr { return n.clip }
func (n *nativeView) Attach(v uintptr) error {
	valid, _, _ := procIsWindow.Call(v)
	parent, _, _ := procGetParent.Call(v)
	thread, _, _ := procGetWindowThreadProcessId.Call(v, 0)
	if n.closed || n.view != 0 || valid == 0 || parent != n.clip || uint32(thread) != currentThreadID() || windowLong(v, gwlStyle)&wsChild == 0 {
		return errors.New("a child HWND created with the supplied Parent is required")
	}
	n.view = v
	return nil
}

func nativePixels(r platform.RectF, scale float64) rect {
	return rect{Left: int32(math.Round(r.X * scale)), Top: int32(math.Round(r.Y * scale)), Right: int32(math.Round((r.X + r.W) * scale)), Bottom: int32(math.Round((r.Y + r.H) * scale))}
}

func (n *nativeView) Place(p platform.NativeViewPlacement) {
	if n.closed {
		return
	}
	n.p = p
	if !p.Visible || !p.Enabled {
		n.Blur()
	}
	var enabled uintptr
	if p.Enabled {
		enabled = 1
	}
	procEnableWindow.Call(n.clip, enabled)
	if !p.Visible {
		procShowWindow.Call(n.clip, swHide)
		return
	}
	scale := float64(n.s.dpi()) / 96
	clip, bounds := nativePixels(p.Clip, scale), nativePixels(p.Bounds, scale)
	procSetWindowPos.Call(n.clip, 0, uintptr(clip.Left), uintptr(clip.Top), uintptr(clip.Right-clip.Left), uintptr(clip.Bottom-clip.Top), swpNoZOrder|swpNoActivate)
	procSetWindowPos.Call(n.view, 0, uintptr(bounds.Left-clip.Left), uintptr(bounds.Top-clip.Top), uintptr(bounds.Right-bounds.Left), uintptr(bounds.Bottom-bounds.Top), swpNoZOrder|swpNoActivate)
	procShowWindow.Call(n.view, swShow)
	procShowWindow.Call(n.clip, swShow)
}

func nativeHostFor(hwnd uintptr) *nativeView {
	for _, n := range hostedViews {
		if n.closed || !n.p.Visible {
			continue
		}
		child, _, _ := procIsChild.Call(n.clip, hwnd)
		if hwnd == n.clip || child != 0 {
			return n
		}
	}
	return nil
}
func (n *nativeView) hasFocus() bool { f, _, _ := procGetFocus.Call(); return nativeHostFor(f) == n }
func (n *nativeView) Focus(bool) {
	if !n.closed && n.p.Visible && n.p.Enabled && !n.hasFocus() {
		procSetFocus.Call(n.view)
	}
}
func (n *nativeView) Blur() {
	if !n.s.w.closed && n.hasFocus() {
		procSetFocus.Call(n.s.hwnd)
	}
}
func (n *nativeView) Close() { n.close(true) }

func (n *nativeView) Command(command string) bool {
	if n.closed || !n.p.Enabled || !n.hasFocus() {
		return false
	}
	focus, _, _ := procGetFocus.Call()
	if command == "selectAll" {
		procSendMessageW.Call(focus, 0x00b1, 0, ^uintptr(0))
		return true
	} // EM_SETSEL
	message := map[string]uintptr{"cut": 0x0300, "copy": 0x0301, "paste": 0x0302, "delete": 0x0303, "undo": 0x0304, "redo": 0x0454}[command]
	if message == 0 {
		return false
	}
	procSendMessageW.Call(focus, message, 0, 0)
	return true
}
func (n *nativeView) close(destroy bool) {
	if n.closed {
		return
	}
	n.Blur()
	n.closed = true
	n.h.Closing()
	delete(hostedViews, n.clip)
	if destroy && !n.s.w.closed {
		procDestroyWindow.Call(n.clip)
	} // destroys the adopted child too
	for i, v := range n.s.nativeViews {
		if v == n {
			n.s.nativeViews = append(n.s.nativeViews[:i:i], n.s.nativeViews[i+1:]...)
			break
		}
	}
}
func (s *surface) closeNativeViews() {
	for len(s.nativeViews) > 0 {
		i := len(s.nativeViews) - 1
		n := s.nativeViews[i]
		s.nativeViews = s.nativeViews[:i]
		n.Close()
	}
}

func (n *nativeView) message(m uint32, wp, lp uintptr) (uintptr, bool) {
	switch m {
	case wmNativeFocus:
		if n.hasFocus() && !n.closed {
			n.h.Focused()
		}
		return 0, true
	case wmDestroy:
		n.close(false)
		return 0, true
	case wmGetObject:
		if int32(lp) == uiaRootObjectID && n.p.Visible {
			n.s.accessRoot()
			if t := n.s.access; t != nil {
				if e := t.nodes[n.p.ID]; e != nil {
					r, _, _ := procUiaReturnRawElementProvider.Call(n.clip, wp, lp, e.ptr(ifaceSimple))
					return r, true
				}
			}
		}
	}
	return n.h.Message(m, wp, lp)
}

// takeNativeTab runs before TranslateMessage, so no WM_CHAR(Tab) is
// delivered to an edit control after focus moves into MyGo.
func takeNativeTab(m *msg) bool {
	if m.Message != wmKeyDown || m.WParam != 9 {
		return false
	}
	n := nativeHostFor(m.HWnd)
	mods := mods()
	if n == nil || n.p.ManagesTab || mods&^platform.ModShift != 0 {
		return false
	}
	n.h.Traverse(mods&platform.ModShift != 0)
	return true
}

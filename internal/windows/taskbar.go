//go:build windows && (amd64 || arm64)

package windows

import (
	"math"
	"strings"
	"unsafe"
)

// A window's taskbar button: progress, hiding it, flashing it, its icon.
// Explorer creates the button when the window is first shown (and again
// when Explorer restarts) and then sends TaskbarButtonCreated, so the state
// is kept on the window and applied then.

var (
	clsidTaskbarList = guid("56fdf344-fd6d-11d0-958a-006097c9a090")
	iidITaskbarList3 = guid("ea1afb91-9e28-4b86-90e9-9e9f8a5eefaf")
)

// ITaskbarList3 vtable indices and progress flags (ShObjIdl_core.h).
const (
	tbHrInit           = 3
	tbAddTab           = 4
	tbDeleteTab        = 5
	tbSetProgressValue = 9
	tbSetProgressState = 10
	tbSetOverlayIcon   = 18

	// smCxIcon and smCxSmIcon name the system's big and small icon sizes.
	smCxIcon, smCxSmIcon = 11, 49

	tbpfNoProgress    = 0
	tbpfIndeterminate = 1
	tbpfNormal        = 2
	tbpfError         = 4
	tbpfPaused        = 8
)

// iconSize is a system icon size metric at a DPI, scaled from its default
// at 96 DPI when the system does not say.
func iconSize(metric uintptr, def, dpi int) int {
	if has(procGetSystemMetricsForDpi) {
		if n, _, _ := procGetSystemMetricsForDpi.Call(metric, uintptr(dpi)); n != 0 {
			return int(n)
		}
	}
	return def * dpi / 96
}

// taskbar returns the shell's ITaskbarList3, created on first use, or 0.
func (b *Backend) taskbar() uintptr {
	if b.taskbarList == 0 && !b.taskbarFailed {
		var tl uintptr
		hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidTaskbarList)), 0, clsctxInprocServer,
			uintptr(unsafe.Pointer(&iidITaskbarList3)), uintptr(unsafe.Pointer(&tl)))
		if failed(hr) || tl == 0 || failed(comCall(tl, tbHrInit)) {
			b.taskbarFailed = true
			return 0
		}
		b.taskbarList = tl
	}
	return b.taskbarList
}

// applyTaskbar puts the window's state on its taskbar button.
func (w *window) applyTaskbar() {
	tl := w.b.taskbar()
	if tl == 0 {
		return
	}
	if w.skipTaskbar {
		comCall(tl, tbDeleteTab, w.hwnd)
		return
	}
	w.applyProgress(tl)
	if thumbApply != nil {
		thumbApply(w)
	}
	if badgeApply != nil && w.b.badge != "" {
		badgeApply(w)
	}
}

// applyProgress updates only the taskbar's progress, so frequent
// SetProgressBar calls do not rebuild the toolbar's icons or call
// ThumbBarAddButtons again.
func (w *window) applyProgress(tl uintptr) {
	flags := map[string]uintptr{"normal": tbpfNormal, "indeterminate": tbpfIndeterminate, "paused": tbpfPaused, "error": tbpfError}[w.progress.state]
	comCall(tl, tbSetProgressState, w.hwnd, flags)
	if flags != tbpfNoProgress && flags != tbpfIndeterminate {
		const total = 10000
		comCall(tl, tbSetProgressValue, w.hwnd, uintptr(w.progress.value*total), total)
	}
}

func (w *window) SetProgressBar(state string, value float64) {
	w.progress.state, w.progress.value = state, value
	if !w.skipTaskbar {
		if tl := w.b.taskbar(); tl != 0 {
			w.applyProgress(tl)
		}
	}
}

func (w *window) SetSkipTaskbar(v bool) {
	if v == w.skipTaskbar {
		return
	}
	w.skipTaskbar = v
	tl := w.b.taskbar()
	if tl == 0 {
		return
	}
	if v {
		comCall(tl, tbDeleteTab, w.hwnd)
		// The button is gone, and with it the toolbar: a new one gets
		// both again on TaskbarButtonCreated.
		w.taskbarButton, w.thumb.live = false, false
		return
	}
	comCall(tl, tbAddTab, w.hwnd)
	w.applyProgress(tl)
}

func (w *window) FlashFrame(flash bool) {
	fi := flashWInfo{HWnd: w.hwnd, Flags: flashwStop}
	if flash {
		fi.Flags = flashwAll | flashwTimerNoFG // until the window comes to the foreground
	}
	fi.Size = uint32(unsafe.Sizeof(fi))
	procFlashWindowEx.Call(uintptr(unsafe.Pointer(&fi)))
}

// SetVisibleOnAllWorkspaces does nothing: Windows offers apps no way to
// pin a window to every virtual desktop.
func (w *window) SetVisibleOnAllWorkspaces(bool) {}

// SetIcon sets the window's own icons, in the sizes of the title bar and
// the taskbar at its DPI.
func (w *window) SetIcon(png []byte) error {
	var small, big uintptr
	if png != nil {
		dpi := dpiOf(w.hwnd)
		var err error
		if small, err = iconFromPNG(png, iconSize(smCxSmIcon, 16, dpi)); err != nil {
			return err
		}
		if big, err = iconFromPNG(png, iconSize(smCxIcon, 32, dpi)); err != nil {
			procDestroyIcon.Call(small)
			return err
		}
	}
	w.freeIcons()
	w.icons = [2]uintptr{small, big}
	if png == nil {
		small, big = w.b.icon, w.b.icon // the application's
	}
	const iconSmall, iconBig = 0, 1
	procSendMessageW.Call(w.hwnd, wmSetIcon, iconSmall, small)
	procSendMessageW.Call(w.hwnd, wmSetIcon, iconBig, big)
	return nil
}

func (w *window) freeIcons() {
	for _, icon := range w.icons {
		if icon != 0 {
			procDestroyIcon.Call(icon)
		}
	}
	w.icons = [2]uintptr{}
}

// The app's badge (Dock.SetBadge, App.SetBadgeCount) shows on its windows'
// taskbar buttons as their overlay icon: its text in white on a red disc,
// a small icon at the window's DPI, as the overlays of Teams and
// Chromium's web apps are. A count past 99 shows as 99+, which still
// reads in 16 pixels; screen readers read the whole text.

// badgeApply is applyBadge, linked when the app first sets a badge
// (LinkBadges): SetBadge, applyTaskbar and the window proc run in every
// app, and only call it.
var badgeApply func(*window)

// LinkBadges links the code of badges, when the app first sets one.
func LinkBadges() { badgeApply = (*window).applyBadge }

// badgeRed is the disc's color, a DIB pixel: Windows 11's critical fill
// (SystemFillColorCritical).
const badgeRed = 0xC42B1C

// applyBadge puts the app's badge on the window's taskbar button, or takes
// it off when there is none.
func (w *window) applyBadge() {
	tl := w.b.taskbar()
	if tl == 0 || w.skipTaskbar {
		return
	}
	var icon uintptr
	var desc *uint16
	if w.b.badge != "" {
		icon = badgeIcon(badgeText(w.b.badge), iconSize(smCxSmIcon, 16, dpiOf(w.hwnd)))
		desc = u16(w.b.badge)
	}
	hr := comCall(tl, tbSetOverlayIcon, w.hwnd, icon, uintptr(unsafe.Pointer(desc)))
	if !failed(hr) {
		w.badgeShown = icon != 0
	}
	if icon != 0 {
		procDestroyIcon.Call(icon) // the taskbar keeps a copy
	}
}

// badgeText returns what a badge shows of its label: a count past 99 as
// 99+.
func badgeText(label string) string {
	if len(label) > 2 && strings.Trim(label, "0123456789") == "" {
		return "99+"
	}
	return label
}

var (
	procCreateIconIndirect = user32.NewProc("CreateIconIndirect")
	procCreateBitmap       = gdi32.NewProc("CreateBitmap")
)

// iconInfo is ICONINFO.
type iconInfo struct {
	Icon        int32
	X, Y        uint32
	Mask, Color uintptr
}

// badgeIcon returns an icon of the badge's text, size pixels square, or 0.
func badgeIcon(text string, size int) uintptr {
	s := int32(size)
	bmp := newBitmap(0, s, s)
	if bmp == nil {
		return 0
	}
	defer bmp.free()
	drawBadge(bmp, text, s)
	// The color bitmap's alpha draws the icon; the mask is unused.
	mask, _, _ := procCreateBitmap.Call(uintptr(size), uintptr(size), 1, 1, 0)
	defer procDeleteObject.Call(mask)
	ii := iconInfo{Icon: 1, Mask: mask, Color: bmp.bmp}
	icon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	return icon
}

// drawBadge draws the badge's text in bmp, size pixels square, with
// straight alpha, as icons have it: semibold Segoe UI, centered on the
// disc by its digits' height, at the largest size up to three quarters of
// the disc that fits across it.
func drawBadge(bmp *bitmap, text string, s int32) {
	for i := range bmp.px {
		bmp.px[i] = badgeRed
	}
	const semibold, antialiased = 600, 4
	lf := logFont{Weight: semibold, Quality: antialiased}
	copy(lf.FaceName[:], utf16z("Segoe UI"))
	room := int32(math.Round(float64(s) * 0.84))
	str := u16(text)
	var font, old uintptr
	var r rect
	for em := s * 3 / 4; ; em-- {
		lf.Height = -em
		font, _, _ = procCreateFontIndirectW.Call(uintptr(unsafe.Pointer(&lf)))
		old, _, _ = procSelectObject.Call(bmp.dc, font)
		r = rect{}
		procDrawTextW.Call(bmp.dc, uintptr(unsafe.Pointer(str)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), dtCalcRect|dtSingleLine|dtNoPrefix)
		if r.Right-r.Left <= room || em <= s/3 {
			break
		}
		procSelectObject.Call(bmp.dc, old)
		procDeleteObject.Call(font)
	}
	tm := textMetricsOf(bmp.dc)
	// Digits stand on the baseline as tall as capitals, about 0.7 of the
	// em: centering that, rather than the line with its descent, centers
	// them.
	digits := 0.7 * float64(tm.Height-tm.InternalLeading)
	w := r.Right - r.Left
	x := (s - w) / 2
	y := int32(math.Round((float64(s)+digits)/2)) - tm.Ascent
	r = rect{x, y, x + w, y + tm.Height}
	procSetBkMode.Call(bmp.dc, 1) // TRANSPARENT
	procSetTextColor.Call(bmp.dc, 0xFFFFFF)
	procDrawTextW.Call(bmp.dc, uintptr(unsafe.Pointer(str)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), dtSingleLine|dtNoPrefix|dtNoClip)
	procSelectObject.Call(bmp.dc, old)
	procDeleteObject.Call(font)
	procGdiFlush.Call()
	disc := rect{0, 0, s, s}
	for y := range s {
		for x := range s {
			a := roundedCoverage(float64(x)+0.5, float64(y)+0.5, &disc, float64(s)/2)
			i := y*s + x
			bmp.px[i] = uint32(math.Round(a*255))<<24 | bmp.px[i]&0xFFFFFF
		}
	}
}

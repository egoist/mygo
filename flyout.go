package mygo

import (
	"fmt"

	"github.com/egoist/mygo/internal/platform"
)

// Placement is where a flyout goes from its anchor: the side, and how it
// lines up with the anchor along that side, as in "bottom-start", below
// the anchor with their left edges lined up.
type Placement string

// Placements.
const (
	PlacementBottomStart Placement = "bottom-start"
	PlacementBottom      Placement = "bottom"
	PlacementBottomEnd   Placement = "bottom-end"
	PlacementTopStart    Placement = "top-start"
	PlacementTop         Placement = "top"
	PlacementTopEnd      Placement = "top-end"
	PlacementRightStart  Placement = "right-start"
	PlacementRight       Placement = "right"
	PlacementRightEnd    Placement = "right-end"
	PlacementLeftStart   Placement = "left-start"
	PlacementLeft        Placement = "left"
	PlacementLeftEnd     Placement = "left-end"
)

var placements = map[Placement][2]uint8{
	"":                   {uint8(platform.SideBottom), uint8(platform.AlignStart)},
	PlacementBottomStart: {uint8(platform.SideBottom), uint8(platform.AlignStart)},
	PlacementBottom:      {uint8(platform.SideBottom), uint8(platform.AlignCenter)},
	PlacementBottomEnd:   {uint8(platform.SideBottom), uint8(platform.AlignEnd)},
	PlacementTopStart:    {uint8(platform.SideTop), uint8(platform.AlignStart)},
	PlacementTop:         {uint8(platform.SideTop), uint8(platform.AlignCenter)},
	PlacementTopEnd:      {uint8(platform.SideTop), uint8(platform.AlignEnd)},
	PlacementRightStart:  {uint8(platform.SideRight), uint8(platform.AlignStart)},
	PlacementRight:       {uint8(platform.SideRight), uint8(platform.AlignCenter)},
	PlacementRightEnd:    {uint8(platform.SideRight), uint8(platform.AlignEnd)},
	PlacementLeftStart:   {uint8(platform.SideLeft), uint8(platform.AlignStart)},
	PlacementLeft:        {uint8(platform.SideLeft), uint8(platform.AlignCenter)},
	PlacementLeftEnd:     {uint8(platform.SideLeft), uint8(platform.AlignEnd)},
}

// FlyoutOptions configures NewFlyout.
type FlyoutOptions struct {
	// Parent owns the flyout, which stays above it, follows it as it moves
	// and closes with it. Required.
	Parent *Window
	// Anchor is the rectangle the flyout is placed against, in DIPs
	// relative to the parent's content area: the box of the element
	// that opens it, as ui.Element.Bounds gives it, or a point (a
	// rectangle of zero size), such as the pointer's.
	Anchor Rectangle
	// Placement is where the flyout goes from Anchor (default
	// PlacementBottomStart). Where the display's work area has no room
	// for it there, the flyout flips to the other side of the anchor, or
	// to its other end, if that has room, then slides back into the work
	// area, then shrinks to fit it.
	Placement Placement
	// Gap is the distance between Anchor and the flyout, in DIPs.
	Gap int
	// Width and Height of the flyout in DIPs (default 200x200).
	Width, Height int
	// Focusable makes the flyout take the keyboard as it shows, for text
	// fields in it, and close as a menu does when the user presses outside
	// it, another window takes the keyboard or the app is deactivated:
	// OnClose listeners hear it, and may keep it open, except on Wayland,
	// whose compositor dismisses it. Without it, the flyout never takes
	// the keyboard, which stays in the parent, as for a tooltip or the
	// list of a combo box whose keys the parent handles, and stays until
	// it is closed.
	Focusable bool
	// Hidden creates the flyout without showing it.
	Hidden bool
	// Shadow gives the flyout the system's window shadow, which follows
	// the shape of what its content draws on macOS (macOS, Windows).
	Shadow bool
	// Popover gives the flyout the look of the system's popovers on macOS:
	// it shows in an NSPopover, which draws its material, rounded corners,
	// shadow and an arrow pointing at the middle of Anchor, and animates
	// as it opens and closes. AppKit places it on the side Placement
	// names, or the opposite one where the screen has no room: the rest
	// of Placement, and Gap, do not apply. As AppKit's popovers do, it has
	// the keyboard while it shows, Focusable or not, which makes it close
	// as the user clicks elsewhere. Its content draws no background there
	// (ui.Context.Vibrancy reports the material): elsewhere, where Popover
	// changes nothing, it draws its own.
	Popover bool

	// URL is loaded right after the flyout is created, as
	// WindowOptions.URL.
	URL string
	// Page configures the flyout's web page.
	Page PageOptions
	// Content makes the flyout show native UI instead of a web page, as
	// WindowOptions.Content.
	Content Content
}

// NewFlyout creates a flyout: a window without a frame, a background, a
// shadow or a taskbar button, owned by its parent and placed next to a
// rectangle of it, which may extend beyond the parent but stays in the
// work area of a display. Its page or native UI draws all of it: give the
// page a background, or the native UI's root one. Like NewWindow, it must
// be called after the application is ready.
func NewFlyout(opts FlyoutOptions) *Window {
	if opts.Parent == nil {
		panic("mygo: NewFlyout needs a Parent")
	}
	fo, err := opts.platform()
	if err != nil {
		panic(err)
	}
	return createWindow("NewFlyout", WindowOptions{
		URL:               opts.URL,
		Width:             or(opts.Width, 200),
		Height:            or(opts.Height, 200),
		UseContentSize:    true,
		Frameless:         true,
		Transparent:       true,
		DisableResize:     true,
		DisableMinimize:   true,
		DisableMaximize:   true,
		DisableFullScreen: true,
		DisableShadow:     !opts.Shadow,
		SkipTaskbar:       true,
		Parent:            opts.Parent,
		Page:              opts.Page,
		Content:           opts.Content,
		Hidden:            opts.Hidden,
	}, nil, fo)
}

func (o *FlyoutOptions) platform() (*platform.Flyout, error) {
	p, ok := placements[o.Placement]
	if !ok {
		return nil, fmt.Errorf("mygo: unknown flyout placement %q", o.Placement)
	}
	return &platform.Flyout{
		Anchor:    platform.Rect(o.Anchor),
		Side:      platform.Side(p[0]),
		Align:     platform.Align(p[1]),
		Gap:       o.Gap,
		Focusable: o.Focusable,
		Popover:   o.Popover,
	}, nil
}

// IsFlyout reports whether NewFlyout created the window.
func (w *Window) IsFlyout() bool { return w.flyout != nil }

// SetAnchor places a flyout against another rectangle of its parent, in
// DIPs relative to the parent's content area, as FlyoutOptions.Anchor. It
// does nothing for other windows.
func (w *Window) SetAnchor(anchor Rectangle) {
	onMain(func() {
		if w.flyout == nil || w.native == nil {
			return
		}
		w.flyout.Anchor = platform.Rect(anchor)
		w.placeFlyout()
	})
}

// placeFlyout places a flyout where it goes now. Main thread only.
func (w *Window) placeFlyout() {
	if w.flyout != nil && w.native != nil {
		w.native.PlaceFlyout(*w.flyout, w.flyoutSize)
	}
}

// placeFlyouts places the flyouts of a window that moved or resized
// again, which keeps them next to their anchors and in the work area.
// Main thread only.
func (w *Window) placeFlyouts() {
	for _, x := range Windows() {
		if x.parent == w && x.flyout != nil {
			x.placeFlyout()
		}
	}
}

// setFlyoutSize resizes a flyout, which SetSize and SetContentSize do
// alike, and places it again: it reports false for other windows. Main
// thread only.
func (w *Window) setFlyoutSize(width, height int) bool {
	if w.flyout == nil {
		return false
	}
	w.flyoutSize = platform.Size{Width: max(width, 1), Height: max(height, 1)}
	w.placeFlyout()
	return true
}

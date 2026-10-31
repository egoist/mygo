package mobile

import (
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/mobile/wire"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/transfer"
)

// surface is the drawing area of a window of native UI: a CAMetalLayer the
// Metal renderer draws into on iOS, an ANativeWindow an EGL context draws
// into on Android, which the host made (CreateWindow, SurfaceChanged).
// Frames come at the display's refreshes the content asks for
// (RequestFrame, Frame). A finger is the pointer while it touches the
// screen, and there is no pointer once it lifts (SurfaceEvent.Touch).
type surface struct {
	w      *window
	handle uintptr
	// refresh is how many times a second the display refreshes.
	refresh float64

	pending bool // a frame asked of the host
	gone    bool

	// The fingers on the screen, by the host's numbers: the first is the
	// pointer, two pinch.
	primary   int
	touching  bool
	fingers   map[int][2]float64
	pinchDist float64
	// lastTap is where and when the last tap ended, for double taps.
	lastTapX, lastTapY, lastTapTime float64
	taps                            int

	text     platform.TextInputState
	textOn   bool
	marked   string
	access   *platform.AccessTree
	accessOn bool
}

func newSurface(w *window) *surface {
	return &surface{w: w, refresh: 60, fingers: map[int][2]float64{}}
}

func (s *surface) send(ev platform.SurfaceEvent) bool {
	if s.gone {
		return false
	}
	return s.w.h.SurfaceEvent(ev)
}

// resized tells the content the window's new size, or that its insets
// changed, which moves what it lays out inside them.
func (s *surface) resized(size, insets bool) {
	if size {
		s.send(platform.SurfaceEvent{Kind: platform.SurfaceResize})
	}
	if size || insets {
		s.RequestFrame()
	}
}

func (s *surface) closed() {
	s.gone = true
}

func (s *surface) themeChanged() { s.RequestFrame() }

func (s *surface) idle() {}

// back hands Android's back to the content: Escape, which closes a menu
// or a dialog, then the Back key, as a mouse's back button, which a
// Router takes to go back. It reports whether the content took either.
func (s *surface) back() bool {
	for _, k := range []platform.Key{platform.KeyEscape, platform.KeyBack} {
		taken := s.send(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: k})
		s.send(platform.SurfaceEvent{Kind: platform.KeyReleased, Key: k})
		if taken {
			return true
		}
	}
	return false
}

// Native is the CAMetalLayer of iOS, the ANativeWindow of Android.
func (s *surface) Native() platform.SurfaceNative {
	if mapsSchemes { // Android
		return platform.SurfaceNative{Window: s.handle}
	}
	return platform.SurfaceNative{Layer: s.handle}
}

func (s *surface) Size() (float64, float64, float64) { return s.w.width, s.w.height, s.w.scale }

// Insets are those of the system's bars, the cutout and the keyboard: the
// content lays out above the keyboard while it shows.
func (s *surface) Insets() platform.Insets {
	in := s.w.insets
	in.Bottom = max(in.Bottom, s.w.keyboard)
	return in
}

func (s *surface) RequestFrame() {
	if s.pending || s.gone {
		return
	}
	s.pending = true
	s.w.b.call(&wire.RequestFrame{ID: s.w.id}, nil)
}

func (s *surface) RefreshRate() float64 { return s.refresh }

// frame draws the frame the content asked for, at a refresh.
func (s *surface) frame() {
	s.pending = false
	if s.gone || s.handle == 0 || s.w.width == 0 {
		return
	}
	s.send(platform.SurfaceEvent{Kind: platform.SurfaceFrame})
}

// PresentPixels has nothing to show frames drawn in memory on: phones
// draw with their GPUs.
func (s *surface) PresentPixels([]byte, int, int, int) {}

func (s *surface) SetCursor(c platform.Cursor) {}

// SetBackground tells the host the color the content's frames fill the
// window with, which the system's bars around it take on Android (its
// toolbar, its bottom navigation), for the screen to be of one color.
func (s *surface) SetBackground(c platform.Color) {
	if c == s.w.bg {
		return
	}
	s.w.bg = c
	s.w.b.call(&wire.SetBackground{ID: s.w.id, Color: rgba(c)}, nil)
}

// SetTextInput shows the keyboard while an element takes text, and gives
// the host the text around the caret, which the keyboard's suggestions
// and input methods read.
func (s *surface) SetTextInput(t platform.TextInputState) {
	if t.Active == s.text.Active && t.Caret == s.text.Caret && t.Text == s.text.Text && t.Start == s.text.Start && t.End == s.text.End {
		return
	}
	s.text = t
	if !t.Active {
		s.marked = ""
	}
	s.w.b.call(&wire.SetTextInput{ID: s.w.id, Active: t.Active, X: t.Caret.X, Y: t.Caret.Y, Width: t.Caret.W, Height: t.Caret.H,
		Kind: "text", Text: t.Text, SelStart: t.Start, SelEnd: t.End}, nil)
}

func (s *surface) UpdateAccessibility(tree *platform.AccessTree) {
	old := s.access
	s.access = tree
	if !s.accessOn {
		return
	}
	changed := old == nil || len(old.Nodes) != len(tree.Nodes) || old.Focus != tree.Focus
	if !changed {
		for i := range tree.Nodes {
			a, b := &old.Nodes[i], &tree.Nodes[i]
			if a.ID != b.ID || a.Label != b.Label || a.Value != b.Value || a.Bounds != b.Bounds || a.States != b.States {
				changed = true
				break
			}
		}
	}
	if changed {
		s.w.b.call(&wire.AccessibilityChanged{ID: s.w.id, Focus: int(tree.Focus)}, nil)
	}
	for _, a := range tree.Announcements {
		s.w.b.call(&wire.Announce{Text: a}, nil)
	}
}

func (s *surface) StartDataDrag(platform.DragRequest)        {}
func (s *surface) CancelDataDrag()                           {}
func (s *surface) SetDropFormats([]transfer.Format)          {}
func (s *surface) PlaceWebViews([]platform.WebViewPlacement) {}
func (s *surface) NewWebView(*platform.WindowOptions, platform.WindowHandler) (platform.WebView, error) {
	return nil, platform.ErrUnsupported
}

// touch turns a finger, a pen or a mouse into pointer events: the first
// finger is the pointer, a second pinches, and a tap soon after another
// near it counts as a double tap.
func (s *surface) touch(e *wire.TouchEvent) {
	if e.Kind != "touch" {
		s.mouse(e)
		return
	}
	switch e.Phase {
	case "down":
		s.fingers[e.Pointer] = [2]float64{e.X, e.Y}
		if len(s.fingers) == 2 {
			s.pinchDist = s.spread()
			if s.touching {
				// Pinching, not pressing: the press ends without a click.
				s.cancel()
			}
			return
		}
		if len(s.fingers) > 1 {
			return
		}
		s.primary, s.touching = e.Pointer, true
		if e.Time-s.lastTapTime < 0.3 && math.Hypot(e.X-s.lastTapX, e.Y-s.lastTapY) < 24 {
			s.taps++
		} else {
			s.taps = 1
		}
		s.send(platform.SurfaceEvent{Kind: platform.PointerMove, X: e.X, Y: e.Y, Touch: true})
		s.send(platform.SurfaceEvent{Kind: platform.PointerDown, X: e.X, Y: e.Y, Clicks: s.taps, Touch: true})
	case "move":
		if _, ok := s.fingers[e.Pointer]; !ok {
			return
		}
		s.fingers[e.Pointer] = [2]float64{e.X, e.Y}
		if len(s.fingers) == 2 {
			d := s.spread()
			if s.pinchDist > 0 && d > 0 {
				cx, cy := s.center()
				s.send(platform.SurfaceEvent{Kind: platform.PointerMove, X: cx, Y: cy, Touch: true})
				s.send(platform.SurfaceEvent{Kind: platform.PointerZoom, X: cx, Y: cy, Scale: d / s.pinchDist, Touch: true})
			}
			s.pinchDist = d
			return
		}
		if s.touching && e.Pointer == s.primary {
			s.send(platform.SurfaceEvent{Kind: platform.PointerMove, X: e.X, Y: e.Y, Touch: true})
		}
	case "up", "cancel":
		delete(s.fingers, e.Pointer)
		if len(s.fingers) < 2 {
			s.pinchDist = 0
		}
		if !s.touching || e.Pointer != s.primary {
			return
		}
		s.touching = false
		if e.Phase == "cancel" {
			s.cancel()
			return
		}
		s.send(platform.SurfaceEvent{Kind: platform.PointerUp, X: e.X, Y: e.Y, Clicks: s.taps, Touch: true})
		s.send(platform.SurfaceEvent{Kind: platform.PointerLeave, Touch: true})
		s.lastTapX, s.lastTapY, s.lastTapTime = e.X, e.Y, e.Time
	}
}

// cancel ends the finger's press without a click: it lets go far outside
// what it pressed.
func (s *surface) cancel() {
	s.touching = false
	s.send(platform.SurfaceEvent{Kind: platform.PointerUp, X: -1e6, Y: -1e6, Touch: true})
	s.send(platform.SurfaceEvent{Kind: platform.PointerLeave, Touch: true})
	s.taps = 0
}

func (s *surface) spread() float64 {
	var p [][2]float64
	for _, f := range s.fingers {
		p = append(p, f)
	}
	if len(p) < 2 {
		return 0
	}
	return math.Hypot(p[0][0]-p[1][0], p[0][1]-p[1][1])
}

func (s *surface) center() (float64, float64) {
	var x, y float64
	for _, f := range s.fingers {
		x += f[0]
		y += f[1]
	}
	n := float64(len(s.fingers))
	return x / n, y / n
}

// mouse handles a mouse's or a trackpad's pointer, as on an iPad with one,
// which hovers.
func (s *surface) mouse(e *wire.TouchEvent) {
	button := 0
	if e.Buttons&2 != 0 {
		button = 1
	}
	switch e.Phase {
	case "down":
		s.send(platform.SurfaceEvent{Kind: platform.PointerDown, X: e.X, Y: e.Y, Button: button, Clicks: 1})
	case "up":
		s.send(platform.SurfaceEvent{Kind: platform.PointerUp, X: e.X, Y: e.Y, Button: button, Clicks: 1})
	case "cancel":
		s.send(platform.SurfaceEvent{Kind: platform.PointerLeave})
	default:
		s.send(platform.SurfaceEvent{Kind: platform.PointerMove, X: e.X, Y: e.Y})
	}
}

// keys maps the names the hosts give a hardware keyboard's keys.
var keys = map[string]platform.Key{
	"Enter": platform.KeyEnter, "Escape": platform.KeyEscape, "Backspace": platform.KeyBackspace, "Tab": platform.KeyTab,
	"Space": platform.KeySpace, "Delete": platform.KeyDelete, "Home": platform.KeyHome, "End": platform.KeyEnd,
	"PageUp": platform.KeyPageUp, "PageDown": platform.KeyPageDown, "ArrowLeft": platform.KeyLeft,
	"ArrowRight": platform.KeyRight, "ArrowUp": platform.KeyUp, "ArrowDown": platform.KeyDown,
}

func keyOf(name string) platform.Key {
	if k, ok := keys[name]; ok {
		return k
	}
	if len(name) == 1 {
		c := name[0] | 0x20
		switch {
		case c >= 'a' && c <= 'z':
			return platform.KeyA + platform.Key(c-'a')
		case name[0] >= '0' && name[0] <= '9':
			return platform.Key0 + platform.Key(name[0]-'0')
		}
	}
	return platform.KeyUnknown
}

// typed hands the keyboard's text to the content: a new line or a tab as
// its key, which submits a field or moves the focus, other text as typed.
func (s *surface) typed(text string) {
	s.marked = ""
	switch text {
	case "\n":
		s.key(platform.KeyEnter)
	case "\t":
		s.key(platform.KeyTab)
	default:
		s.send(platform.SurfaceEvent{Kind: platform.TextInput, Text: text})
	}
}

func (s *surface) key(k platform.Key) {
	s.send(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: k, PhysicalKey: k})
	s.send(platform.SurfaceEvent{Kind: platform.KeyReleased, Key: k, PhysicalKey: k})
}

// textState is the text around the caret, for the keyboard.
func (s *surface) textState() wire.TextStateReply {
	t := s.text
	r := []rune(t.Text)
	start, end := max(0, min(t.Start, len(r))), max(0, min(t.End, len(r)))
	if start > end {
		start, end = end, start
	}
	return wire.TextStateReply{Before: string(r[:start]), Selected: string(r[start:end]), After: string(r[end:]),
		Marked: utf8.RuneCountInString(s.marked), CaretX: t.Caret.X, CaretY: t.Caret.Y, CaretHeight: t.Caret.H}
}

// accessTree encodes the content's accessible elements for the host,
// asking the content for them first if it was not giving them yet.
func (s *surface) accessTree() []byte {
	if !s.accessOn {
		s.accessOn = true
		// The content gives its tree at once (UpdateAccessibility).
		s.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	}
	var w wire.Writer
	if s.access == nil {
		return w.Buf
	}
	for i := range s.access.Nodes {
		n := &s.access.Nodes[i]
		w.Int(int(n.ID))
		parent := 0
		if n.Parent >= 0 && n.Parent < len(s.access.Nodes) {
			parent = int(s.access.Nodes[n.Parent].ID)
		}
		w.Int(parent)
		w.Int(int(n.Role))
		w.String(n.Label)
		value := n.Value
		if n.Role.Ranged() && value == "" && n.Max > n.Min {
			value = strconv.Itoa(int(math.Round(100*(n.Now-n.Min)/(n.Max-n.Min)))) + "%"
		}
		w.String(value)
		w.String(n.Description)
		w.Float(n.Bounds.X)
		w.Float(n.Bounds.Y)
		w.Float(n.Bounds.W)
		w.Float(n.Bounds.H)
		w.Int(int(n.States))
		w.Int(int(n.Actions))
		w.Int(n.Level)
	}
	return w.Buf
}

// accessAction performs an assistive technology's action on an element.
func (s *surface) accessAction(node int, action string) bool {
	kinds := map[string]platform.AccessActionKind{
		"press": platform.AccessPress, "focus": platform.AccessFocus, "increment": platform.AccessIncrement,
		"decrement": platform.AccessDecrement, "scrollIntoView": platform.AccessScrollIntoView,
		"expand": platform.AccessExpand, "collapse": platform.AccessCollapse,
	}
	k, ok := kinds[action]
	if !ok {
		return false
	}
	s.send(platform.SurfaceEvent{Kind: platform.AccessAction, ID: uint64(node), Action: k})
	return true
}

// handleSurface handles the events of surfaces, and reports whether op
// was one.
func (b *Backend) handleSurface(op uint64, r *wire.Reader, w *wire.Writer) bool {
	sf := func(id int) *surface {
		if win := b.windows[id]; win != nil {
			return win.s
		}
		return nil
	}
	switch op {
	case wire.EventFrame:
		var e wire.FrameEvent
		e.Decode(r)
		if s := sf(e.ID); s != nil {
			s.frame()
		}
	case wire.EventSurfaceChanged:
		var e wire.SurfaceChangedEvent
		e.Decode(r)
		if s := sf(e.ID); s != nil {
			old := s.handle
			s.handle = uintptr(e.Handle)
			if e.Refresh > 0 {
				s.refresh = e.Refresh
			}
			if old != 0 && s.handle != old {
				s.send(platform.SurfaceEvent{Kind: platform.SurfaceRenew})
			}
			if s.handle != 0 {
				s.RequestFrame()
			}
		}
	case wire.EventSurfaceDestroyed:
		var e wire.SurfaceDestroyedEvent
		e.Decode(r)
		if s := sf(e.ID); s != nil && s.handle != 0 {
			s.handle = 0
			// The content lets go of its renderer at once.
			s.send(platform.SurfaceEvent{Kind: platform.SurfaceRenew})
		}
	case wire.EventTouch:
		var e wire.TouchEvent
		e.Decode(r)
		if s := sf(e.ID); s != nil {
			s.touch(&e)
		}
	case wire.EventScroll:
		var e wire.ScrollEvent
		e.Decode(r)
		if s := sf(e.ID); s != nil {
			s.send(platform.SurfaceEvent{Kind: platform.PointerScroll, X: e.X, Y: e.Y, DX: e.DX, DY: e.DY,
				Precise: e.Precise, Momentum: e.Phase == "momentum"})
		}
	case wire.EventKey:
		var e wire.KeyEvent
		e.Decode(r)
		reply := wire.KeyReply{}
		if s := sf(e.ID); s != nil {
			kind := platform.KeyPressed
			if !e.Down {
				kind = platform.KeyReleased
			}
			k := keyOf(e.Key)
			reply.Handled = s.send(platform.SurfaceEvent{Kind: kind, Key: k, PhysicalKey: k, Mods: platform.Modifiers(e.Mods), Repeat: e.Repeated})
		}
		reply.Encode(w)
	case wire.EventTextInsert:
		var e wire.TextInsertEvent
		e.Decode(r)
		if s := sf(e.ID); s != nil {
			s.typed(e.Text)
		}
	case wire.EventTextMarked:
		var e wire.TextMarkedEvent
		e.Decode(r)
		if s := sf(e.ID); s != nil {
			s.marked = e.Text
			s.send(platform.SurfaceEvent{Kind: platform.TextComposition, Text: e.Text, Caret: e.SelEnd})
		}
	case wire.EventTextDelete:
		var e wire.TextDeleteEvent
		e.Decode(r)
		if s := sf(e.ID); s != nil {
			for range e.Before {
				s.key(platform.KeyBackspace)
			}
			for range e.After {
				s.key(platform.KeyDelete)
			}
		}
	case wire.EventTextCommand:
		var e wire.TextCommandEvent
		e.Decode(r)
		if s := sf(e.ID); s != nil {
			switch e.Command {
			case "newline":
				s.key(platform.KeyEnter)
			case "tab":
				s.key(platform.KeyTab)
			case "left", "right", "up", "down":
				s.key(map[string]platform.Key{"left": platform.KeyLeft, "right": platform.KeyRight, "up": platform.KeyUp, "down": platform.KeyDown}[e.Command])
			default:
				s.send(platform.SurfaceEvent{Kind: platform.SurfaceCommand, Text: e.Command})
			}
		}
	case wire.EventTextState:
		var e wire.TextStateEvent
		e.Decode(r)
		reply := wire.TextStateReply{}
		if s := sf(e.ID); s != nil {
			reply = s.textState()
		}
		reply.Encode(w)
	case wire.EventTextSelect, wire.EventTextInputEnded:
		// The content keeps its selection, and the focus, as the keyboard
		// goes: a tap elsewhere moves them.
	case wire.EventMenuChosen:
		var e wire.MenuChosenEvent
		e.Decode(r)
		if s := sf(e.ID); s != nil && e.Action != "" {
			s.send(platform.SurfaceEvent{Kind: platform.SurfaceCommand, Text: e.Action})
		}
	case wire.EventAccessibilityTree:
		var e wire.AccessibilityTreeEvent
		e.Decode(r)
		reply := wire.AccessibilityTreeReply{}
		if s := sf(e.ID); s != nil {
			reply.Nodes = s.accessTree()
		}
		reply.Encode(w)
	case wire.EventAccessibilityAction:
		var e wire.AccessibilityActionEvent
		e.Decode(r)
		reply := wire.AccessibilityActionReply{}
		if s := sf(e.ID); s != nil {
			reply.Handled = s.accessAction(e.Node, e.Action)
		}
		reply.Encode(w)
	default:
		return false
	}
	return true
}

package ui

import (
	"fmt"
	"hash/maphash"
	"math"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/text"
)

// The inspector shows the elements of a window's frames in a panel beside
// the content, as a browser's developer tools show a page's: their tree,
// the box, layout and style of the one chosen in the tree or picked in the
// window, which it outlines over the content, the time frames take, and
// warnings, as duplicate keys. Windows whose DevTools are on open it with
// the Toggle Developer Tools menu item, F12, or Alt+Cmd+I (Ctrl+Shift+I
// outside macOS).
//
// The panel is built with the view's elements, as the last child of the
// root, which the content leaves room for: the content's root is narrower
// by the panel's width. It shows the elements of the frame before, which
// the inspector notes after each frame is laid out, asking for another
// frame when they changed.

// inspectorWidth is the width of the inspector's panel, at most half the
// window's.
const inspectorWidth = 380

// inspectorKey keys the panel under the root.
const inspectorKey = "mygo.inspector"

type inspector struct {
	// enabled tells that the window's DevTools let the inspector open.
	enabled bool
	open    bool
	// picking is set while the pointer picks an element in the content,
	// and swallow while the release of the press that picked it goes
	// nowhere.
	picking, swallow bool
	// selected is the element chosen, hovered the element under the
	// pointer in the tree, or in the content while picking; reveal scrolls
	// the tree to the element chosen.
	selected, hovered uint64
	reveal            bool
	// source is where the app built the element chosen.
	source string
	// panel is the ID of the panel, appW the width it leaves the content.
	panel uint64
	appW  float32
	// nodes are the elements of the last frame, in the order of the tree,
	// and props the details of the element chosen; sum hashes them, and
	// shown is the sum the panel was built from. changed asks for a panel
	// built anew, as warnings come.
	nodes   []inspNode
	seen    map[uint64]bool
	props   []inspProp
	sum     uint64
	shown   uint64
	changed bool
	// rows are the nodes the tree shows, inside no collapsed node.
	rows      []int32
	collapsed map[uint64]bool
	list      ListState
	// selElem and hoverElem are the elements of the frame being painted
	// with those IDs.
	selElem, hoverElem *Element
	// times are how long the last frame took to build, lay out and paint,
	// counted from lapAt; elements is how many it had.
	times    [3]time.Duration
	lapAt    time.Time
	elements int
	theme    Theme
}

// inspNode is an element of the tree. key keys its row: the element's ID,
// unless another element had it (a duplicate key).
type inspNode struct {
	id, key uint64
	depth   int32
	kids    bool
	name    string
	desc    string
	w, h    float32
	// leaving marks a copy of an element going (Transition.Exit).
	leaving bool
}

// inspProp is a detail of the element chosen.
type inspProp struct{ name, value string }

// contentWidth returns the width the window w wide leaves the content.
func (in *inspector) contentWidth(w float32) float32 {
	if !in.open {
		in.appW = w
		return w
	}
	in.appW = max(w-min(inspectorWidth, w/2), 0)
	return in.appW
}

// lap notes how long the frame's phase took since the last lap (-1 starts
// the frame).
func (in *inspector) lap(phase int) {
	if !in.open {
		return
	}
	now := time.Now()
	if phase >= 0 {
		in.times[phase] = now.Sub(in.lapAt)
	}
	in.lapAt = now
}

// toggleInspector opens or closes the inspector.
func (rt *engine) toggleInspector() {
	in := &rt.insp
	in.open = !in.open
	in.picking, in.hovered = false, 0
	if !in.open {
		in.nodes, in.props, in.rows = nil, nil, nil
	}
	rt.requestFrame()
}

// inspectKey toggles the inspector for its keys, and stops picking for
// Escape.
func (rt *engine) inspectKey(mods Modifiers, key Key) bool {
	in := &rt.insp
	if in.picking && key == KeyEscape && mods == 0 {
		in.picking, in.hovered = false, 0
		rt.requestFrame()
		return true
	}
	if !in.enabled {
		return false
	}
	mac := runtime.GOOS == "darwin"
	if key == KeyF12 && mods == 0 || key == KeyI && (mac && mods == Super|Alt || !mac && mods == Ctrl|Shift) {
		rt.toggleInspector()
		return true
	}
	return false
}

// noteSource notes where the app builds the element chosen.
func (in *inspector) noteSource() { in.source = callSite() }

// pointer takes the pointer over the content while picking: moving
// outlines the element under it, and a press chooses it. It reports
// whether it took ev.
func (in *inspector) pointer(rt *engine, ev platform.SurfaceEvent, x, y float32) bool {
	switch {
	case in.swallow && ev.Kind == platform.PointerUp:
		in.swallow = false
		return true
	case !in.picking || x >= in.appW:
		return false
	}
	switch ev.Kind {
	case platform.PointerMove:
		var id uint64
		if chain := rt.hitChain(x, y); len(chain) > 0 {
			id = chain[0]
		}
		if id != in.hovered {
			in.hovered = id
			rt.requestFrame()
		}
		return true
	case platform.PointerDown:
		if chain := rt.hitChain(x, y); len(chain) > 0 {
			in.choose(chain[0])
			in.reveal = true
		}
		in.picking, in.hovered, in.swallow = false, 0, true
		rt.requestFrame()
		return true
	case platform.PointerUp:
		return true
	}
	return false
}

// choose chooses the element id.
func (in *inspector) choose(id uint64) {
	if id != in.selected {
		in.selected, in.source = id, ""
	}
}

// snapshot notes the elements of the frame laid out under root, and asks
// for another frame when the panel showed others.
func (in *inspector) snapshot(rt *engine, root *Element) {
	in.nodes = in.nodes[:0]
	in.selElem, in.hoverElem = nil, nil
	if in.seen == nil {
		in.seen = map[uint64]bool{}
	}
	clear(in.seen)
	var sum uint64
	in.walk(root, 0, &sum)
	in.elements = len(in.nodes)
	in.props = in.props[:0]
	if e := in.selElem; e != nil {
		in.props = describe(rt, e, in.props)
	}
	for _, p := range in.props {
		sum = mix(sum, maphash.String(keySeed, p.value))
	}
	sum = mix(sum, uint64(len(rt.warnings)))
	sum = mix(sum, maphash.String(keySeed, in.source))
	in.sum = sum
	if in.sum != in.shown || in.changed {
		rt.animating = true
	}
}

func (in *inspector) walk(e *Element, depth int32, sum *uint64) {
	if e.id == in.panel && e.parent != nil && e.parent.parent == nil {
		return
	}
	name, desc := describeNode(e)
	i := len(in.nodes)
	key := e.id
	if in.seen[key] {
		key = mix(key, uint64(i))
	}
	in.seen[key] = true
	in.nodes = append(in.nodes, inspNode{id: e.id, key: key, depth: depth, name: name, desc: desc, w: e.w, h: e.h, leaving: e.leaving != 0})
	*sum = mix(*sum, e.id^uint64(depth)<<56)
	*sum = mix(*sum, uint64(math.Float32bits(e.x))<<32|uint64(math.Float32bits(e.y)))
	*sum = mix(*sum, uint64(math.Float32bits(e.w))<<32|uint64(math.Float32bits(e.h)))
	if desc != "" {
		*sum = mix(*sum, maphash.String(keySeed, desc))
	}
	if e.id == in.selected {
		in.selElem = e
	}
	if e.id == in.hovered {
		in.hoverElem = e
	}
	for c := e.first; c != nil; c = c.next {
		in.walk(c, depth+1, sum)
	}
	in.nodes[i].kids = len(in.nodes)-1 > i
}

// describeNode returns what the tree calls an element, and a few words
// about it: its text or label.
func describeNode(e *Element) (name, desc string) {
	name = elementName(e)
	switch {
	case e.kind == kindText && e.text != "":
		desc = strconv.Quote(clip(e.text, 60))
	case e.label != "":
		desc = clip(e.label, 60)
	}
	return name, desc
}

// clip returns s cut to n runes, on one line.
func clip(s string, n int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i] + "…"
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

// elementName returns the widget an element is, else its role, else its
// kind.
func elementName(e *Element) string {
	if e.widget != "" {
		return e.widget
	}
	if n := roleName(e.role); n != "" {
		return n
	}
	switch {
	case e.flags&flagEditable != 0:
		return "TextInput"
	case e.role == RoleAuto && e.flags&flagClickable != 0 && e.flags&flagFocusable != 0:
		return "Button" // as assistive technology sees it
	}
	switch e.kind {
	case kindText:
		return "Text"
	case kindImage:
		return "Image"
	case kindIcon:
		return "Icon"
	case kindInput:
		return "Input"
	}
	switch {
	case e.parent == nil:
		return "Root"
	case e.id == overlayID:
		return "Overlay"
	case e.grid:
		return "Grid"
	case e.flags&(flagScrollX|flagScrollY) != 0:
		return "Scroll"
	case e.row:
		return "Row"
	}
	return "Column"
}

func roleName(r Role) string {
	switch r {
	case RoleButton:
		return "Button"
	case RoleLink:
		return "Link"
	case RoleCheckBox:
		return "Checkbox"
	case RoleRadio:
		return "Radio"
	case RoleSwitch:
		return "Switch"
	case RoleSlider:
		return "Slider"
	case RoleProgress:
		return "Progress"
	case RoleTextField:
		return "TextField"
	case RoleList:
		return "List"
	case RoleDialog:
		return "Dialog"
	case RoleAlertDialog:
		return "AlertDialog"
	case RolePopup:
		return "Popup"
	case RoleTooltip:
		return "Tooltip"
	case RolePopUpButton:
		return "Select"
	case RoleTabList:
		return "Tabs"
	case RoleTab:
		return "Tab"
	case RoleSplitter:
		return "Splitter"
	case RoleStatus:
		return "Status"
	case RoleTable:
		return "Table"
	case RoleRow:
		return "TableRow"
	case RoleCell:
		return "Cell"
	case RoleColumnHeader:
		return "ColumnHeader"
	case RoleTree:
		return "Tree"
	case RoleTreeItem:
		return "TreeItem"
	case RoleListItem:
		return "ListItem"
	case RoleMenuButton:
		return "MenuButton"
	case RoleToolbar:
		return "Toolbar"
	case RoleRadioGroup:
		return "RadioGroup"
	case RoleToggleButton:
		return "Toggle"
	case RoleComboBox:
		return "Combobox"
	case RoleDisclosure:
		return "Disclosure"
	case RoleMeter:
		return "Meter"
	case RoleStepper:
		return "Stepper"
	case RoleColorWell:
		return "ColorWell"
	}
	return ""
}

// describe appends the details of e to props.
func describe(rt *engine, e *Element, props []inspProp) []inspProp {
	add := func(name, value string) { props = append(props, inspProp{name, value}) }
	add("Element", elementName(e))
	if rt.insp.source != "" {
		add("Built at", rt.insp.source)
	}
	add("ID", fmt.Sprintf("%016x", e.id))
	add("Box", fmt.Sprintf("%s × %s at %s, %s", num(e.w), num(e.h), num(e.x), num(e.y)))
	if cw, ch := e.w-e.padX(), e.h-e.padY(); cw != e.w || ch != e.h {
		add("Content", fmt.Sprintf("%s × %s", num(max(cw, 0)), num(max(ch, 0))))
	}
	if v, ok := edgesText(e.pad); ok {
		add("Padding", v)
	}
	if v, ok := edgesText(e.border); ok {
		add("Border", v+" "+colorText(e.borderC))
	}
	if v, ok := edgesText(e.margin); ok {
		add("Margin", v)
	}
	if e.kind == kindBox {
		switch {
		case e.grid:
			add("Layout", fmt.Sprintf("grid, %d columns, gap %s %s", len(e.cols), num(e.gapX), num(e.gapY)))
		case e.list != nil:
			add("Layout", fmt.Sprintf("list of %d rows", e.list.n))
		default:
			dir := "column"
			if e.row {
				dir = "row"
			}
			if e.reverse {
				dir += ", reversed"
			}
			if e.wrap {
				dir += ", wrapping"
			}
			if e.gapX != 0 || e.gapY != 0 {
				dir += ", gap " + num(e.gapX)
				if e.gapY != e.gapX {
					dir += " " + num(e.gapY)
				}
			}
			if e.justify != alignAuto && e.justify != Start {
				dir += ", justify " + alignText(e.justify)
			}
			if e.align != alignAuto {
				dir += ", align " + alignText(e.align)
			}
			add("Layout", dir)
		}
	}
	size := lengthText(e.width) + " × " + lengthText(e.height)
	if e.minW.u != unitAuto || e.minH.u != unitAuto {
		size += ", min " + lengthText(e.minW) + " × " + lengthText(e.minH)
	}
	if e.maxW.u != unitAuto || e.maxH.u != unitAuto {
		size += ", max " + lengthText(e.maxW) + " × " + lengthText(e.maxH)
	}
	add("Size", size)
	if e.grow != 0 || e.shrink != 1 || e.basis.u != unitAuto {
		add("Flex", fmt.Sprintf("grow %s, shrink %s, basis %s", num(e.grow), num(e.shrink), lengthText(e.basis)))
	}
	if e.self != alignAuto {
		add("Align self", alignText(e.self))
	}
	switch {
	case e.attach != 0:
		at, self := e.attach.anchors()
		add("Position", "attached, its "+anchorText(self)+" on the parent's "+anchorText(at))
	case e.flags&flagAbsolute != 0:
		add("Position", "absolute "+insetsText(e.inset))
	case e.inset != [4]length{}:
		add("Position", "moved "+insetsText(e.inset))
	}
	if e.kind == kindText || e.kind == kindIcon {
		ts := e.resolvedText()
		weight := ts.weight
		if weight == 0 {
			weight = 400
		}
		family := ts.family
		if family == "" {
			family = "system-ui"
		}
		font := fmt.Sprintf("%s %s, weight %d", family, num(ts.size), weight)
		if ts.italic {
			font += ", italic"
		}
		add("Font", font)
		add("Color", colorText(ts.color))
	}
	if e.kind == kindBox && e.bg.A > 0 && e.fill == fillColor {
		add("Background", colorText(e.bg))
	}
	if e.radius != [4]float32{} {
		v, _ := edgesText(e.radius)
		add("Radius", v)
	}
	if e.opacitySet && e.opacity < 1 {
		add("Opacity", num(e.opacity))
	}
	var flags []string
	for _, f := range []struct {
		flag uint32
		name string
	}{
		{flagFocusable, "focusable"}, {flagClickable, "clickable"}, {flagEditable, "editable"},
		{flagSelectable, "selectable"}, {flagScrollX | flagScrollY, "scrolls"}, {flagClipX | flagClipY, "clips"},
		{flagDisabled, "disabled"}, {flagInvisible, "invisible"}, {flagInert, "inert"}, {flagPassThrough, "passes the pointer"},
	} {
		if e.flags&f.flag != 0 {
			flags = append(flags, f.name)
		}
	}
	if len(flags) > 0 {
		add("Flags", strings.Join(flags, ", "))
	}
	var st []string
	if rt.focused == e.id {
		st = append(st, "focused")
	}
	for _, id := range rt.hover {
		if id == e.id {
			st = append(st, "under the pointer")
			break
		}
	}
	if e.st.pressed {
		st = append(st, "pressed")
	}
	if e.leaving != 0 {
		st = append(st, "leaving")
	} else if r := e.st.trec; r != nil && r.built == rt.frame && r.elem == e && (r.moving != 0 || r.colorMoving) {
		st = append(st, "moving")
	}
	if len(st) > 0 {
		add("State", strings.Join(st, ", "))
	}
	if e.scrolls() {
		add("Scroll", fmt.Sprintf("%s, %s of %s × %s", num(float32(e.st.scrollX)), num(float32(e.st.scrollY)), num(float32(e.contentW)), num(float32(e.contentH))))
	}
	return props
}

// num formats a size: whole, or with one decimal.
func num(v float32) string {
	if v == float32(math.Round(float64(v))) {
		return strconv.Itoa(int(v))
	}
	return strconv.FormatFloat(float64(v), 'f', 1, 32)
}

func lengthText(l length) string {
	switch l.u {
	case unitPx:
		return num(l.v)
	case unitPercent:
		return num(l.v) + "%"
	}
	return "auto"
}

// edgesText returns the four values top, right, bottom and left as CSS
// writes them, and whether one is not zero.
func edgesText(v [4]float32) (string, bool) {
	if v == [4]float32{} {
		return "", false
	}
	f := func(x float32) string {
		if isAuto(x) {
			return "auto"
		}
		return num(x)
	}
	switch {
	case v[0] == v[1] && v[1] == v[2] && v[2] == v[3]:
		return f(v[0]), true
	case v[0] == v[2] && v[1] == v[3]:
		return f(v[0]) + " " + f(v[1]), true
	}
	return f(v[0]) + " " + f(v[1]) + " " + f(v[2]) + " " + f(v[3]), true
}

func insetsText(v [4]length) string {
	var parts []string
	for i, side := range [4]string{"top", "right", "bottom", "left"} {
		if v[i].u != unitAuto {
			parts = append(parts, side+" "+lengthText(v[i]))
		}
	}
	return strings.Join(parts, ", ")
}

func alignText(a Align) string {
	switch a {
	case Start:
		return "start"
	case Center:
		return "center"
	case End:
		return "end"
	case Stretch:
		return "stretch"
	case SpaceBetween:
		return "space between"
	case SpaceAround:
		return "space around"
	case SpaceEvenly:
		return "space evenly"
	}
	return "auto"
}

func anchorText(a Anchor) string {
	return [...]string{"top left", "top", "top right", "left", "center", "right", "bottom left", "bottom", "bottom right"}[min(int(a), 8)]
}

func colorText(c Color) string {
	if c.A == 255 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A)
}

// buildInspector builds the panel, right of the content appW wide, in a
// window w×h.
func (rt *engine) buildInspector(c *Context, appW, w, h float32) {
	in := &rt.insp
	in.shown = in.sum
	in.changed = false
	if in.collapsed == nil {
		in.collapsed = map[uint64]bool{}
	}
	// The panel looks the same whatever theme the view set.
	in.theme = rt.theme
	t := &in.theme
	saved, savedParent := c.theme, c.parent
	c.theme, c.parent = t, c.root
	defer func() { c.theme, c.parent = saved, savedParent }()

	// The rows of the tree: the nodes inside no collapsed node.
	in.rows = in.rows[:0]
	hide := int32(-1)
	sel := -1
	for i, n := range in.nodes {
		if hide >= 0 && n.depth > hide {
			continue
		}
		hide = -1
		if n.id == in.selected {
			sel = len(in.rows)
		}
		in.rows = append(in.rows, int32(i))
		if n.kids && in.collapsed[n.id] {
			hide = n.depth
		}
	}
	if in.reveal && in.selected != 0 {
		in.reveal = false
		if sel < 0 && in.expandTo(in.selected) {
			// Shown once the nodes around it open.
			in.reveal = true
			rt.animating = true
		} else if sel >= 0 {
			in.list.ScrollIntoView(sel)
		}
	}

	panel := Column(c).Key(inspectorKey)
	in.panel = panel.id
	panel.Absolute().Left(appW).Top(0).Width(w-appW).Height(h).
		Background(t.Background).BorderWidth(0, 0, 0, 1).BorderColor(t.Border).
		FontSize(12).Label("Inspector")
	panel.Children(func() {
		Row(c).Padding(8, 8, 6, 10).Gap(6).AlignItems(Center).Children(func() {
			Text(c, "Elements").Bold()
			Spacer(c)
			pick := inspectorButton(c, t, "Pick", in.picking)
			if pick.Clicked() {
				in.picking, in.hovered = !in.picking, 0
			}
			if inspectorButton(c, t, "Close", false).Clicked() {
				rt.toggleInspector()
			}
		})
		total := in.times[0] + in.times[1] + in.times[2]
		Text(c, fmt.Sprintf("%s ms: build %s, layout %s, paint %s · %d elements",
			ms(total), ms(in.times[0]), ms(in.times[1]), ms(in.times[2]), in.elements)).
			FontSize(11).TextColor(t.TextMuted).PaddingX(10).SingleLine().Ellipsis("…")
		in.list.Key = func(i int) any { return in.nodes[in.rows[i]].key }
		hovered := uint64(0)
		List(c, &in.list, len(in.rows), func(i int) {
			n := &in.nodes[in.rows[i]]
			row := Row(c).Height(22).PaddingX(6).Gap(4).AlignItems(Center).Radius(4)
			if row.Hovered() {
				hovered = n.id
			}
			if row.Clicked() {
				in.choose(n.id)
			}
			switch {
			case n.id == in.selected:
				row.Background(t.Selection)
			case n.id == in.hovered:
				row.Background(t.SurfaceHover)
			}
			row.Children(func() {
				Box(c).Width(float32(n.depth) * 12).Shrink(0)
				if !n.kids {
					Box(c).Width(12).Shrink(0)
				} else {
					arrow := "▾"
					if in.collapsed[n.id] {
						arrow = "▸"
					}
					if Text(c, arrow).Width(12).Shrink(0).TextColor(t.TextMuted).Clicked() {
						in.collapsed[n.id] = !in.collapsed[n.id]
					}
				}
				name := Text(c, n.name).FontWeight(600).Shrink(0)
				if n.leaving {
					name.TextColor(t.TextMuted)
				}
				if n.desc != "" {
					Text(c, n.desc).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				}
				Spacer(c)
				Text(c, num(n.w)+" × "+num(n.h)).FontSize(11).TextColor(t.TextMuted).Shrink(0)
			})
		}).Grow(1).MinHeight(0).PaddingX(4).Label("Elements")
		if !in.picking {
			in.hovered = hovered
		}
		Divider(c)
		Scroll(c).Height(min(300, h*0.42)).Shrink(0).Padding(8, 10).Gap(3).Children(func() {
			if len(in.props) == 0 {
				Text(c, "Choose an element in the tree, or pick one in the window.").TextColor(t.TextMuted)
			}
			for _, p := range in.props {
				Row(c).Gap(8).Children(func() {
					Text(c, p.name).TextColor(t.TextMuted).Width(76).Shrink(0)
					Text(c, p.value).Font("monospace").FontSize(11).Grow(1).MinWidth(0)
				})
			}
			if n := len(rt.warnings); n > 0 {
				Text(c, fmt.Sprintf("Warnings (%d)", n)).Bold().MarginY(6)
				for i := n - 1; i >= 0; i-- {
					Text(c, rt.warnings[i]).TextColor(t.Danger).FontSize(11)
				}
			}
		}).Label("Details")
	})
}

// expandTo opens the nodes around the node id, and reports whether it
// opened one.
func (in *inspector) expandTo(id uint64) bool {
	i := -1
	for j := range in.nodes {
		if in.nodes[j].id == id {
			i = j
			break
		}
	}
	if i < 0 {
		return false
	}
	opened := false
	depth := in.nodes[i].depth
	for j := i - 1; j >= 0 && depth > 0; j-- {
		if n := in.nodes[j]; n.depth < depth {
			if in.collapsed[n.id] {
				delete(in.collapsed, n.id)
				opened = true
			}
			depth = n.depth
		}
	}
	return opened
}

// inspectorButton creates a small button of the panel, which takes no
// focus, so that it does not take it from the content.
func inspectorButton(c *Context, t *Theme, label string, on bool) *Element {
	b := Box(c).PaddingX(8).PaddingY(3).Radius(5).Border(1, t.Border).Cursor(CursorPointer).Label(label).Role(RoleButton)
	b.Children(func() { Text(c, label) })
	switch {
	case on:
		b.Background(t.Accent).TextColor(t.AccentText).BorderColor(t.Accent)
	case b.Hovered():
		b.Background(t.SurfaceHover)
	}
	return b
}

func ms(d time.Duration) string { return strconv.FormatFloat(float64(d)/1e6, 'f', 1, 64) }

// paintHighlight outlines over the content the element under the pointer
// in the tree or picking, with its margins in orange, its border and
// padding in green and its content in blue, as browsers do, and else the
// element chosen.
func (in *inspector) paintHighlight(p *Painter, h float32) {
	savedClip, savedOpacity := p.clip, p.opacity
	p.pushClip(Rect{0, 0, in.appW, h}, [4]float32{})
	p.opacity = 1
	defer func() {
		p.popClip()
		p.clip, p.opacity = savedClip, savedOpacity
	}()
	e := in.hoverElem
	if e == nil {
		if e = in.selElem; e == nil {
			return
		}
		p.Stroke(Rect{e.x, e.y, e.w, e.h}, RGBA(59, 130, 246, 1), 0, 1)
		return
	}
	box := Rect{e.x, e.y, e.w, e.h}
	marginBox := Rect{box.X - e.m(3), box.Y - e.m(0), box.W + e.marginX(), box.H + e.marginY()}
	content := e.contentBox()
	ring(p, marginBox, box, RGBA(249, 115, 22, 0.35))
	ring(p, box, content, RGBA(34, 197, 94, 0.35))
	p.Fill(content, RGBA(59, 130, 246, 0.35), 0)
	// A tag above the box, else inside it, naming the element.
	label := elementName(e) + "  " + num(e.w) + " × " + num(e.h)
	l := textSystem().Layout(text.Params{Text: label, Style: text.Style{Family: in.theme.Font, Size: 11, Weight: 600}})
	tw, th := l.Width+12, l.Height+6
	x, y := max(min(box.X, in.appW-tw), 0), box.Y-th-2
	if y < 0 {
		y = min(box.Y+box.H+2, h-th)
	}
	p.Fill(Rect{x, y, tw, th}, RGB(30, 41, 59), 4)
	p.textLayout(l, x+6, y+3, RGB(255, 255, 255), textStyle{}, nil)
}

// ring fills the room between outer and inner, which it holds.
func ring(p *Painter, outer, inner Rect, c Color) {
	if inner.W <= 0 || inner.H <= 0 {
		p.Fill(outer, c, 0)
		return
	}
	p.Fill(Rect{outer.X, outer.Y, outer.W, inner.Y - outer.Y}, c, 0)
	p.Fill(Rect{outer.X, inner.Y + inner.H, outer.W, outer.Y + outer.H - inner.Y - inner.H}, c, 0)
	p.Fill(Rect{outer.X, inner.Y, inner.X - outer.X, inner.H}, c, 0)
	p.Fill(Rect{inner.X + inner.W, inner.Y, outer.X + outer.W - inner.X - inner.W, inner.H}, c, 0)
}

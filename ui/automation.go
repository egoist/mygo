package ui

import (
	"math"
	"runtime"
	"slices"
	"sync/atomic"

	accel "github.com/egoist/mygo/internal/accelerator"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
	"github.com/egoist/mygo/ui/automation"
)

var automationSessions atomic.Uint64

// Automatable returns a copy of the content enabling mygo.Automate's
// Inspect, Perform and Input APIs. It works in production and mygo_noinspector
// builds. The host owns authorization and transport; nothing is published.
func (v *Content) Automatable() *Content {
	copy := *v
	copy.automate = func(rt *engine, conn *surface.Conn) {
		a := &surface.Automation{}
		conn.Automation = a
		send := func(ev platform.SurfaceEvent) {
			if rt.closed {
				return
			}
			rt.event(ev)
			rt.runFrame()
		}
		a.Inspect = func() (automation.Snapshot, error) {
			if rt.inFrame || rt.automationActive {
				return automation.Snapshot{}, automation.ErrUnsupported
			}
			return rt.inspect(), nil
		}
		a.Perform = func(target automation.Target, request automation.Request) error {
			return rt.perform(target, request, send)
		}
		a.Input = func(input automation.Input) error { return rt.automationInput(input, send) }
	}
	return &copy
}

// Inspect returns detached semantic data for the Tester's current frame.
// Like other Tester methods, call it on the goroutine driving the tester.
func (t *Tester) Inspect() automation.Snapshot { return t.rt.inspect() }

// Perform validates target against the current frame and drives a normal
// control action, settling frames without advancing the Tester's clock.
func (t *Tester) Perform(target automation.Target, request automation.Request) error {
	return t.rt.perform(target, request, t.send)
}

// Input sends key, text or scroll input as the native automation driver does.
// Like Perform, it settles frames without advancing the Tester's clock.
func (t *Tester) Input(input automation.Input) error {
	return t.rt.automationInput(input, t.send)
}

func (rt *engine) inspect() automation.Snapshot {
	if rt.automationSession == 0 {
		rt.automationSession = automationSessions.Add(1)
	}
	tree := rt.accessTree()
	s := automation.Snapshot{Session: rt.automationSession, Revision: rt.paints, Focus: tree.Focus, Nodes: make([]automation.Node, len(tree.Nodes))}
	var elements map[uint64]*node
	if rt.c.root != nil {
		elements = rt.elementsByID(rt.c.root)
	}
	for i, n := range tree.Nodes {
		state := func(flag platform.AccessStates) bool { return n.States&flag != 0 }
		s.Nodes[i] = automation.Node{
			ID: n.ID, Parent: n.Parent, Role: automationRoles[n.Role], Name: n.Label, Value: n.Value,
			Description: n.Description, Placeholder: n.Placeholder,
			Bounds:   automation.Bounds{X: n.Bounds.X, Y: n.Bounds.Y, W: n.Bounds.W, H: n.Bounds.H},
			Disabled: state(platform.AccessDisabled), Focusable: state(platform.AccessFocusable),
			Checked: state(platform.AccessChecked), Mixed: state(platform.AccessMixed), Selected: state(platform.AccessSelected),
			Expanded: state(platform.AccessExpanded), Expandable: state(platform.AccessExpandable),
			ReadOnly: state(platform.AccessReadOnly), Password: state(platform.AccessPassword), Multiline: state(platform.AccessMultiline),
			Selectable: state(platform.AccessSelectable), Multiselectable: state(platform.AccessMultiselectable),
			Offscreen: state(platform.AccessOffscreen), Invalid: state(platform.AccessInvalid),
			Min: n.Min, Max: n.Max, Now: n.Now, Step: n.Step, SelStart: n.SelStart, SelEnd: n.SelEnd,
			Level: n.Level, PosInSet: n.PosInSet, SetSize: n.SetSize,
		}
		out := &s.Nodes[i]
		switch n.Role {
		case platform.RoleListItem, platform.RoleRow, platform.RoleTab, platform.RoleTreeItem:
			out.Selected = out.Selected || out.Checked
		}
		if st := rt.states[n.ID]; st != nil {
			out.Generation = st.born
		}
		if out.Password {
			out.Value = ""
			out.SelStart, out.SelEnd = 0, 0
		}
		for _, a := range automationActions {
			if !out.Disabled && n.Actions&a.flag != 0 {
				out.Actions = append(out.Actions, a.action)
			}
		}
		if n.Actions&platform.ActionExpand != 0 {
			out.Actions = append(out.Actions, automation.Collapse)
		}
		if e := elements[n.ID]; e != nil {
			if !out.Password && (leafRole(n.Role) || e.kind == kindText) {
				out.Text = e.innerText()
			}
			if !out.Disabled && e.flags&interactive != 0 {
				out.Actions = append(out.Actions, automation.Click)
			}
			if !out.Disabled && e.st.editor != nil && e.flags&flagEditable != 0 {
				out.Actions = append(out.Actions, automation.SelectText)
				if !out.ReadOnly {
					out.Actions = append(out.Actions, automation.InsertText)
				}
			}
		}
	}
	return s
}

var automationRoles = [...]string{
	"group", "text", "button", "link", "checkbox", "radio", "switch", "slider", "progress", "textField",
	"image", "list", "scroll", "dialog", "popup", "tooltip", "popupButton", "tabList", "tab", "splitter",
	"status", "table", "row", "cell", "columnHeader", "tree", "treeItem", "listItem", "menuButton", "toolbar",
	"radioGroup", "toggleButton", "comboBox", "disclosure", "meter", "stepper", "colorWell", "alertDialog",
	"menu", "menuBar", "menuItem", "menuItemCheckbox", "menuItemRadio", "heading",
}

var automationActions = [...]struct {
	action automation.Action
	flag   platform.AccessActions
	kind   platform.AccessActionKind
}{
	{automation.Press, platform.ActionPress, platform.AccessPress},
	{automation.Focus, platform.ActionFocus, platform.AccessFocus},
	{automation.Increment, platform.ActionIncrement, platform.AccessIncrement},
	{automation.Decrement, platform.ActionDecrement, platform.AccessDecrement},
	{automation.SetValue, platform.ActionSetValue, platform.AccessSetValue},
	{automation.ScrollIntoView, platform.ActionScrollIntoView, platform.AccessScrollIntoView},
	{automation.Expand, platform.ActionExpand, platform.AccessExpand},
}

func (rt *engine) perform(target automation.Target, request automation.Request, send func(platform.SurfaceEvent)) error {
	if rt.closed {
		return automation.ErrClosed
	}
	if rt.inFrame || rt.automationActive {
		return automation.ErrUnsupported
	}
	rt.automationActive = true
	defer func() { rt.automationActive = false }()
	s := rt.inspect()
	if target.Session != s.Session {
		return automation.ErrStale
	}
	i := slices.IndexFunc(s.Nodes, func(n automation.Node) bool { return n.ID == target.ID })
	if i < 0 {
		return automation.ErrMissing
	}
	n := s.Nodes[i]
	if target.Generation != n.Generation {
		return automation.ErrStale
	}
	if n.Disabled {
		return automation.ErrDisabled
	}
	if !slices.Contains(n.Actions, request.Action) {
		return automation.ErrUnsupported
	}
	if request.Action == automation.Click {
		x, y := float32(n.Bounds.X+n.Bounds.W/2), float32(n.Bounds.Y+n.Bounds.H/2)
		w, h, _ := rt.host.size()
		chain := rt.hitChain(x, y)
		web := len(chain) > 0 && rt.states[chain[0]] != nil && rt.states[chain[0]].webView != nil
		at := slices.IndexFunc(chain, func(id uint64) bool { return rt.states[id] != nil && rt.states[id].flags&interactive != 0 })
		if n.Bounds.W <= 0 || n.Bounds.H <= 0 || x < 0 || y < 0 || x >= w || y >= h || web || at < 0 || chain[at] != target.ID {
			return automation.ErrCovered
		}
		send(platform.SurfaceEvent{Kind: platform.PointerDown, X: float64(x), Y: float64(y), Clicks: 1})
		send(platform.SurfaceEvent{Kind: platform.PointerUp, X: float64(x), Y: float64(y), Clicks: 1})
		return nil
	}
	ev := platform.SurfaceEvent{Kind: platform.AccessAction, ID: target.ID, Text: request.Text, From: request.From, To: request.To}
	switch request.Action {
	case automation.Collapse:
		ev.Action = platform.AccessCollapse
	case automation.SelectText:
		ev.Action = platform.AccessSelectText
	case automation.InsertText:
		ev.Action = platform.AccessInsertText
	default:
		for _, a := range automationActions {
			if a.action == request.Action {
				ev.Action = a.kind
				break
			}
		}
	}
	send(ev)
	return nil
}

func (rt *engine) automationInput(input automation.Input, send func(platform.SurfaceEvent)) error {
	if rt.closed {
		return automation.ErrClosed
	}
	if rt.inFrame || rt.automationActive {
		return automation.ErrUnsupported
	}
	rt.automationActive = true
	defer func() { rt.automationActive = false }()
	switch input.Kind {
	case automation.KeyInput:
		a, err := accel.Parse(input.Key, runtime.GOOS)
		if err != nil {
			return err
		}
		var key Key
		for k := KeyEnter; k <= KeyZ; k++ {
			if keyName(k) == a.Key {
				key = k
				break
			}
		}
		if key == KeyUnknown {
			return automation.ErrUnsupported
		}
		var mods platform.Modifiers
		for _, m := range []struct {
			from accel.Modifiers
			to   platform.Modifiers
		}{{accel.Shift, platform.ModShift}, {accel.Ctrl, platform.ModCtrl}, {accel.Alt, platform.ModAlt}, {accel.Super, platform.ModSuper}} {
			if a.Has(m.from) {
				mods |= m.to
			}
		}
		send(platform.SurfaceEvent{Kind: platform.KeyPressed, Key: platform.Key(key), Mods: mods})
		send(platform.SurfaceEvent{Kind: platform.KeyReleased, Key: platform.Key(key)})
	case automation.TextInput:
		send(platform.SurfaceEvent{Kind: platform.TextInput, Text: input.Text})
	case automation.ScrollInput:
		for _, v := range []float64{input.X, input.Y, input.DX, input.DY} {
			if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > math.MaxFloat32 {
				return automation.ErrUnsupported
			}
		}
		w, h, _ := rt.host.size()
		if input.X < 0 || input.Y < 0 || input.X >= float64(w) || input.Y >= float64(h) {
			return automation.ErrCovered
		}
		chain := rt.hitChain(float32(input.X), float32(input.Y))
		if len(chain) > 0 && rt.states[chain[0]] != nil && rt.states[chain[0]].webView != nil {
			return automation.ErrUnsupported
		}
		send(platform.SurfaceEvent{Kind: platform.PointerScroll, X: input.X, Y: input.Y, DX: input.DX, DY: input.DY, Precise: true})
	default:
		return automation.ErrUnsupported
	}
	return nil
}

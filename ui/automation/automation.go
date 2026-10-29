// Package automation describes headless MyGo UI for test tools.
// It provides data and control actions, without a transport or access policy.
package automation

import "errors"

var (
	ErrMissing     = errors.New("automation: target not found")
	ErrAmbiguous   = errors.New("automation: target is ambiguous")
	ErrStale       = errors.New("automation: target belongs to another session or element lifetime")
	ErrDisabled    = errors.New("automation: target is disabled")
	ErrUnsupported = errors.New("automation: operation is unsupported")
	ErrCovered     = errors.New("automation: target center is outside the surface or covered")
)

// Snapshot is detached value data from the last rendered semantic tree.
// Nodes are in tree order; Parent is a node index, or -1 at the top.
// Invisible and inert content, including content behind modal dialogs, is
// omitted. RoleNone and layout-only containers are flattened; virtualized
// content not built by the view and embedded web pages are not described.
type Snapshot struct {
	Session uint64 `json:",string"`
	// Revision counts paints for observation; it does not expire targets.
	Revision uint64 `json:",string"`
	Focus    uint64 `json:",string"`
	Nodes    []Node
}

// Target identifies an element's lifetime in one session. It survives
// repainting and rebuilds while the element keeps its UI identity, but not
// removal and recreation with the same ID. Perform checks the current tree,
// state and geometry. Do not persist targets across sessions.
type Target struct {
	Session, Generation, ID uint64 `json:",string"`
}

// Bounds is the transformed axis-aligned box in device-independent pixels
// relative to the native content surface, before clipping and occlusion.
// Geometry does not prove that a control is visible or pointer-accessible.
type Bounds struct{ X, Y, W, H float64 }

// Node describes one semantic element. Name is its accessible name; Text
// is its leaf content, where available, even when an explicit Label replaces
// the name. Password values and selections are omitted. Offscreen describes
// built content outside a scroll viewport, not arbitrary window occlusion.
// Selected describes chosen list items, table rows, tabs and tree items;
// Checked also retains the accessibility tree's checked state for these roles.
type Node struct {
	ID                                                uint64 `json:",string"`
	Generation                                        uint64 `json:",string"`
	Parent                                            int
	Role, Name, Text, Value, Description, Placeholder string
	Bounds                                            Bounds

	Disabled, Focusable, Checked, Mixed, Selected, Expanded, Expandable   bool
	ReadOnly, Password, Multiline, Selectable, Multiselectable, Offscreen bool
	Invalid                                                               bool

	Min, Max, Now, Step      float64
	SelStart, SelEnd         int
	Level, PosInSet, SetSize int
	Actions                  []Action
}

// Action is a supported control operation. Press is semantic activation
// (including toggles), not a pointer click. Click sends a primary pointer
// down/up at the box center after hit testing. Focus also selects choosable
// rows; text editing uses the control's normal input path. Increment and
// Decrement use its keyboard behavior. Expand/Collapse apply to tree items.
type Action string

const (
	Click          Action = "click"
	Press          Action = "press"
	Focus          Action = "focus"
	Increment      Action = "increment"
	Decrement      Action = "decrement"
	SetValue       Action = "setValue"
	ScrollIntoView Action = "scrollIntoView"
	Expand         Action = "expand"
	Collapse       Action = "collapse"
	SelectText     Action = "selectText"
	InsertText     Action = "insertText"
)

// Request supplies an action's arguments. Text is used by SetValue and
// InsertText; From and To are rune offsets for SelectText (clamped by the
// control). Other actions ignore these fields.
type Request struct {
	Action   Action
	Text     string
	From, To int
}

// InputKind identifies input delivered through the Tester's normal event path.
type InputKind string

const (
	KeyInput    InputKind = "key"
	TextInput   InputKind = "text"
	ScrollInput InputKind = "scroll"
)

// Input supplies a key press/release, committed text, or precise scrolling.
// Key uses menu accelerator syntax ("Enter", "Escape", "CmdOrCtrl+A").
// Keys do not type text; use TextInput for that. Text and keys go to the
// current focus. Scroll coordinates and distances are in surface DIPs;
// positive DY moves down the content. Other kinds ignore unrelated fields.
type Input struct {
	Kind         InputKind
	Key, Text    string
	X, Y, DX, DY float64
}

// Resolve selects exactly one node by ID, or by an exact accessible name
// or available leaf text. Give either id != 0 or name != "", not both.
// Duplicate names return ErrAmbiguous; inspect Nodes and use an ID instead.
// Resolve operates on this snapshot; Perform validates its freshness.
func (s Snapshot) Resolve(id uint64, name string) (Target, error) {
	if (id == 0) == (name == "") {
		return Target{}, ErrUnsupported
	}
	var found Target
	for _, n := range s.Nodes {
		if (id != 0 && n.ID == id) || (name != "" && (n.Name == name || n.Text == name)) {
			if found.ID != 0 {
				return Target{}, ErrAmbiguous
			}
			found = Target{s.Session, n.Generation, n.ID}
		}
	}
	if found.ID == 0 {
		return Target{}, ErrMissing
	}
	return found, nil
}

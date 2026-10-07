package platform

// AccessTree is the content of a surface for assistive technology, such as
// screen readers: the elements a user can perceive and act on.
type AccessTree struct {
	// Nodes are the elements in tree order: a parent before its children.
	Nodes []AccessNode
	// Focus is the ID of the element with the keyboard focus, 0 for none.
	Focus uint64
	// Announcements are texts for assistive technology to read out once,
	// after what it is reading, as the title of a page shown or a toast:
	// news the focus does not bring.
	Announcements []string
}

// AccessNode is an element of an AccessTree.
type AccessNode struct {
	// ID identifies the element from frame to frame.
	ID uint64
	// Parent is the index of the parent node in AccessTree.Nodes, -1 for
	// the elements at the top.
	Parent int
	Role   AccessRole
	// Label names the element; Value is its value, as the text of a text
	// field, for elements that have one.
	Label, Value string
	// Bounds is the element's visible box, in DIPs relative to the
	// surface.
	Bounds RectF
	States AccessStates
	// Min, Max and Now are the range and the value of a slider or a
	// progress bar; Now is below Min for progress of unknown length. Step
	// is how far an increment moves the value, 0 for none.
	Min, Max, Now, Step float64
	// SelStart and SelEnd are the selection of a text field, in runes,
	// and Placeholder what it shows while empty.
	SelStart, SelEnd int
	Placeholder      string
	// Text exposes text ranges. Passwords never supply it. Its queries run
	// synchronously on the main thread and shape only the paragraphs needed.
	Text *AccessText
	// TextStart and TextEnd locate an inline child's text in its parent
	// text node, in runes; this keeps repeated links unambiguous.
	TextStart, TextEnd int
	// Description tells more about the element than its name, as help
	// text read after it: a field's description and its error, or a
	// tooltip.
	Description string
	// Level is how deep an item of a tree is, or the rank of a heading,
	// from 1; 0 for other nodes.
	Level int
	// PosInSet is the place of an item in its set, from 1, and SetSize
	// the size of the set, 0 when not given: a list's rows say which of
	// all they are, whether or not the others are built, and the list how
	// many rows it has.
	PosInSet, SetSize int
	// Actions are the actions the element takes in AccessAction events.
	Actions AccessActions
}

// AccessRole is the kind of an element of an AccessTree.
type AccessRole uint8

const (
	RoleGroup AccessRole = iota
	RoleText
	RoleButton
	RoleLink
	RoleCheckBox
	RoleRadio
	RoleSwitch
	RoleSlider
	RoleProgress
	RoleTextField
	RoleImage
	RoleList
	RoleScroll
	RoleDialog
	RolePopup
	RoleTooltip
	RolePopUpButton
	RoleTabList
	RoleTab
	RoleSplitter
	RoleStatus
	RoleTable
	RoleRow
	RoleCell
	RoleColumnHeader
	RoleTree
	RoleTreeItem
	RoleListItem
	RoleMenuButton
	RoleToolbar
	RoleRadioGroup
	// RoleToggleButton is pressed while AccessChecked.
	RoleToggleButton
	// RoleComboBox is a text field with a popup of options, which is
	// AccessExpanded while it shows.
	RoleComboBox
	// RoleDisclosure is a button showing or hiding content below it, which
	// is AccessExpanded while it shows: the trigger of a collapsible, or
	// the header of an accordion's section.
	RoleDisclosure
	// RoleMeter shows a value in a range, as a level indicator, and
	// RoleStepper steps one, as a spin button: both have Min, Max and Now.
	RoleMeter
	RoleStepper
	// RoleColorWell is a button showing a color, its Value, which opens a
	// picker of it, as AppKit's color well.
	RoleColorWell
	// RoleAlertDialog is a dialog asking about something important, its
	// Description the message.
	RoleAlertDialog
	// RoleMenu holds the items of a menu, RoleMenuBar the menus' titles
	// along a window, and RoleMenuItem is an item of either, which
	// RoleMenuItemCheckBox and RoleMenuItemRadio are when they show a
	// choice, AccessChecked (AccessMixed) while chosen.
	RoleMenu
	RoleMenuBar
	RoleMenuItem
	RoleMenuItemCheckBox
	RoleMenuItemRadio
	// RoleHeading is the title of a section, Level its rank: 1 for the
	// highest.
	RoleHeading
)

// Ranged reports whether nodes of a role have a value in a range (Min,
// Max and Now).
func (r AccessRole) Ranged() bool {
	return r == RoleSlider || r == RoleProgress || r == RoleMeter || r == RoleStepper
}

// AccessStates are the states of an element of an AccessTree.
// AccessChecked is the choice among a tab list's tabs, a tree's items, and
// the rows of a list or a table that are AccessSelectable.
type AccessStates uint32

const (
	AccessFocusable AccessStates = 1 << iota
	AccessDisabled
	AccessChecked
	AccessMixed
	AccessSelected
	AccessExpanded
	AccessMultiline
	AccessPassword
	AccessReadOnly
	AccessSelectable
	// AccessOffscreen is set on elements built but out of view, as the
	// rows a list builds beyond its edges.
	AccessOffscreen
	// AccessMultiselectable is set on a list or a table whose user
	// chooses several rows.
	AccessMultiselectable
	// AccessSegment is set on the segments of a segmented control or a
	// group of toggles, as AppKit's subrole AXSegment says.
	AccessSegment
	// AccessSearch is set on a text field for searching.
	AccessSearch
	// AccessInvalid is set on a control whose value is not valid, as a
	// field with an error.
	AccessInvalid
	// AccessSortAscending and AccessSortDescending are set on the header
	// of the column a table's rows are sorted by.
	AccessSortAscending
	AccessSortDescending
	// AccessExpandable is set on an item of a tree with children, which
	// AccessExpanded shows.
	AccessExpandable
	// AccessVertical is set on a slider going up.
	AccessVertical
)

// AccessActions are the actions an element of an AccessTree takes.
type AccessActions uint8

const (
	ActionPress AccessActions = 1 << iota
	ActionFocus
	ActionIncrement
	ActionDecrement
	ActionSetValue
	// ActionScrollIntoView scrolls the containers around the element to
	// show it, as assistive technology moving to it out of view asks.
	ActionScrollIntoView
	// ActionExpand opens and closes an item of a tree with children
	// (AccessExpand, AccessCollapse), which pressing chooses.
	ActionExpand
)

// AccessActionKind is the action of an AccessAction event.
type AccessActionKind uint8

const (
	AccessPress AccessActionKind = iota
	AccessFocus
	AccessIncrement
	AccessDecrement
	AccessSetValue
	AccessScrollIntoView
	AccessExpand
	AccessCollapse
	// AccessSetSelection selects From..To, with To the active caret, even
	// in read-only/selectable text. AccessScrollText reveals that range;
	// Caret is 1 to align its start at the top, -1 its end at the bottom.
	AccessSetSelection
	AccessScrollText
)

// AccessText is a frame's text state. Offsets are Go runes; native bridges
// convert UTF-16 or byte offsets through Query, without copying the document.
type AccessText struct {
	Content       string
	Length, Caret int
	Selectable    bool
	Query         func(AccessTextQuery) AccessTextResult
}

type AccessTextRange struct{ Start, End int }

type AccessTextUnit uint8

const (
	TextCharacter AccessTextUnit = iota // an extended grapheme cluster
	TextWord
	TextLine // a visual, wrapped line, including its line break
	TextParagraph
	TextDocument
)

type AccessTextQueryKind uint8

const (
	TextSlice AccessTextQueryKind = iota
	TextUnitRange
	TextMoveOffset
	TextRangeBounds // visible rectangles; an empty range is the caret
	TextVisibleRanges
	TextOffsetAtPoint
	TextLineNumber
	TextLineRange
	TextToUTF16
	TextFromUTF16
	TextToByte
	TextFromByte
)

type AccessTextQuery struct {
	Kind              AccessTextQueryKind
	Unit              AccessTextUnit
	Start, End, Count int
	X, Y              float64
}

type AccessTextResult struct {
	OK                bool // false after removal, or for an unsupported query
	Start, End, Count int
	Text              string
	Rects             []RectF
	Ranges            []AccessTextRange
}

func (t *AccessText) Ask(q AccessTextQuery) AccessTextResult {
	if t == nil || t.Query == nil {
		return AccessTextResult{}
	}
	return t.Query(q)
}

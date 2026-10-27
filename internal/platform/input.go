package platform

// PageInput is a Page that takes input as the system delivers the input of
// the user's devices and input methods: the page's events are trusted
// (isTrusted is true), not the ones scripts make. The macOS, Linux and
// Windows backends' web views have it; the browsers' pages do not, and
// Page.SendMouse, SendKey and InsertText then return ErrUnsupported. It is
// not part of Page so that the interface's method types, and the backends'
// implementations, are linked only into apps that call them (see "Binary
// size" in the architecture guide).
type PageInput interface {
	// SendMouse and SendKey deliver one input event to the page, and
	// InsertText inserts text at its caret. done runs on the main thread
	// once the page took the input, or with the reason it could not; a
	// backend whose page answers a mouse move only when it draws a frame
	// (WebView2 while the page is not visible) runs it as soon as the move
	// is sent.
	SendMouse(e PageMouseEvent, done func(err error))
	SendKey(e PageKeyEvent, done func(err error))
	InsertText(text string, done func(err error))
}

// PageMouseKind is what a PageMouseEvent does.
type PageMouseKind uint8

// The kinds of mouse event for a page.
const (
	PageMouseMove PageMouseKind = iota
	PageMouseDown
	PageMouseUp
)

// PageMouseEvent is the pointer and its primary button acting on a page.
type PageMouseEvent struct {
	Kind PageMouseKind
	// X and Y are CSS pixels from the top-left corner of the page's
	// viewport.
	X, Y float64
	// ClickCount is how many presses in a row this one is (1 for a single
	// click); it is ignored for PageMouseMove.
	ClickCount int
	Mods       Modifiers
}

// PageKeyKind is what a PageKeyEvent does.
type PageKeyKind uint8

// The kinds of key event for a page.
const (
	// PageKeyPress presses a key and releases it: a key down, the
	// character it types if it types one, and a key up.
	PageKeyPress PageKeyKind = iota
	PageKeyDown
	PageKeyUp
)

// PageKeyEvent is a key of the keyboard pressed or released in a page.
type PageKeyEvent struct {
	Kind PageKeyKind
	Key  PageKey
	Mods Modifiers
}

// PageKey is a key of a US keyboard in every vocabulary a backend needs
// to send it to a page.
type PageKey struct {
	// Name is the KeyboardEvent.key value of the key without modifiers
	// ("Enter", "a"), Code its KeyboardEvent.code ("Enter", "KeyA").
	Name, Code string
	// Chars is what AppKit's events say the key types ("\r", "a"), and
	// Text what the DevTools protocol says it types: the character of a
	// printable key and "\r" for Enter, nothing for the others.
	Chars, Text string
	// Mac is the virtual key code of macOS (kVK_*), Win the one of
	// Windows (VK_*).
	Mac uint16
	Win int
}

// pageKeys are the named keys SendKey knows, the keys of editing and Space.
var pageKeys = [...]PageKey{
	{Name: "Enter", Code: "Enter", Chars: "\r", Text: "\r", Mac: 36, Win: 0x0D},
	{Name: "Tab", Code: "Tab", Chars: "\t", Mac: 48, Win: 0x09},
	{Name: "Escape", Code: "Escape", Chars: "\x1b", Mac: 53, Win: 0x1B},
	{Name: "Backspace", Code: "Backspace", Chars: "\x7f", Mac: 51, Win: 0x08},
	{Name: "Delete", Code: "Delete", Chars: "\uf728", Mac: 117, Win: 0x2E},
	{Name: "ArrowLeft", Code: "ArrowLeft", Chars: "\uf702", Mac: 123, Win: 0x25},
	{Name: "ArrowRight", Code: "ArrowRight", Chars: "\uf703", Mac: 124, Win: 0x27},
	{Name: "ArrowDown", Code: "ArrowDown", Chars: "\uf701", Mac: 125, Win: 0x28},
	{Name: "ArrowUp", Code: "ArrowUp", Chars: "\uf700", Mac: 126, Win: 0x26},
	{Name: "Home", Code: "Home", Chars: "\uf729", Mac: 115, Win: 0x24},
	{Name: "End", Code: "End", Chars: "\uf72b", Mac: 119, Win: 0x23},
	{Name: "PageUp", Code: "PageUp", Chars: "\uf72c", Mac: 116, Win: 0x21},
	{Name: "PageDown", Code: "PageDown", Chars: "\uf72d", Mac: 121, Win: 0x22},
	{Name: " ", Code: "Space", Chars: " ", Text: " ", Mac: 49, Win: 0x20},
}

// macLetters are the kVK_ANSI_ virtual key codes of a to z, macDigits those
// of 0 to 9.
var (
	macLetters = [26]uint16{0, 11, 8, 2, 14, 3, 5, 4, 34, 38, 40, 37, 46, 45, 31, 35, 12, 15, 1, 17, 32, 9, 13, 7, 16, 6}
	macDigits  = [10]uint16{29, 18, 19, 20, 21, 23, 22, 26, 28, 25}
)

// PageKeyByName returns the key a KeyboardEvent.key value names: "Enter",
// "Tab", "Escape", "Backspace", "Delete", "ArrowLeft", "ArrowRight",
// "ArrowDown", "ArrowUp", "Home", "End", "PageUp", "PageDown", " ", or a
// letter or digit. A capital letter is the key of that letter, with shift
// true.
func PageKeyByName(name string) (k PageKey, shift, ok bool) {
	for _, k := range pageKeys {
		if k.Name == name {
			return k, false, true
		}
	}
	if len(name) != 1 {
		return PageKey{}, false, false
	}
	switch c := name[0]; {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		lower := c | 0x20
		s := string(rune(lower))
		return PageKey{Name: s, Code: "Key" + string(rune(lower&^0x20)), Chars: s, Text: s,
			Mac: macLetters[lower-'a'], Win: int(lower &^ 0x20)}, c != lower, true
	case c >= '0' && c <= '9':
		return PageKey{Name: name, Code: "Digit" + name, Chars: name, Text: name,
			Mac: macDigits[c-'0'], Win: int(c)}, false, true
	}
	return PageKey{}, false, false
}

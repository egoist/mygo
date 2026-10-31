package main

// field is a field of a message: its name, in Go's form, and its type:
// int, float, bool, string, bytes or strings.
type field struct{ name, typ string }

// msg is a message. A call goes from Go to the host, which runs it on the
// UI thread; an event from the host to Go, which handles it on its main
// thread. reply lists what the receiver answers: a message without one
// is not waited for (wait false), so that a call or an event costs its
// sender nothing; one with wait set is, even with no fields to answer.
type msg struct {
	name  string
	doc   string
	args  []field
	reply []field
	wait  bool
}

func f(name, typ string) field { return field{name, typ} }

// calls go from Go to the host.
var calls = []msg{
	{name: "AppInfo", doc: "the app's bundle and the system's settings, asked once at start", wait: true, reply: []field{
		f("Name", "string"), f("Version", "string"), f("Identifier", "string"),
		f("FilesDir", "string"), f("CacheDir", "string"), f("TempDir", "string"), f("DocumentsDir", "string"),
		f("ResourcesDir", "string"),
		f("Languages", "strings"), f("RegionLocale", "string"), f("FirstWeekday", "int"), f("HourCycle", "int"),
		f("System", "string"), f("SystemVersion", "string"), f("Model", "string"),
	}},
	{name: "Theme", doc: "the appearance and the accessibility preferences", wait: true, reply: []field{
		f("Dark", "bool"), f("Accent", "int"), f("ReduceMotion", "bool"), f("ReduceTransparency", "bool"),
		f("IncreaseContrast", "bool"), f("TextScale", "float"),
	}},
	{name: "SetAppearance", doc: `the appearance the app asks for: "light", "dark" or "system"`, args: []field{f("Source", "string")}},
	{name: "Screen", doc: "the screen's size in DIPs and scale", wait: true, reply: []field{
		f("Width", "float"), f("Height", "float"), f("Scale", "float"),
	}},
	{name: "Log", doc: "a line the app wrote to its standard output or error", args: []field{f("Error", "bool"), f("Line", "string")}},

	{name: "CreateWindow", doc: "the screen's content: a web view, or a surface for native UI", wait: true,
		args: []field{
			f("ID", "int"), f("Surface", "bool"), f("Background", "int"), f("HasBackground", "bool"),
			f("Schemes", "strings"), f("Scripts", "strings"), f("ScriptFlags", "strings"),
			f("UserAgent", "string"), f("UserAgentSuffix", "string"), f("Inspectable", "bool"),
			f("Autoplay", "bool"), f("SwipeNavigation", "bool"), f("PinchZoom", "bool"),
		},
		reply: []field{f("Error", "string"), f("Layer", "int")}},
	{name: "ShowWindow", args: []field{f("ID", "int")}},
	{name: "HideWindow", args: []field{f("ID", "int")}},
	{name: "CloseWindow", args: []field{f("ID", "int")}},
	{name: "SetBackground", args: []field{f("ID", "int"), f("Color", "int")}},
	{name: "Focus", args: []field{f("ID", "int")}},

	{name: "LoadURL", args: []field{f("ID", "int"), f("URL", "string")}},
	{name: "LoadHTML", args: []field{f("ID", "int"), f("HTML", "string"), f("BaseURL", "string")}},
	{name: "Reload", args: []field{f("ID", "int"), f("IgnoreCache", "bool")}},
	{name: "StopLoading", args: []field{f("ID", "int")}},
	{name: "GoBack", args: []field{f("ID", "int")}},
	{name: "GoForward", args: []field{f("ID", "int")}},
	{name: "Eval", args: []field{f("ID", "int"), f("JS", "string")}},
	{name: "CallAsync", doc: "runs Body as an async function's; AsyncResult answers", args: []field{f("ID", "int"), f("Req", "int"), f("Body", "string")}},
	{name: "SetZoom", args: []field{f("ID", "int"), f("Factor", "float")}},
	{name: "SetUserAgent", args: []field{f("ID", "int"), f("UserAgent", "string")}},
	{name: "SetUserScripts", args: []field{f("ID", "int"), f("Scripts", "strings"), f("ScriptFlags", "strings")}},
	{name: "CapturePage", doc: "a PNG of the page; Captured answers", args: []field{f("ID", "int"), f("Req", "int")}},
	{name: "UserAgent", wait: true, args: []field{f("ID", "int")}, reply: []field{f("UserAgent", "string")}},

	{name: "SchemeRespond", doc: "the status and headers of a SchemeRequest (Header: names and values in turn)",
		args: []field{f("Req", "int"), f("Status", "int"), f("Header", "strings")}},
	{name: "SchemeData", args: []field{f("Req", "int"), f("Data", "bytes")}},
	{name: "SchemeFinish", args: []field{f("Req", "int")}},
	{name: "SchemeFail", args: []field{f("Req", "int"), f("Message", "string")}},

	{name: "MessageBox", doc: "an alert; MessageBoxResult answers",
		args: []field{f("Req", "int"), f("Type", "string"), f("Title", "string"), f("Message", "string"),
			f("Detail", "string"), f("Buttons", "strings"), f("DefaultID", "int"), f("CancelID", "int")}},
	{name: "OpenFiles", doc: "the system's picker of documents; FilesResult answers with copies the app may read",
		args: []field{f("Req", "int"), f("Multiple", "bool"), f("Directories", "bool"), f("Extensions", "strings")}},
	{name: "ShareFile", doc: "the share sheet of a file, as the place to save it", args: []field{f("Path", "string")}},

	{name: "ClipboardText", wait: true, reply: []field{f("Text", "string"), f("OK", "bool")}},
	{name: "SetClipboardText", args: []field{f("Text", "string")}},
	{name: "OpenURL", doc: "opens a URL in the app the system has for it", wait: true, args: []field{f("URL", "string")}, reply: []field{f("OK", "bool")}},

	{name: "SecretGet", wait: true, args: []field{f("Service", "string"), f("Account", "string")},
		reply: []field{f("Value", "bytes"), f("Found", "bool"), f("Error", "string")}},
	{name: "SecretSet", wait: true, args: []field{f("Service", "string"), f("Account", "string"), f("Value", "bytes")},
		reply: []field{f("Error", "string")}},
	{name: "SecretDelete", wait: true, args: []field{f("Service", "string"), f("Account", "string")},
		reply: []field{f("Error", "string")}},

	{name: "Notify", doc: "a local notification; NotifyDone answers once the system has it",
		args: []field{f("Req", "int"), f("NotificationID", "string"), f("Title", "string"), f("Subtitle", "string"),
			f("Body", "string"), f("Group", "string"), f("Silent", "bool")}},
	{name: "RemoveNotification", args: []field{f("NotificationID", "string")}},
	{name: "RemoveAllNotifications"},
	{name: "KeepAwake", doc: "keeps the screen on while the app shows", args: []field{f("On", "bool")}},
	{name: "Haptic", doc: `"selection", "light", "medium", "heavy", "success", "warning" or "error"`, args: []field{f("Kind", "string")}},

	// Native UI.
	{name: "RequestFrame", doc: "asks for a Frame at the next refresh of the display", args: []field{f("ID", "int")}},
	{name: "SetTextInput", doc: "where the caret of the text being edited is, and whether the keyboard shows for it",
		args: []field{f("ID", "int"), f("Active", "bool"), f("X", "float"), f("Y", "float"), f("Width", "float"), f("Height", "float"),
			f("Kind", "string"), f("Secure", "bool"), f("Text", "string"), f("SelStart", "int"), f("SelEnd", "int")}},
	{name: "TextInputChanged", doc: "the text around the caret changed, which the keyboard's suggestions read again", args: []field{f("ID", "int")}},
	{name: "SetCursor", doc: "the pointer's shape, for a mouse or a trackpad", args: []field{f("ID", "int"), f("Cursor", "string")}},
	{name: "AccessibilityChanged", doc: "the tree of accessible elements changed; Focus names the one with the focus",
		args: []field{f("ID", "int"), f("Focus", "int")}},
	{name: "Announce", args: []field{f("Text", "string")}},
	{name: "ShowMenu", doc: "the system's edit menu at a point, with the actions given (cut, copy, paste, selectAll and labels); MenuChosen answers",
		args: []field{f("ID", "int"), f("Req", "int"), f("X", "float"), f("Y", "float"), f("Actions", "strings")}},
	{name: "Shape", doc: "shapes Text in Font (Android): glyph IDs, advances and offsets, as Minikin does for a TextView", wait: true,
		args:  []field{f("Text", "string"), f("Font", "string"), f("Size", "float"), f("Weight", "int"), f("Italic", "bool"), f("RTL", "bool")},
		reply: []field{f("Glyphs", "bytes"), f("Fonts", "strings"), f("Ascent", "float"), f("Descent", "float"), f("Leading", "float")}},
	{name: "DrawGlyphs", doc: "draws glyphs of a font file into a mask (Android): their coverage, a byte per pixel", wait: true,
		args:  []field{f("FontFile", "string"), f("Size", "float"), f("Weight", "int"), f("Italic", "bool"), f("Glyphs", "bytes"), f("Scale", "float")},
		reply: []field{f("Width", "int"), f("Height", "int"), f("Left", "int"), f("Top", "int"), f("Mask", "bytes")}},
}

// events go from the host to Go.
var events = []msg{
	{name: "Ready", doc: "the app finished launching"},
	{name: "Active", doc: "the app is in front, or no longer", args: []field{f("Active", "bool")}},
	{name: "Background", doc: "the app went to the background (true) or came back", args: []field{f("Background", "bool")}},
	{name: "ThemeChanged"},
	{name: "LocaleChanged"},
	{name: "OpenURLs", args: []field{f("URLs", "strings")}},
	{name: "MemoryWarning"},
	{name: "Terminating"},

	{name: "Resized", doc: "the window's size in DIPs, the scale, and the insets of what covers it: the system's bars, the cutout, the keyboard",
		args: []field{f("ID", "int"), f("Width", "float"), f("Height", "float"), f("Scale", "float"),
			f("SafeTop", "float"), f("SafeLeft", "float"), f("SafeBottom", "float"), f("SafeRight", "float"), f("Keyboard", "float")}},
	{name: "BackPressed", doc: "Android's back: whether the app took it", wait: true, args: []field{f("ID", "int")}, reply: []field{f("Handled", "bool")}},
	{name: "Message", doc: "a message the page's bridge posted", args: []field{f("ID", "int"), f("Text", "string")}},
	{name: "WillNavigate", wait: true,
		args:  []field{f("ID", "int"), f("URL", "string"), f("MainFrame", "bool"), f("UserInitiated", "bool"), f("Unasked", "bool"), f("Reload", "bool")},
		reply: []field{f("Allow", "bool")}},
	{name: "NewWindow", doc: "window.open or a link to _blank: whether the app opened it", wait: true,
		args: []field{f("ID", "int"), f("URL", "string"), f("UserInitiated", "bool")}, reply: []field{f("Handled", "bool")}},
	{name: "PageState", doc: "what the page is at, after any change of it",
		args: []field{f("ID", "int"), f("URL", "string"), f("Title", "string"), f("Loading", "bool"), f("CanGoBack", "bool"), f("CanGoForward", "bool")}},
	{name: "NavigationStarted", args: []field{f("ID", "int"), f("URL", "string")}},
	{name: "NavigationCommitted", args: []field{f("ID", "int"), f("URL", "string")}},
	{name: "LoadFinished", args: []field{f("ID", "int")}},
	{name: "LoadFailed", args: []field{f("ID", "int"), f("URL", "string"), f("Code", "int"), f("Description", "string")}},
	{name: "ClosedByPage", args: []field{f("ID", "int")}},
	{name: "ProcessGone", args: []field{f("ID", "int"), f("Reason", "string")}},
	{name: "PermissionRequested", wait: true, args: []field{f("ID", "int"), f("Kinds", "strings"), f("Origin", "string")}, reply: []field{f("Allow", "bool")}},
	{name: "SchemeRequest", doc: "a request of one of the app's schemes (Header: names and values in turn)",
		args: []field{f("ID", "int"), f("Req", "int"), f("Method", "string"), f("URL", "string"), f("Header", "strings"),
			f("Body", "bytes"), f("HasBody", "bool")}},
	{name: "SchemeCancel", args: []field{f("Req", "int")}},
	{name: "AsyncResult", args: []field{f("Req", "int"), f("Result", "string"), f("Error", "string")}},
	{name: "Captured", args: []field{f("Req", "int"), f("PNG", "bytes"), f("Error", "string")}},
	{name: "MessageBoxResult", args: []field{f("Req", "int"), f("Response", "int")}},
	{name: "FilesResult", args: []field{f("Req", "int"), f("Paths", "strings"), f("Error", "string")}},
	{name: "NotifyDone", args: []field{f("Req", "int"), f("Error", "string")}},
	{name: "NotificationClicked", args: []field{f("NotificationID", "string")}},

	// Native UI.
	{name: "Frame", doc: "the display refreshes: draw a frame if one was asked for", args: []field{f("ID", "int")}},
	{name: "SurfaceChanged", doc: "the native surface to draw on (a CAMetalLayer, an ANativeWindow), 0 when gone",
		args: []field{f("ID", "int"), f("Handle", "int"), f("Width", "float"), f("Height", "float"), f("Scale", "float"), f("Refresh", "float")}},
	{name: "Touch", doc: `a finger, a pen or a mouse: Phase is "down", "move", "up" or "cancel", Kind "touch", "pen" or "mouse"`,
		args: []field{f("ID", "int"), f("Pointer", "int"), f("Phase", "string"), f("Kind", "string"), f("X", "float"), f("Y", "float"),
			f("Pressure", "float"), f("Buttons", "int"), f("Time", "float")}},
	{name: "Scroll", doc: "a mouse's or a trackpad's scrolling, in DIPs", args: []field{f("ID", "int"), f("X", "float"), f("Y", "float"),
		f("DX", "float"), f("DY", "float"), f("Precise", "bool"), f("Phase", "string")}},
	{name: "Key", doc: "a hardware keyboard's key: Down, its name (as ui names keys) and the text it types",
		args:  []field{f("ID", "int"), f("Down", "bool"), f("Key", "string"), f("Text", "string"), f("Mods", "int"), f("Repeated", "bool")},
		reply: []field{f("Handled", "bool")}, wait: true},
	{name: "TextInsert", doc: "the keyboard typed Text, in place of the marked text if any", args: []field{f("ID", "int"), f("Text", "string")}},
	{name: "TextMarked", doc: "the input method's text being composed, its selection in runes", args: []field{f("ID", "int"), f("Text", "string"),
		f("SelStart", "int"), f("SelEnd", "int")}},
	{name: "TextDelete", doc: "the keyboard deleted Before runes before the caret and After after it", args: []field{f("ID", "int"), f("Before", "int"), f("After", "int")}},
	{name: "TextCommand", doc: `an editing action of the keyboard or the edit menu: "newline", "tab", "cut", "copy", "paste", "selectAll", "left", …`,
		args: []field{f("ID", "int"), f("Command", "string")}},
	{name: "TextState", doc: "the text around the caret and the selection, in runes, for the keyboard's suggestions", wait: true,
		args: []field{f("ID", "int")}, reply: []field{f("Before", "string"), f("Selected", "string"), f("After", "string"),
			f("Marked", "int"), f("CaretX", "float"), f("CaretY", "float"), f("CaretHeight", "float")}},
	{name: "TextSelect", doc: "the keyboard moved the caret or the selection, by runes from the caret", args: []field{f("ID", "int"), f("Start", "int"), f("End", "int")}},
	{name: "TextInputEnded", doc: "the keyboard went away", args: []field{f("ID", "int")}},
	{name: "MenuChosen", args: []field{f("ID", "int"), f("Req", "int"), f("Action", "string")}},
	{name: "AccessibilityTree", doc: "the accessible elements, encoded as Nodes: for each, its ID, parent, role, label, value, hint, frame and actions", wait: true,
		args: []field{f("ID", "int")}, reply: []field{f("Nodes", "bytes")}},
	{name: "AccessibilityAction", doc: `an assistive technology's action on an element: "press", "increment", "decrement", "focus", "scrollForward", …`, wait: true,
		args: []field{f("ID", "int"), f("Node", "int"), f("Action", "string")}, reply: []field{f("Handled", "bool")}},
}

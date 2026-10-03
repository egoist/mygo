# Terminal

The terminal plugin is a terminal for apps of [native UI](../ui.md): a view
that runs the user's shell, or any program, in a pseudo-terminal, with the
terminal emulator of [Ghostty](https://ghostty.org), libghostty-vt. It is
all Go, with no npm package: the emulator is a native library that the
app loads at run time, with no cgo, and that `mygo build` and `mygo dev`
put into the app.

## Set up

Make a terminal, which starts its program, and show it in a window:

```go
import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

func main() {
	mygo.App.WhenReady(func() {
		term, err := terminal.New(terminal.Options{})
		if err != nil {
			log.Fatal(err)
		}
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "Terminal",
			Content: ui.View(func(c *ui.Context) {
				terminal.View(c, term).Fill().AutoFocus()
			}),
		})
		win.OnClosed(func() { term.Close() })
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
```

`examples/terminal` is this app, whose window takes the shell's title and
closes when the shell exits:

```sh
go run ./examples/terminal
```

`terminal.View` is an element like any other: size it with `Fill` or
`Grow`, and put it next to other elements, as in a pane of an editor. Its
size sets the terminal's, in cells of its font, and a click gives it the
keyboard focus. A terminal shows in one view at a time. Its program starts
once a view first shows it, so that it draws its first screen at the
view's size, or half a second after `New` at 80×24 when no view does.

The terminal does what terminal apps do:

- colors (16, 256 and 24-bit), bold, italic, faint, inverse, strikethrough,
  overlines and underlines (single, double, curly, dotted, dashed, in their
  own colors);
- Unicode: wide characters, emoji and grapheme clusters take the cells of
  their width, as in Ghostty, and the system's fonts stand in for what the
  terminal's font lacks;
- box drawing, blocks and Powerline's separators drawn rather than taken
  from the font, so that the lines of full-screen programs join;
- a scrollback that reflows when the terminal resizes, and the alternate
  screen of full-screen programs;
- the keyboard as programs ask: legacy sequences, xterm's modifyOtherKeys
  or the Kitty keyboard protocol, and input methods, which compose at the
  cursor;
- mouse reports, focus reports, bracketed paste, synchronized output,
  hyperlinks (OSC 8), the title (OSC 0 and 2), the working directory
  (OSC 7) and copying to the clipboard (OSC 52).

## Using it

- **Selecting** with the pointer works as in Ghostty: a drag selects
  cells, a double click words and a triple click lines, and dragging past
  the top or the bottom scrolls. Option (Alt elsewhere) drags a rectangle.
  While a program takes the mouse, as editors do, Shift selects.
- **Copy and paste** are Command+C and Command+V on macOS, which the Edit
  menu's roles also send, and Control+Shift+C and Control+Shift+V
  elsewhere. Pastes are bracketed when the program asks, so that pasted
  lines do not run as commands. The context menu has Copy, Paste, Select
  All and Clear Scrollback.
- **Scrolling**: the wheel and the touchpad scroll the scrollback, as do
  Shift+Page Up and Shift+Page Down. Full-screen programs get the wheel as
  arrow keys, or as mouse reports when they take the mouse. Typing scrolls
  back to the bottom.
- **Links**: Command+click (Control+click elsewhere) opens a hyperlink a
  program printed (OSC 8).
- Shortcuts of the app come first: Command shortcuts on macOS are never
  sent to the program, and keys that the window or an element around the
  terminal handles with `Shortcut` go there.

## Options

```go
term, err := terminal.New(terminal.Options{
	Command:  []string{"htop"},          // the user's login shell when empty
	Dir:      "/tmp",                     // the home directory when empty
	Env:      []string{"EDITOR=vim"},     // added to the app's environment
	Font:     "JetBrains Mono, monospace", // "monospace" when empty
	FontSize: 14,                          // 13 when zero
	Theme:    terminal.DarkTheme(),        // follows the window when nil
	Cursor:   terminal.CursorBar,          // a block when zero
	OnTitle:  func(title string) { win.SetTitle(title) },
	OnExit:   func(code int) { win.Close() },
})
```

- `Command` runs a program instead of the shell. The shell runs as a login
  shell (`-zsh`), as terminal apps start it, so that it reads the user's
  profile: `$SHELL`, else `/bin/zsh` on macOS and `/bin/sh` elsewhere, and
  PowerShell on Windows.
- `Env` adds to the environment, which has `TERM=xterm-256color`,
  `COLORTERM=truecolor`, and `LANG=en_US.UTF-8` when no locale is set, as
  for apps started from the Finder.
- `Theme` sets the colors: the foreground, the background, the cursor's,
  the selection's and the palette of 16 colors. `terminal.DarkTheme()` and
  `terminal.LightTheme()` are the window's background and text with Visual
  Studio Code's palette, and without a theme the terminal takes the one of
  the window's appearance as it changes. Programs may change the colors
  (OSC 4, 10, 11, 12).
- `Cursor` and `NoBlink` set the cursor, which programs may change.
- `Scrollback` is about how many bytes of output to keep above the
  screen: 10 MB by default, none when negative.
- `OptionAsAlt` makes Option on macOS the Alt key of programs (Meta),
  instead of the key typing accented letters.
- `OnTitle`, `OnExit`, `OnBell` and `OnNotify` run on a goroutine of the
  terminal when the program sets the title, ends, rings the bell or asks
  for a desktop notification. `Done` is closed once the program exited,
  and `ExitCode` returns its code (-1 for a program that could not
  start); the terminal then shows "[Process exited]".

The terminal's methods are safe from any goroutine:

- `Send` sends bytes to the program as if typed, and `Paste` pastes text.
- `Feed` shows bytes as if the program wrote them, text and escape
  sequences.
- `Title`, `Dir` (the directory the shell reported), `Size` (in cells) and
  `Text` (the screen and its scrollback) read the terminal.
- `Close` hangs up the program, which gets SIGHUP, and frees the terminal.

## Without a program

`Conn` connects the terminal to anything that reads and writes, such as an
SSH session or a serial port, instead of a program: what the terminal reads
from it shows, and what is typed is written to it. A `Conn` with a method
`Resize(cols, rows int) error` is told the size of the terminal:

```go
term, err := terminal.New(terminal.Options{Conn: session})
```

A terminal whose `Conn` never sends anything shows what the app writes
with `Feed`, as a log with colors would.

## The library

libghostty-vt is a native library of Ghostty, built for each platform from
the version of Ghostty the plugin binds, and published with MyGo. The
plugin's package names those builds and their SHA-256 in
`mygo-natives.json`, and the CLI puts the one of each platform into the
apps it builds, among their resources, as
`libghostty-vt.dylib`, `libghostty-vt.so` or `ghostty-vt.dll`: a macOS app
signs it with the app, and a universal app gets both architectures in one
file.

A program not built by the CLI, as under `go run` and `go test`, downloads
the library once into the user's cache (`<cache>/mygo/natives/`), where
the CLI keeps those it downloads, and checks its SHA-256. Packaged apps
never download it. `terminal.LibraryPath` returns the library a program
loads, and `$MYGO_GHOSTTY_VT` names another, such as one you built:

```sh
git clone https://github.com/ghostty-org/ghostty && cd ghostty
zig build -Demit-lib-vt -Doptimize=ReleaseFast
MYGO_GHOSTTY_VT=$PWD/zig-out/lib/libghostty-vt.dylib go run ./examples/terminal
```

`terminal.Load` loads the library, which `New` does too: call it first to
report a missing library before showing a window.

The library runs on macOS 13 or later, as Ghostty does, so apps with a
terminal set `minimumSystemVersion` to 13.0 or later; on Linux with glibc
2.28 or later (Debian 10, Ubuntu 20.04), on x64 and arm64; and on Windows
10 1809 or later, whose ConPTY runs the program, on x64 and arm64.

## Testing

`ui.NewTester` runs a terminal view without a window: type into it with
`Type` and `Key`, and read what the program printed with `Text`:

```go
func TestShell(t *testing.T) {
	term, err := terminal.New(terminal.Options{Command: []string{"/bin/sh"}})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	tt := ui.NewTester(func(c *ui.Context) { terminal.View(c, term).Fill().AutoFocus() }, 640, 400)
	tt.Type("echo hello")
	tt.Key(0, ui.KeyEnter)
	for !strings.Contains(term.Text(), "\nhello") {
		time.Sleep(10 * time.Millisecond)
		tt.Frame()
	}
}
```

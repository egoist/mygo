package terminal

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/terminal/internal/vt"
	"github.com/egoist/mygo/ui"
)

// viewportText is the text of the rows the viewport shows, and where it is.
func viewportText(term *Terminal) (string, vt.Scrollbar, bool) {
	term.mu.Lock()
	defer term.mu.Unlock()
	_, rows := term.term.Size()
	var b strings.Builder
	for y := range rows {
		text, _ := term.term.RowText(y)
		b.WriteString(strings.TrimRight(string(text), " \x00"))
		b.WriteByte('\n')
	}
	return b.String(), term.term.Scrollbar(), term.term.AtBottom()
}

func TestPrependHistory(t *testing.T) {
	loadLib(t)
	newTerm := func() *Terminal {
		term, err := New(Options{Conn: newPipe()})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { term.Close() })
		term.Resize(20, 5)
		return term
	}
	// whole is the session as a server keeps it: older rows, one of them
	// wider than the screen, then what the client was sent.
	var older, recent strings.Builder
	for i := range 30 {
		older.WriteString("old " + itoa(i) + " \x1b[31mred\x1b[0m\r\n")
		if i == 20 {
			older.WriteString(strings.Repeat("w", 30) + "\r\n") // soft wrapped at 20
		}
	}
	for i := range 12 {
		recent.WriteString("new " + itoa(i) + "\r\n")
	}
	recent.WriteString("\x1b]2;the title\x07prompt % \x1b[?2004h")
	whole := newTerm()
	whole.Feed([]byte(older.String() + recent.String()))
	part := newTerm()
	part.Feed([]byte(recent.String()))

	for _, scrolled := range []bool{false, true} {
		term := newTerm()
		term.Feed(part.Snapshot())
		term.Feed([]byte("\x1b]2;the title\x07"))
		if scrolled {
			term.mu.Lock()
			term.term.ScrollBy(-3)
			term.mu.Unlock()
		}
		shown, before, _ := viewportText(term)
		n, err := term.PrependHistory(whole.historyAbove(t, part))
		if err != nil {
			t.Fatal(err)
		}
		if n != 32 { // 30 rows and the wrapped one's two
			t.Errorf("scrolled %v: added %d rows, want 32", scrolled, n)
		}
		got, after, bottom := viewportText(term)
		if got != shown {
			t.Errorf("scrolled %v: the viewport shows\n%s\nwant\n%s", scrolled, got, shown)
		}
		if bottom == scrolled {
			t.Errorf("scrolled %v: at the bottom %v", scrolled, bottom)
		}
		if after.Total != before.Total+32 {
			t.Errorf("scrolled %v: scrollbar %+v after %+v", scrolled, after, before)
		}
		// The text, the cursor and the modes are the whole session's:
		// what comes next lands alike.
		term.Feed([]byte("ls"))
		w := newTerm()
		w.Feed([]byte(older.String() + recent.String() + "ls"))
		if term.Text() != w.Text() {
			t.Errorf("scrolled %v: text\n%q, want\n%q", scrolled, term.Text(), w.Text())
		}
		term.mu.Lock()
		paste, title := term.term.Mode(vt.ModeBracketedPaste), term.term.Title()
		term.mu.Unlock()
		if !paste || title != "the title" {
			t.Errorf("scrolled %v: bracketed paste %v, title %q", scrolled, paste, title)
		}
	}

	full := newTerm()
	full.Feed([]byte("\x1b[?1049h"))
	if _, err := full.PrependHistory([]byte("x\r\n")); err != ErrFullScreen {
		t.Errorf("full screen: err %v", err)
	}
}

// historyAbove returns the rows of t's history above those part holds, as
// a server sends them: History's rows, less part's.
func (t *Terminal) historyAbove(tb *testing.T, part *Terminal) []byte {
	tb.Helper()
	all, mine := t.History(), part.History()
	if !strings.HasSuffix(string(all), string(mine)) {
		tb.Fatalf("history %q does not end with %q", all, mine)
	}
	return all[:len(all)-len(mine)]
}

// TestOnNearTop: scrolling up to within a screen of the top asks for older
// rows, once a row, and the rows PrependHistory adds keep the view where it
// is, out of reach of the top.
func TestOnNearTop(t *testing.T) {
	loadLib(t)
	var asked, locked int
	var term *Terminal
	term, err := New(Options{Conn: newPipe(), OnNearTop: func() {
		asked++
		// It runs as a frame builds, with neither lock held.
		if !term.rmu.TryLock() {
			locked++
			return
		}
		term.rmu.Unlock()
		if !term.mu.TryLock() {
			locked++
			return
		}
		term.mu.Unlock()
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	tt := ui.NewTester(func(c *ui.Context) { View(c, term).Fill().AutoFocus() }, 400, 200)
	var b strings.Builder
	for i := range 40 {
		b.WriteString("line " + itoa(i) + "\r\n")
	}
	term.Feed([]byte(b.String()))
	tt.Frame()
	if asked != 0 {
		t.Fatalf("asked %d times at the bottom", asked)
	}
	_, rows := term.Size()
	term.mu.Lock()
	term.term.ScrollToRow(rows) // a screen from the top: not yet
	term.mu.Unlock()
	term.redraw()
	tt.Frame()
	if asked != 0 {
		t.Fatalf("asked %d times a screen from the top", asked)
	}
	tt.Key(ui.Shift, ui.KeyPageUp)
	tt.Frame()
	if asked != 1 {
		t.Fatalf("asked %d times near the top, want 1", asked)
	}
	tt.Frame()
	if asked != 1 {
		t.Fatalf("asked %d times after a frame that did not move, want 1", asked)
	}
	shown, _, _ := viewportText(term)
	var older strings.Builder
	for i := range 50 {
		older.WriteString("older " + itoa(i) + "\r\n")
	}
	if _, err := term.PrependHistory([]byte(older.String())); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got, _, _ := viewportText(term); got != shown {
		t.Errorf("after the rows came, the view shows\n%s\nwant\n%s", got, shown)
	}
	if asked != 1 {
		t.Errorf("asked %d times once the rows came, want 1", asked)
	}
	if locked != 0 {
		t.Errorf("asked %d times with a lock of the terminal held", locked)
	}
}

// TestPrependHistoryKeepsState: what a snapshot leaves out stays across
// PrependHistory: the working directory (OSC 7), the colors programs set
// (OSC 4, 10, 11), and the marks of the shell's prompt (OSC 133), on the
// rows they marked and on the input the shell goes on with.
func TestPrependHistoryKeepsState(t *testing.T) {
	loadLib(t)
	term, err := New(Options{Conn: newPipe()})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	term.Resize(20, 6)
	var b strings.Builder
	for i := range 8 {
		b.WriteString("out " + itoa(i) + "\r\n")
	}
	b.WriteString("\x1b]7;file://host/tmp/work\x1b\\")
	b.WriteString("\x1b]4;1;rgb:12/34/56\x1b\\\x1b]10;rgb:aa/bb/cc\x1b\\\x1b]11;rgb:01/02/03\x1b\\")
	b.WriteString("\x1b]133;A\a$ ls\r\n\x1b]133;C\afile\r\n")
	b.WriteString("\x1b]133;A\a> two\r\n\x1b]133;A;k=c\a> line\x1b]133;B\a")
	term.Feed([]byte(b.String()))

	dir0, colors0, marks0 := readUnlocked(term)
	if _, err := term.PrependHistory([]byte("older 1\r\nolder 2\r\n")); err != nil {
		t.Fatal(err)
	}
	dir1, colors1, marks1 := readUnlocked(term)
	if dir1 != dir0 || dir1 != "/tmp/work" {
		t.Errorf("working directory %q, was %q", dir1, dir0)
	}
	if colors1 != colors0 {
		t.Errorf("colors\n%+v\nwere\n%+v", colors1, colors0)
	}
	if colors1.Palette[1] != (vt.RGB{R: 0x12, G: 0x34, B: 0x56}) || colors1.Background != (vt.RGB{R: 1, G: 2, B: 3}) {
		t.Errorf("colors programs set are gone: %+v", colors1)
	}
	if !slices.Equal(marks1, marks0) {
		t.Errorf("prompt marks %v, were %v", marks1, marks0)
	}
	// The shell's input goes on in the prompt: a row it wraps onto is
	// the prompt's.
	term.Feed([]byte(strings.Repeat("x", 20)))
	_, _, marks2 := readUnlocked(term)
	if got := marks2[len(marks2)-1]; got != vt.PromptContinuation {
		t.Errorf("the row the input wrapped onto is marked %v, want a continuation (%v)", got, marks2)
	}
}

// readUnlocked returns the working directory, the colors programs see and
// the prompt marks of the screen's rows.
func readUnlocked(term *Terminal) (string, vt.TermColors, []vt.RowPrompt) {
	dir := term.Dir()
	term.mu.Lock()
	defer term.mu.Unlock()
	var marks []vt.RowPrompt
	_, h := term.term.Size()
	for y := range h {
		p, _ := term.term.Row(y)
		marks = append(marks, p)
	}
	return dir, term.term.Colors(false), marks
}

// TestPrependHistoryOffLock: the new terminal is built without t.mu, so
// output, a resize and frames go on meanwhile; what came then lands in it
// as in the old one, and its effects (a reply, a bell) happen once.
func TestPrependHistoryOffLock(t *testing.T) {
	loadLib(t)
	var bells int
	conn := newPipe()
	term, err := New(Options{Conn: conn, OnBell: func() { bells++ }})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	term.Resize(20, 5)
	whole, err := New(Options{Conn: newPipe()})
	if err != nil {
		t.Fatal(err)
	}
	defer whole.Close()
	whole.Resize(20, 5)
	older := "older 1\r\nolder 2\r\n"
	before := "a\r\nb\r\nc\r\nd\r\ne\r\nf\r\n"
	during := "g\a\x1b[6n\r\nh\r\n"
	whole.Feed([]byte(older + before))
	term.Feed([]byte(before))
	conn.take(term)

	locked := 0
	prependBuilt = func(tt *Terminal) {
		if !tt.mu.TryLock() {
			locked++
			return
		}
		tt.mu.Unlock()
		tt.Feed([]byte(during[:4]))
		tt.Resize(24, 5)
		tt.Feed([]byte(during[4:]))
	}
	defer func() { prependBuilt = nil }()
	if _, err := term.PrependHistory([]byte(older)); err != nil {
		t.Fatal(err)
	}
	prependBuilt = nil
	if locked != 0 {
		t.Fatal("t.mu is held while the new terminal is built")
	}
	whole.Feed([]byte(during[:4]))
	whole.Resize(24, 5)
	whole.Feed([]byte(during[4:]))
	if term.Text() != whole.Text() {
		t.Errorf("text\n%q, want\n%q", term.Text(), whole.Text())
	}
	if c, r := term.Size(); c != 24 || r != 5 {
		t.Errorf("size %dx%d, want 24x5", c, r)
	}
	if got := strings.Count(conn.take(term), "R"); got != 1 {
		t.Errorf("the cursor position was reported %d times, want 1", got)
	}
	if bells != 1 {
		t.Errorf("rang %d times, want 1", bells)
	}
}

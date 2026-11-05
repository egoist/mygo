package terminal

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/egoist/mygo/plugins/terminal/internal/vt"
)

// ErrFullScreen is PrependHistory's error while a full-screen program's
// screen (the alternate screen) shows: the scrollback it would add to is
// the main screen's, which then does not show.
var ErrFullScreen = errors.New("terminal: a full-screen program's screen shows")

// PrependHistory adds rows above the oldest row of the scrollback, as a
// client of a server that keeps a session's terminal does when the view
// nears the top of the history it was sent and it fetches what came
// before. rows are VT sequences, row after row, each ended by "\r\n", or
// soft wrapped by running on to the last column, as History returns them.
// It returns how many rows the scrollback grew by, which is fewer than
// rows holds where the scrollback's limit lets go of the oldest.
//
// What the view shows stays where it is: scrolled into the history, it
// shows the same rows after, which are now further from the top; at the
// bottom, it stays there. The screen, the cursor, the modes programs set,
// the title, the working directory (OSC 7), the colors programs set (OSC
// 4, 10, 11, 12) and the marks of the shell's prompt on the screen (OSC
// 133) stay as they are, and output that comes later lands as it would
// have. The terminal is rebuilt to do this: a new one takes rows, then
// what this one shows (Snapshot), so it costs as much as writing the whole
// scrollback again. It is built while frames and output go on, which only
// wait for the snapshot and for the swap. The images programs placed,
// which a snapshot does not carry, go, and a selection ends; a search
// starts over, with the rows added.
func (t *Terminal) PrependHistory(rows []byte) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	t.pmu.Lock()
	defer t.pmu.Unlock()

	t.mu.Lock()
	if t.term == nil || t.closed {
		t.mu.Unlock()
		return 0, nil
	}
	if t.term.AltScreen() {
		t.mu.Unlock()
		return 0, ErrFullScreen
	}
	s := t.snapshotFor()
	t.prep = &prepending{}
	t.mu.Unlock()

	quiet := &quietVT{on: true, size: s.size, dark: s.dark}
	term, err := t.newVT(s.size.cols, s.size.rows, quiet)
	if err == nil {
		s.build(term, rows)
	}
	if prependBuilt != nil {
		prependBuilt(t)
	}

	t.mu.Lock()
	prep := t.prep
	t.prep = nil
	if err != nil || t.term == nil || t.closed {
		t.mu.Unlock()
		if term != nil {
			term.Free()
		}
		return 0, err
	}
	// What changed the terminal while this one was built changes it too.
	for _, op := range prep.ops {
		if op.resize != nil {
			r := op.resize
			term.Resize(r.cols, r.rows, r.cellW, r.cellH)
		} else {
			term.Write(op.p)
		}
	}
	quiet.on = false
	if t.curTheme != nil {
		t.curTheme.apply(term)
	}
	old := t.term
	added := int(term.Scrollbar().Total) - int(old.Scrollbar().Total)
	if old.AtBottom() {
		term.ScrollToBottom()
	} else {
		term.ScrollToRow(int(old.Scrollbar().Offset) + added)
	}
	t.swap(term)
	t.mu.Unlock()
	t.redraw()
	return max(added, 0), nil
}

// prependBuilt, for tests, runs once PrependHistory built the new
// emulator, before it takes t.mu to swap it in.
var prependBuilt func(t *Terminal)

// prepending is what changed the emulator while PrependHistory builds the
// one that replaces it, in order: output, or a resize.
type prepending struct {
	ops []prepOp
}

type prepOp struct {
	p      []byte
	resize *gridSize
}

// logPrepend records output p, or a resize to r, for the emulator
// PrependHistory builds; t.mu is held.
func (t *Terminal) logPrepend(p []byte, r *gridSize) {
	if t.prep == nil {
		return
	}
	if r != nil {
		t.prep.ops = append(t.prep.ops, prepOp{resize: r})
		return
	}
	if n := len(t.prep.ops); n > 0 && t.prep.ops[n-1].resize == nil {
		t.prep.ops[n-1].p = append(t.prep.ops[n-1].p, p...)
		return
	}
	t.prep.ops = append(t.prep.ops, prepOp{p: append([]byte(nil), p...)})
}

// vtWrite writes output to the emulator; t.mu is held.
func (t *Terminal) vtWrite(p []byte) {
	t.term.Write(p)
	t.logPrepend(p, nil)
}

// vtResize resizes the emulator; t.mu is held.
func (t *Terminal) vtResize(cols, rows, cellW, cellH int) {
	t.term.Resize(cols, rows, cellW, cellH)
	t.logPrepend(nil, &gridSize{cols, rows, cellW, cellH})
}

// prependSnapshot is what PrependHistory reads of the terminal under t.mu
// to build the one that replaces it without: the snapshot, and what a
// snapshot leaves out.
type prependSnapshot struct {
	size   gridSize
	dark   bool
	theme  *Theme
	vt     []byte
	title  string
	colors string
	// prompts are the marks of the shell's prompt of the screen's rows,
	// and cursor where the cursor is, without origin mode, which has the
	// cursor's position count from the scrolling region.
	prompts []vt.RowPrompt
	cursor  [2]int
	origin  bool
}

// snapshotFor reads what PrependHistory builds the new terminal from; t.mu
// is held.
func (t *Terminal) snapshotFor() prependSnapshot {
	term := t.term
	s := prependSnapshot{
		size:   t.size,
		dark:   t.curTheme == nil || t.curTheme.dark(),
		theme:  t.curTheme,
		vt:     term.VT(),
		title:  term.Title(),
		colors: programColors(term),
		origin: term.Mode(vt.ModeOrigin),
	}
	s.cursor[0], s.cursor[1] = term.Cursor()
	_, h := term.Size()
	for y := range h {
		p, _ := term.ScreenRowPrompt(y)
		s.prompts = append(s.prompts, p)
	}
	return s
}

// build writes rows, then the snapshot and what it leaves out, to term,
// which nothing else uses yet.
func (s *prependSnapshot) build(term *vt.Terminal, rows []byte) {
	if s.theme != nil {
		s.theme.apply(term)
	}
	term.Write(rows)
	if !bytes.HasSuffix(rows, []byte("\n")) {
		// The last row ran on to its last column: what was the oldest
		// row starts below it.
		term.Write([]byte("\r\n"))
	}
	// The snapshot draws from the top of the screen: the rows still on
	// it, above the cursor, scroll into the scrollback first.
	if _, y := term.Cursor(); y > 0 {
		_, h := term.Size()
		term.Write(fmt.Appendf(nil, "\x1b[%dH%s", h, strings.Repeat("\n", y)))
	}
	term.Write(s.vt)
	var b bytes.Buffer
	if s.title != "" {
		b.WriteString("\x1b]2;" + s.title + "\x1b\\")
	}
	b.WriteString(s.colors)
	if !s.origin {
		s.marks(&b)
	}
	term.Write(b.Bytes())
}

// marks writes the marks of the shell's prompt of the screen's rows, which
// a snapshot leaves out, and puts the cursor back: in the prompt, where its
// row is one, as the shell's input goes on there.
func (s *prependSnapshot) marks(b *bytes.Buffer) {
	marked := false
	for y, p := range s.prompts {
		switch p {
		case vt.PromptStart:
			fmt.Fprintf(b, "\x1b[%dH%s", y+1, markPrompt)
		case vt.PromptContinuation:
			fmt.Fprintf(b, "\x1b[%dH%s", y+1, markContinuation)
		default:
			continue
		}
		marked = true
	}
	if !marked {
		return
	}
	if y := s.cursor[1]; y >= len(s.prompts) || s.prompts[y] == vt.NoPrompt {
		b.WriteString(markEnd)
	}
	fmt.Fprintf(b, "\x1b[%d;%dH", s.cursor[1]+1, s.cursor[0]+1)
}

// swap makes term the terminal's emulator in place of the one it has,
// which it frees; t.mu is held.
func (t *Terminal) swap(term *vt.Terminal) {
	old := t.term
	t.term = term
	t.rmu.Lock()
	if t.held {
		// The render state holds what the old one showed until the
		// program's synchronized update ends: what this one shows now.
		t.rs.Update(term)
	}
	t.rmu.Unlock()
	if f := &t.find; f.s != nil {
		f.s.Free()
		f.s = nil
		if s, err := vt.NewSearch(term); err == nil {
			f.s = s
			s.SetText(f.status.Text)
			f.status = SearchStatus{Text: f.status.Text, Searching: true}
			f.pick = false
		}
	}
	for _, m := range t.mirrors {
		m.started = false
	}
	t.rebuilt = true
	old.Free()
}

// nearTop reports whether OnNearTop is due: the view shows the scrollback
// within a screen of its top, at a row it was not told of yet. t.mu is
// held.
func (v *view) nearTop(t *Terminal) bool {
	term := t.term
	if t.opts.OnNearTop == nil || term.AtBottom() || term.AltScreen() {
		v.toldTop = false
		return false
	}
	_, rows := term.Size()
	sb := term.Scrollbar()
	if sb.Offset >= uint64(rows) {
		v.toldTop = false
		return false
	}
	if v.toldTop && v.toldAt == sb.Offset {
		return false
	}
	v.toldTop, v.toldAt = true, sb.Offset
	return true
}

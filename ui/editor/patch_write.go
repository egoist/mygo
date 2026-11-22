package editor

import (
	"fmt"
	"github.com/egoist/mygo/internal/diff"
	"strconv"
	"strings"
)

// noNewline is the line a patch puts after the last line of a text that
// ends without a newline.
const noNewline = `\ No newline at end of file`

// patchContext is how many lines of a text a patch shows around each
// change, as git diff's three do.
const patchContext = 3

// Format returns the patch as the text of a unified diff, as ParsePatch
// reads it: git's diff --git line, the file's modes and its rename when it
// has them, and its hunks as @@ headers and lines, each - and + line but
// for the context around it. A binary patch is the line git writes for one,
// its contents not being a patch.
func (p FilePatch) Format() string {
	if p.empty() {
		return ""
	}
	var b strings.Builder
	a, bn := p.OldName, p.NewName
	if a == "" {
		a = p.NewName
	}
	if bn == "" {
		bn = p.OldName
	}
	if p.OldName != "" || p.NewMode != "" {
		// Git's header names both sides, a file it does not have as the
		// one it makes: of a file added whose mode the patch does not
		// know, the traditional header below is what git apply reads as
		// one, as diff -u writes it.
		b.WriteString("diff --git " + patchNameText("a/", a) + " " + patchNameText("b/", bn) + "\n")
	}
	switch {
	case p.OldName == "" && p.NewMode != "":
		b.WriteString("new file mode " + p.NewMode + "\n")
	case p.NewName == "" && p.OldMode != "":
		b.WriteString("deleted file mode " + p.OldMode + "\n")
	case p.OldMode != "" && p.NewMode != "" && p.OldMode != p.NewMode:
		b.WriteString("old mode " + p.OldMode + "\nnew mode " + p.NewMode + "\n")
	}
	if p.OldName != "" && p.NewName != "" && p.OldName != p.NewName {
		b.WriteString("rename from " + patchNameText("", p.OldName) + "\n")
		b.WriteString("rename to " + patchNameText("", p.NewName) + "\n")
	}
	if p.Binary {
		b.WriteString("Binary files " + patchSide(p.OldName, "a/") + " and " + patchSide(p.NewName, "b/") + " differ\n")
		return b.String()
	}
	if len(p.Hunks) > 0 {
		b.WriteString("--- " + patchSide(p.OldName, "a/") + "\n+++ " + patchSide(p.NewName, "b/") + "\n")
		for _, h := range p.Hunks {
			patchHunkText(&b, h)
		}
	}
	return b.String()
}

// FormatPatch returns the files' patches, in order, as one text that
// ParsePatch reads back as them.
func FormatPatch(files []FilePatch) string {
	var b strings.Builder
	for _, p := range files {
		b.WriteString(p.Format())
	}
	return b.String()
}

// empty tells whether the patch changes nothing, which a patch prints
// nothing for, as git diff does of two equal files.
func (p FilePatch) empty() bool {
	return len(p.Hunks) == 0 && !p.Binary && p.OldName == p.NewName && p.OldMode == p.NewMode
}

// DiffPatch returns the patch that turns old into new, as git diff writes
// one file of it: three lines of context around each change, the changes
// closer than that as one hunk, and a "\ No newline at end of file" line
// where a text ends without one. The names are the files' without git's a/
// and b/, the old's empty for a file added and the new's for one deleted.
// Texts that are equal are a patch of no hunks.
func DiffPatch(oldName, newName, old, new string) FilePatch {
	p := FilePatch{OldName: oldName, NewName: newName}
	oldLines, oldEOL := patchLines(old)
	newLines, newEOL := patchLines(new)
	hs := diff.Lines(oldLines, newLines)
	if len(hs) == 0 && oldEOL != newEOL && len(oldLines) > 0 {
		// Texts whose lines are equal but whose last one ends with a
		// newline in only one of them: a patch says that by writing the
		// line again, with the "\ No newline at end of file" that tells
		// it, as git does.
		last, lastNew := len(oldLines)-1, len(newLines)-1
		hs = []diff.Hunk{{A0: last, A1: last + 1, B0: lastNew, B1: lastNew + 1}}
	}
	for _, g := range patchGroups(hs, len(oldLines)) {
		p.Hunks = append(p.Hunks, patchHunk(g, oldLines, oldEOL, newLines, newEOL))
	}
	return p
}

// patchLines splits a text into the lines a patch counts, and tells whether
// it ends with a newline. An empty text has no lines.
func patchLines(text string) ([]string, bool) {
	if text == "" {
		return nil, true
	}
	lines := strings.Split(text, "\n")
	if lines[len(lines)-1] == "" {
		return lines[:len(lines)-1], true
	}
	return lines, false
}

// patchGroup is the changes of a patch that one hunk shows: those within
// the context of each other, and the context itself.
type patchGroup struct {
	start, end int // the lines of the old text it shows, from 0
	hunks      []diff.Hunk
}

// patchGroups groups the hunks of two texts into the hunks of a patch: each
// with three lines of context around it, or all of them together when that
// context reaches from one to the next.
func patchGroups(hunks []diff.Hunk, oldLines int) []patchGroup {
	var out []patchGroup
	for _, h := range hunks {
		start, end := max(h.A0-patchContext, 0), min(h.A1+patchContext, oldLines)
		if k := len(out) - 1; k >= 0 && start <= out[k].end {
			out[k].hunks = append(out[k].hunks, h)
			out[k].end = end
			continue
		}
		out = append(out, patchGroup{start: start, end: end, hunks: []diff.Hunk{h}})
	}
	return out
}

// patchHunk makes the hunk of a patch that shows a group of the changes of
// two texts, its lines, and the counts and starts of its header.
func patchHunk(g patchGroup, oldLines []string, oldEOL bool, newLines []string, newEOL bool) PatchHunk {
	var h PatchHunk
	first := g.hunks[0]
	// The line of the new text the group starts at: the change's own line,
	// less the context before it, the new text's lines and the old's
	// counted the same up to there.
	at, newAt := g.start, first.B0-(first.A0-g.start)
	start, startNew := at, newAt
	last, lastNew := len(oldLines)-1, len(newLines)-1
	for _, c := range g.hunks {
		for ; at < c.A0; at++ {
			h.Lines = append(h.Lines, " "+oldLines[at])
			if at == last && !oldEOL {
				h.Lines = append(h.Lines, noNewline)
			}
			if newAt == lastNew && !newEOL {
				h.Lines = append(h.Lines, noNewline)
			}
			newAt++
		}
		for ; at < c.A1; at++ {
			h.Lines = append(h.Lines, "-"+oldLines[at])
			if at == last && !oldEOL {
				h.Lines = append(h.Lines, noNewline)
			}
		}
		for ; newAt < c.B1; newAt++ {
			h.Lines = append(h.Lines, "+"+newLines[newAt])
			if newAt == lastNew && !newEOL {
				h.Lines = append(h.Lines, noNewline)
			}
		}
	}
	for ; at < g.end; at++ {
		h.Lines = append(h.Lines, " "+oldLines[at])
		if at == last && !oldEOL {
			h.Lines = append(h.Lines, noNewline)
		}
		if newAt == lastNew && !newEOL {
			h.Lines = append(h.Lines, noNewline)
		}
		newAt++
	}
	for _, l := range h.Lines {
		switch {
		case l == "\n" || l == "":
		case l[0] == '-':
			h.OldLines++
		case l[0] == '+':
			h.NewLines++
		case l[0] != '\\':
			h.OldLines++
			h.NewLines++
		}
	}
	// A side of no lines starts at the line before the one it is at, as
	// git writes -0,0 for a file added.
	if h.OldLines > 0 {
		h.OldStart = start + 1
	} else {
		h.OldStart = start
	}
	if h.NewLines > 0 {
		h.NewStart = startNew + 1
	} else {
		h.NewStart = startNew
	}
	return h
}

// patchHunkText writes a hunk as a patch shows it: its @@ header, its lines
// and their own.
func patchHunkText(b *strings.Builder, h PatchHunk) {
	b.WriteString("@@ -" + patchHunkRange(h.OldStart, h.OldLines) + " +" + patchHunkRange(h.NewStart, h.NewLines) + " @@")
	if h.Section != "" {
		b.WriteString(" " + h.Section)
	}
	b.WriteString("\n")
	for _, l := range h.Lines {
		if l == "" {
			b.WriteString(" ")
		} else {
			b.WriteString(l)
		}
		b.WriteString("\n")
	}
}

// patchHunkRange is a side of a hunk's header: its start alone when it is
// one line, as git writes them, and its start and count otherwise.
func patchHunkRange(start, lines int) string {
	if lines == 1 {
		return strconv.Itoa(start)
	}
	return strconv.Itoa(start) + "," + strconv.Itoa(lines)
}

// patchSide is a side of a --- or +++ line: the file's name with git's a/
// or b/, and /dev/null for the side of a file added or deleted.
func patchSide(name, prefix string) string {
	if name == "" {
		return "/dev/null"
	}
	return patchNameText(prefix, name)
}

// patchNameText is a name as a patch writes it: quoted, as a C string, when
// it holds a byte that the headers of a patch read otherwise, as git quotes
// them, and with its prefix as it is when not.
func patchNameText(prefix, name string) string {
	s := prefix + name
	quote := false
	for i := 0; i < len(s) && !quote; i++ {
		switch c := s[i]; {
		case c == ' ', c == '"', c == '\\', c < 0x20, c >= 0x7f:
			quote = true
		}
	}
	if !quote {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"':
			b.WriteString(`\"`)
		case c == '\\':
			b.WriteString(`\\`)
		case c == '\t':
			b.WriteString(`\t`)
		case c == '\n':
			b.WriteString(`\n`)
		case c < 0x20 || c >= 0x7f:
			// Octal, a byte at a time, as git writes a name that is not
			// ASCII, which strconv.Unquote reads back too.
			fmt.Fprintf(&b, "\\%03o", c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

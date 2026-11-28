package editor

import (
	"reflect"
	"strings"
	"testing"
)

func TestDiffPatch(t *testing.T) {
	lines := func(n int) string {
		var b strings.Builder
		for i := 1; i <= n; i++ {
			b.WriteString(strings.Repeat("x", i))
			b.WriteString("\n")
		}
		return b.String()
	}
	tests := []struct {
		name             string
		oldName, newName string
		old, new, want   string
	}{
		{
			name:    "a change",
			oldName: "f", newName: "f",
			old: "a\nb\nc\nd\ne\n",
			new: "a\nb\nc\nX\ne\n",
			want: "diff --git a/f b/f\n--- a/f\n+++ b/f\n" +
				"@@ -1,5 +1,5 @@\n a\n b\n c\n-d\n+X\n e\n",
		},
		{
			name:    "a file added",
			oldName: "", newName: "f",
			old:  "",
			new:  "a\nb\n",
			want: "--- /dev/null\n+++ b/f\n@@ -0,0 +1,2 @@\n+a\n+b\n",
		},
		{
			name:    "a file deleted",
			oldName: "f", newName: "",
			old: "a\nb\n",
			new: "",
			want: "diff --git a/f b/f\n--- a/f\n+++ /dev/null\n" +
				"@@ -1,2 +0,0 @@\n-a\n-b\n",
		},
		{
			name:    "a line added at the start",
			oldName: "f", newName: "f",
			old: "a\nb\n",
			new: "X\na\nb\n",
			want: "diff --git a/f b/f\n--- a/f\n+++ b/f\n" +
				"@@ -1,2 +1,3 @@\n+X\n a\n b\n",
		},
		{
			name:    "a line added at the end",
			oldName: "f", newName: "f",
			old: "a\nb\n",
			new: "a\nb\nX\n",
			want: "diff --git a/f b/f\n--- a/f\n+++ b/f\n" +
				"@@ -1,2 +1,3 @@\n a\n b\n+X\n",
		},
		{
			name:    "no newline at the end of either",
			oldName: "f", newName: "f",
			old: "a\nb",
			new: "a\nc",
			want: "diff --git a/f b/f\n--- a/f\n+++ b/f\n" +
				"@@ -1,2 +1,2 @@\n a\n-b\n" + noNewline + "\n+c\n" + noNewline + "\n",
		},
		{
			name:    "a newline added at the end",
			oldName: "f", newName: "f",
			old: "a\nb",
			new: "a\nb\n",
			want: "diff --git a/f b/f\n--- a/f\n+++ b/f\n" +
				"@@ -1,2 +1,2 @@\n a\n-b\n" + noNewline + "\n+b\n",
		},
		{
			name:    "a newline lost at the end",
			oldName: "f", newName: "f",
			old: "a\nb\n",
			new: "a\nb",
			want: "diff --git a/f b/f\n--- a/f\n+++ b/f\n" +
				"@@ -1,2 +1,2 @@\n a\n-b\n+b\n" + noNewline + "\n",
		},
		{
			// git rewrites the last line here too: a marker after a context
			// line would say that both sides end without a newline.
			name:    "a newline added at the end, and a line changed",
			oldName: "f", newName: "f",
			old: "a\nb",
			new: "X\nb\n",
			want: "diff --git a/f b/f\n--- a/f\n+++ b/f\n" +
				"@@ -1,2 +1,2 @@\n-a\n-b\n" + noNewline + "\n+X\n+b\n",
		},
		{
			name:    "a newline lost at the end, and a line changed",
			oldName: "f", newName: "f",
			old: "a\nb\n",
			new: "X\nb",
			want: "diff --git a/f b/f\n--- a/f\n+++ b/f\n" +
				"@@ -1,2 +1,2 @@\n-a\n-b\n+X\n+b\n" + noNewline + "\n",
		},
		{
			name:    "one marker where both end without a newline",
			oldName: "f", newName: "f",
			old: "a\nb\nc\nd",
			new: "X\nb\nc\nd",
			want: "diff --git a/f b/f\n--- a/f\n+++ b/f\n" +
				"@@ -1,4 +1,4 @@\n-a\n+X\n b\n c\n d\n" + noNewline + "\n",
		},
		{
			name:    "a name with a space",
			oldName: "x y.txt", newName: "x y.txt",
			old: "one\nz\n",
			new: "two\nz\n",
			want: "diff --git a/x y.txt b/x y.txt\n" +
				"--- a/x y.txt\t\n+++ b/x y.txt\t\n" +
				"@@ -1,2 +1,2 @@\n-one\n+two\n z\n",
		},
		{
			name:    "carriage returns of a file with them",
			oldName: "f", newName: "f",
			old: "a\r\nb\r\n",
			new: "a\r\nc\r\n",
			want: "diff --git a/f b/f\n--- a/f\n+++ b/f\n" +
				"@@ -1,2 +1,2 @@\n a\r\n-b\r\n+c\r\n",
		},
		{
			name:    "a change at each end of a long file",
			oldName: "f", newName: "f",
			old: lines(20),
			new: strings.Replace(strings.Replace(lines(20), "x\n", "2\n", 1), strings.Repeat("x", 18)+"\n", "18\n", 1),
			want: "diff --git a/f b/f\n--- a/f\n+++ b/f\n" +
				"@@ -1,4 +1,4 @@\n-x\n+2\n xx\n xxx\n xxxx\n" +
				"@@ -15,6 +15,6 @@\n" +
				" " + strings.Repeat("x", 15) + "\n" +
				" " + strings.Repeat("x", 16) + "\n" +
				" " + strings.Repeat("x", 17) + "\n" +
				"-" + strings.Repeat("x", 18) + "\n" +
				"+" + "18\n" +
				" " + strings.Repeat("x", 19) + "\n" +
				" " + strings.Repeat("x", 20) + "\n",
		},
		{
			name:    "no change",
			oldName: "f", newName: "f",
			old:  "a\nb\n",
			new:  "a\nb\n",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := DiffPatch(tt.oldName, tt.newName, tt.old, tt.new)
			if got := p.Format(); got != tt.want {
				t.Errorf("the patch is\n%s\nwant\n%s", got, tt.want)
			}
			got, err := p.Apply(tt.old)
			if err != nil {
				t.Fatalf("applying it: %v", err)
			}
			if got != tt.new {
				t.Errorf("applying it gives %q, want %q", got, tt.new)
			}
		})
	}
}

func TestDiffPatchContext(t *testing.T) {
	text := func(n int) string {
		var b strings.Builder
		for i := 1; i <= n; i++ {
			b.WriteString(strings.Repeat("x", i))
			b.WriteString("\n")
		}
		return b.String()
	}
	// Twenty lines, of which the tenth changes: the hunk shows three lines
	// before and after it.
	old, changed := text(20), strings.Replace(text(20), strings.Repeat("x", 10)+"\n", "ten\n", 1)
	p := DiffPatch("f", "f", old, changed)
	if len(p.Hunks) != 1 {
		t.Fatalf("got %d hunks, want 1", len(p.Hunks))
	}
	h := p.Hunks[0]
	if h.OldStart != 7 || h.OldLines != 7 || h.NewStart != 7 || h.NewLines != 7 {
		t.Errorf("the hunk is -%d,%d +%d,%d, want -7,7 +7,7", h.OldStart, h.OldLines, h.NewStart, h.NewLines)
	}
}

func TestDiffPatchGroups(t *testing.T) {
	// Two changes are one hunk while their context reaches from one to the
	// other, three lines on each side.
	text := func(n int) string {
		var b strings.Builder
		for i := 1; i <= n; i++ {
			b.WriteString(strings.ReplaceAll(strings.Repeat("x", i), "x", string(rune('a'+i-1))))
			b.WriteString("\n")
		}
		return b.String()
	}
	change := func(text string, at ...int) string {
		lines := strings.Split(text, "\n")
		for _, i := range at {
			lines[i-1] = "changed"
		}
		return strings.Join(lines, "\n")
	}
	tests := []struct {
		name string
		at   []int
		want int
	}{
		{"next to each other", []int{5, 6}, 1},
		{"six lines apart", []int{5, 12}, 1},
		{"seven lines apart", []int{5, 13}, 2},
		{"far apart", []int{2, 40}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old, new := text(50), change(text(50), tt.at...)
			p := DiffPatch("f", "f", old, new)
			if len(p.Hunks) != tt.want {
				t.Errorf("got %d hunks, want %d", len(p.Hunks), tt.want)
			}
			if got, err := p.Apply(old); err != nil || got != new {
				t.Errorf("applying it: %q, %v", got, err)
			}
		})
	}
}

func TestDiffPatchRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		old, new string
	}{
		{"a line changed", "a\nb\nc\n", "a\nB\nc\n"},
		{"lines deleted", "a\nb\nc\nd\n", "a\nd\n"},
		{"lines added", "a\nd\n", "a\nb\nc\nd\n"},
		{"the first line", "a\nb\nc\n", "A\nb\nc\n"},
		{"the last line", "a\nb\nc\n", "a\nb\nC\n"},
		{"only one line", "a\n", "b\n"},
		{"all of it", "a\nb\n", "c\nd\n"},
		{"text of one line from none", "", "a\n"},
		{"a text of nothing", "a\n", ""},
		{"text of no newline", "a\nb\nc", "a\nb\nC"},
		{"a newline at the end again", "a\nb", "a\nb\n"},
		{"a newline at the end lost", "a\nb\n", "a\nb"},
		{"blank lines", "a\n\n\nb\n", "a\n\nb\n"},
		{"spaces", "a \n b\n", "a \n  b\n"},
		{"wide text", "日\n本\n語\n", "日\n语\n"},
		{"two hunks", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n", "one\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\ntwelve\n"},
		{"a hunk at each end", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n", "x\n2\n3\n4\n5\n6\n7\n8\n9\ny\n"},
		{"carriage returns", "a\r\nb\r\nc\r\n", "a\r\nB\r\nc\r\n"},
		{"a carriage return added", "a\nb\n", "a\r\nb\n"},
		{"no newline after a carriage return", "a\r\nb\r", "a\r\nc\r"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := DiffPatch("f", "f", tt.old, tt.new)
			text := p.Format()
			files, err := ParsePatch(text)
			if err != nil {
				t.Fatalf("parsing\n%s\n%v", text, err)
			}
			if len(files) != 1 {
				t.Fatalf("got %d files, want 1:\n%s", len(files), text)
			}
			if !reflect.DeepEqual(files[0], p) {
				t.Errorf("the patch parses as %+v, want %+v\n%s", files[0], p, text)
			}
			if again := files[0].Format(); again != text {
				t.Errorf("formatting it again\n%s\nwant\n%s", again, text)
			}
			got, err := files[0].Apply(tt.old)
			if err != nil {
				t.Fatalf("applying it: %v", err)
			}
			if got != tt.new {
				t.Errorf("applying it gives %q, want %q\n%s", got, tt.new, text)
			}
		})
	}
}

// TestDiffPatchEverySmallPair writes the patch of every pair of small texts
// and applies it, as git's xdiff would: a last line without its newline is
// not the line of the same text with one, so it is never context for it. A
// patch that takes one for the other cuts a hunk with a "\ No newline at end
// of file" line and does not apply to the text it names.
func TestDiffPatchEverySmallPair(t *testing.T) {
	texts := []string{""}
	var grow func(prefix []string, n int)
	grow = func(prefix []string, n int) {
		if len(prefix) > 0 {
			body := strings.Join(prefix, "\n")
			texts = append(texts, body, body+"\n")
		}
		if n == 0 {
			return
		}
		for _, l := range []string{"a", "b", "c"} {
			grow(append(prefix, l), n-1)
		}
	}
	grow(nil, 4)
	for _, old := range texts {
		for _, new := range texts {
			p := DiffPatch("f", "f", old, new)
			text := p.Format()
			got, err := p.Apply(old)
			if err != nil {
				t.Fatalf("applying %q to %q: %v\n%s", new, old, err, text)
			}
			if got != new {
				t.Fatalf("applying the patch of %q to %q gives %q\n%s", new, old, got, text)
			}
			// A marker says that a line of a side ends without a newline, so
			// it ends its hunk or follows the last line taken out of the old
			// text, which the lines put in after it follow. One that follows
			// a line put in ends the hunk.
			for _, h := range p.Hunks {
				for i, l := range h.Lines {
					if l != noNewline {
						continue
					}
					if i > 0 && strings.HasPrefix(h.Lines[i-1], "+") && i != len(h.Lines)-1 {
						t.Fatalf("a marker of %q to %q is not the last line of its hunk\n%s", old, new, text)
					}
					for _, after := range h.Lines[i+1:] {
						if after == noNewline {
							break
						}
						if !strings.HasPrefix(after, "+") {
							t.Fatalf("a marker of %q to %q is followed by %q\n%s", old, new, after, text)
						}
					}
				}
			}
		}
	}
}

func TestFormatPatch(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{
			name: "a file changed",
			text: "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1,3 +1,3 @@ func main\n a\n-b\n+B\n c\n",
		},
		{
			name: "a file added",
			text: "diff --git a/x b/x\nnew file mode 100644\n--- /dev/null\n+++ b/x\n@@ -0,0 +1,2 @@\n+a\n+b\n",
		},
		{
			name: "a file added of no mode",
			text: "--- /dev/null\n+++ b/x\n@@ -0,0 +1,2 @@\n+a\n+b\n",
		},
		{
			name: "a file deleted",
			text: "diff --git a/x b/x\ndeleted file mode 100644\n--- a/x\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-a\n-b\n",
		},
		{
			name: "a mode changed",
			text: "diff --git a/x b/x\nold mode 100644\nnew mode 100755\n",
		},
		{
			name: "a file renamed",
			text: "diff --git a/old b/new\nsimilarity index 100%\nrename from old\nrename to new\n",
		},
		{
			name: "a file renamed and changed",
			text: "diff --git a/old b/new\nsimilarity index 90%\nrename from old\nrename to new\n--- a/old\n+++ b/new\n@@ -1 +1 @@\n-a\n+b\n",
		},
		{
			name: "a binary file",
			text: "diff --git a/x b/x\nBinary files a/x and b/x differ\n",
		},
		{
			name: "a binary file added",
			text: "diff --git a/x b/x\nnew file mode 100644\nBinary files /dev/null and b/x differ\n",
		},
		{
			name: "names git quotes",
			text: "diff --git \"a/x y\" \"b/x y\"\n--- \"a/x y\"\n+++ \"b/x y\"\n@@ -1 +1 @@\n-a\n+b\n",
		},
		{
			name: "a name that is not ASCII",
			text: "diff --git \"a/\\346\\226\\207.txt\" \"b/\\346\\226\\207.txt\"\n--- \"a/\\346\\226\\207.txt\"\n+++ \"b/\\346\\226\\207.txt\"\n@@ -1 +1 @@\n-a\n+b\n",
		},
		{
			name: "a diff -u patch",
			text: "--- a/x\n+++ b/x\n@@ -1,2 +1,2 @@\n a\n-b\n+c\n",
		},
		{
			name: "lines of no newline",
			text: "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n" + noNewline + "\n+b\n" + noNewline + "\n",
		},
		{
			name: "two files",
			text: "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n" +
				"diff --git a/y b/y\n--- a/y\n+++ b/y\n@@ -1 +1 @@\n-c\n+d\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := ParsePatch(tt.text)
			if err != nil {
				t.Fatalf("parsing the test's patch: %v", err)
			}
			text := FormatPatch(files)
			got, err := ParsePatch(text)
			if err != nil {
				t.Fatalf("parsing\n%s\n%v", text, err)
			}
			if !reflect.DeepEqual(got, files) {
				t.Errorf("the patch of\n%s\nparses as %+v, want %+v", text, got, files)
			}
			if again := FormatPatch(got); again != text {
				t.Errorf("formatting it again\n%s\nwant\n%s", again, text)
			}
		})
	}
}

func TestFormatPatchNames(t *testing.T) {
	// The names git quotes, and those it does not, as ParsePatch reads them
	// back.
	tests := []struct{ name, prefix, want string }{
		{"x", "a/", "a/x"},
		{"x y", "a/", "a/x y"},
		{"x\tz", "b/", `"b/x\tz"`},
		{"x\ny", "a/", `"a/x\ny"`},
		{"x\"y", "a/", `"a/x\"y"`},
		{"x\\y", "a/", `"a/x\\y"`},
		{"文.txt", "b/", `"b/\346\226\207.txt"`},
		{"café", "", `"caf\303\251"`},
	}
	for _, tt := range tests {
		if got := patchNameText(tt.prefix, tt.name); got != tt.want {
			t.Errorf("patchNameText(%q, %q) = %s, want %s", tt.prefix, tt.name, got, tt.want)
		}
	}
	// A name with a space ends the field of a --- or +++ line, so git puts a
	// tab after it, and so does a patch of it here.
	if got, want := patchSide("x y", "a/"), "a/x y\t"; got != want {
		t.Errorf("patchSide(%q, %q) = %q, want %q", "x y", "a/", got, want)
	}
	if got, want := patchSide("x", "a/"), "a/x"; got != want {
		t.Errorf("patchSide(%q, %q) = %q, want %q", "x", "a/", got, want)
	}
}

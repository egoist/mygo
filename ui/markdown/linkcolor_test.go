package markdown

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
)

// TestLinkColor colors the text of the links LinkColor picks, a
// reference's and code's included, and leaves the others the theme's.
func TestLinkColor(t *testing.T) {
	muted := ui.Color{R: 120, G: 120, B: 120, A: 255}
	src := "[aside](x:aside) [plain](x:plain) #12 `fn`\n"
	o := &Options{
		LinkColor: func(dest string) (Color, bool) {
			return muted, strings.HasSuffix(dest, "aside")
		},
		Resolve: func(in parser.Inline) (string, bool) { return "x:ref-aside", true },
		CodeLink: func(code string) (string, bool) {
			return "x:code-aside", true
		},
	}
	_, get := rvView(&src, o)
	st := get()
	colors := map[string]ui.Color{}
	for _, pc := range st.paras[1].p.pieces {
		for _, s := range pc.spans {
			colors[s.Text] = s.Color
		}
	}
	for text, want := range map[string]ui.Color{"aside": muted, "#12": muted, "fn": muted, "plain": {}} {
		if got := colors[text]; got != want {
			t.Errorf("%q colored %v, want %v; spans %v", text, got, want, colors)
		}
	}
}

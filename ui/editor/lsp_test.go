package editor

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The completions shown put first those whose words the letters typed
// start, in camelCase and snake_case alike, then a word typed whole, then
// the shorter.
func TestCodeEditorCompletionsRankWordStartsFirst(t *testing.T) {
	for _, c := range []struct {
		typed string
		items []string
		want  []string
	}{
		{"sv", []string{"savior", "sliver", "serverVersion"}, []string{"serverVersion", "savior", "sliver"}},
		{"fb", []string{"fobar", "foo_bar"}, []string{"foo_bar", "fobar"}},
		{"len", []string{"maxLength", "length", "len"}, []string{"len", "length", "maxLength"}},
	} {
		ct := newEditorTest(t, "go", "|")
		ct.st.OnCompletion = func(r CompletionRequest) {
			var items []Completion
			for _, l := range c.items {
				items = append(items, Completion{Label: l})
			}
			ct.st.ShowCompletions(r.Offset-len(r.Prefix), items)
		}
		var shown []string
		ct.st.RenderCompletions = func(c *ui.Context, panel ui.Element, m *CompletionMenu) {
			shown = shown[:0]
			for _, it := range m.Items() {
				shown = append(shown, it.Label)
			}
			DefaultCompletions(c, panel, m)
		}
		ct.typ(c.typed)
		if !slices.Equal(shown, c.want) {
			t.Errorf("%q shows %q, want %q", c.typed, shown, c.want)
		}
	}
}

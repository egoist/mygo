package editor

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Bindings read from a keymap, as code-editor.md shows: keys written as
// menus' accelerators, a chord's two separated by a space, and commands
// by their names; an entry of more keys than a Binding holds is skipped.
func TestBindingsFromAKeymap(t *testing.T) {
	keymap := map[string]string{
		"CmdOrCtrl+K CmdOrCtrl+U":             "uppercase",
		"Alt+Shift+L":                         "lowercase",
		"CmdOrCtrl+K CmdOrCtrl+J CmdOrCtrl+L": "titleCase",
	}
	var bindings []Binding
	for keys, name := range keymap {
		command, ok := CommandNamed(name)
		if !ok {
			t.Fatalf("no command %q", name)
		}
		fields := strings.Fields(keys)
		if len(fields) > 2 {
			continue
		}
		b := Binding{Command: command}
		for i, s := range fields {
			mods, key, err := ui.ParseShortcut(s)
			if err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				b.Mods, b.Key = mods, key
			} else {
				b.ThenMods, b.ThenKey = mods, key
			}
		}
		bindings = append(bindings, b)
	}
	if len(bindings) != 2 {
		t.Fatalf("read %d bindings, want the 2 of one or two keys", len(bindings))
	}

	ct := newEditorTest(t, "", "ab|")
	ct.st.Bindings = bindings
	ct.tt.Frame()
	ct.st.Select(0, 2)
	ct.key(ui.Cmd, ui.KeyK)
	ct.key(ui.Cmd, ui.KeyU)
	ct.key(0, ui.KeyRight)
	ct.want("AB|")
	ct.st.Select(0, 2)
	ct.key(ui.Alt|ui.Shift, ui.KeyL)
	ct.key(0, ui.KeyRight)
	ct.want("ab|")
}

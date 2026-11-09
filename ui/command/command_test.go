package command

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	_ "github.com/egoist/mygo/locales/zh-Hans"
	"github.com/egoist/mygo/ui"
)

func TestRanking(t *testing.T) {
	items := []Item{
		{ID: "all", Title: "Open All Files"},
		{ID: "file", Title: "Open File"},
		{ID: "folder", Title: "Open Folder"},
		{ID: "settings", Title: "Preferences", Keywords: "settings configuration"},
		{ID: "unicode", Title: "Σ Settings 中文"},
	}
	for _, tc := range []struct {
		q    string
		want []string
	}{
		{"", []string{"all", "file", "folder", "settings", "unicode"}},
		{"Open File", []string{"file", "all"}},
		{"of", []string{"file", "folder", "all", "settings"}},
		{"configuration", []string{"settings"}},
		{"ς 中文", []string{"unicode"}},
		{"no matches", nil},
	} {
		s := State{Query: tc.q}
		s.refresh(items, true)
		var got []string
		for _, m := range s.matches {
			got = append(got, items[m.index].ID)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%q: %v want %v", tc.q, got, tc.want)
		}
	}
}

func paletteTest(items *[]Item) (*State, *ui.Tester, *[]string) {
	s := &State{Label: "Commands", Placeholder: "Search commands"}
	var picked []string
	tt := ui.NewTester(func(c *ui.Context) {
		if ui.Button(c, "Open palette").Clicked() {
			s.Open = true
		}
		if id, ok := View(c, s, *items); ok {
			picked = append(picked, id)
		}
	}, 800, 600)
	return s, tt, &picked
}
func click(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
}
func TestInteraction(t *testing.T) {
	items := []Item{{ID: "open", Title: "Open File"}, {ID: "save", Title: "Save", Disabled: true}, {ID: "settings", Title: "Settings", Keywords: "preferences"}}
	s, tt, picked := paletteTest(&items)
	click(t, tt, "Open palette")
	active, _ := tt.TextInputActive()
	if !s.Open || !active {
		t.Fatal("input did not take focus")
	}
	tt.Key(0, ui.KeyDown)
	if s.items[s.matches[s.selected].index].ID != "settings" {
		t.Fatal("disabled result was not skipped")
	}
	if a, ok := tt.Access("Settings"); !ok || !a.Selected || !a.Focused {
		t.Fatalf("active option: %+v %v", a, ok)
	}
	tt.Key(0, ui.KeyEnter)
	if s.Open || !slices.Equal(*picked, []string{"settings"}) {
		t.Fatalf("choose: open=%v picked=%v", s.Open, *picked)
	}
	tt.Frame()
	if len(*picked) != 1 {
		t.Fatal("action repeated")
	}
	if a, ok := tt.Access("Open palette"); !ok || !a.Focused {
		t.Fatalf("focus not restored: %+v", a)
	}
	click(t, tt, "Open palette")
	tt.Type("prefs")
	if s.selected < 0 || s.items[s.matches[s.selected].index].ID != "settings" {
		t.Fatalf("keyword filter: %+v", s.matches)
	}
	click(t, tt, "Settings")
	if !slices.Equal(*picked, []string{"settings", "settings"}) || s.Open {
		t.Fatalf("pointer: %v %v", *picked, s.Open)
	}
	click(t, tt, "Open palette")
	if s.Query != "" {
		t.Fatal("query survived reopening")
	}
	tt.Type("zzzz")
	if !tt.HasText("No results") {
		t.Fatalf("empty message: %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	if !s.Open || len(*picked) != 2 {
		t.Fatal("empty list chose a command")
	}
	tt.Key(0, ui.KeyEscape)
	if s.Open {
		t.Fatal("escape did not dismiss")
	}
	click(t, tt, "Open palette")
	tt.ClickAt(2, 2)
	if s.Open {
		t.Fatal("outside click did not dismiss")
	}
}

func TestDynamicItems(t *testing.T) {
	items := []Item{{ID: "a", Title: "Alpha"}, {ID: "b", Title: "Beta"}, {ID: "c", Title: "Gamma"}}
	s, tt, picked := paletteTest(&items)
	click(t, tt, "Open palette")
	tt.Key(0, ui.KeyDown)
	items = []Item{items[2], items[1], items[0]}
	tt.Frame()
	if s.items[s.matches[s.selected].index].ID != "b" {
		t.Fatal("choice did not follow ID")
	}
	items[1].Disabled = true
	tt.Frame()
	if s.items[s.matches[s.selected].index].ID != "c" {
		t.Fatal("disabled choice did not move")
	}
	tt.Key(0, ui.KeyEnter)
	if !slices.Equal(*picked, []string{"c"}) {
		t.Fatal(*picked)
	}
}

func TestLargeList(t *testing.T) {
	items := make([]Item, 10000)
	for i := range items {
		items[i] = Item{ID: fmt.Sprint(i), Title: fmt.Sprintf("Command %05d", i)}
	}
	s, tt, picked := paletteTest(&items)
	click(t, tt, "Open palette")
	if n := len(tt.Texts()); n > 100 {
		t.Fatalf("built entire list: %d texts", n)
	}
	for range 20 {
		tt.Key(0, ui.KeyPageDown)
	}
	id := s.items[s.matches[s.selected].index].ID
	name := s.items[s.matches[s.selected].index].Title
	if r, ok := tt.Find(name); !ok || r.H <= 0 {
		t.Fatalf("active row was not scrolled into view: %s %+v", name, r)
	}
	tt.Key(0, ui.KeyEnter)
	if !slices.Equal(*picked, []string{id}) {
		t.Fatal(*picked, id)
	}
}

func TestDisabledAndLocale(t *testing.T) {
	items := []Item{{ID: "a", Title: "Unavailable", Disabled: true}}
	s, tt, picked := paletteTest(&items)
	click(t, tt, "Open palette")
	click(t, tt, "Unavailable")
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyEnter)
	if !s.Open || len(*picked) != 0 {
		t.Fatal("disabled command ran")
	}
	s.Open = false
	tt.Frame()
	tt.SetLocale("zh-Hans")
	click(t, tt, "Open palette")
	tt.Type("nope")
	if tt.HasText("No results") {
		t.Fatal("empty message not localized")
	}
}

func TestUnchangedSearchAllocs(t *testing.T) {
	s := State{Query: "of"}
	items := []Item{{ID: "a", Title: "Open File"}}
	s.refresh(items, true)
	if n := testing.AllocsPerRun(100, func() { s.refresh(items, false) }); n != 0 {
		t.Fatal("unchanged filtering allocations", n)
	}
}
func TestInvalidIDs(t *testing.T) {
	for _, items := range [][]Item{{{Title: "No ID"}}, {{ID: "a"}, {ID: "a"}}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("invalid IDs accepted")
				}
			}()
			new(State).refresh(items, true)
		}()
	}
}

func TestCompositionAndAccessibleStructure(t *testing.T) {
	items := []Item{{ID: "a", Title: "中文设置", Detail: "Editor settings"}, {ID: "b", Title: "Open File"}}
	s, tt, picked := paletteTest(&items)
	click(t, tt, "Open palette")
	tt.Compose("中文", 2)
	if s.Query != "" || len(*picked) != 0 {
		t.Fatal("preedit ran or filtered commands")
	}
	tt.Compose("", 0)
	tt.Type("中文")
	if len(s.matches) != 1 {
		t.Fatal(s.matches)
	}
	a, ok := tt.AccessByRole(ui.RoleComboBox, "Commands")
	if !ok || a.AutoComplete != ui.AutoCompleteList || a.HasPopup != ui.PopupListBox || !a.Expanded || len(a.Controls) != 1 {
		t.Fatalf("combobox: %+v", a)
	}
	a, ok = tt.Access("中文设置")
	if !ok || !a.Selected || !a.Focused || a.PosInSet != 1 || a.SetSize != 1 || a.Description != "Editor settings" {
		t.Fatalf("option: %+v", a)
	}
	tt.Key(0, ui.KeyEnter)
	if !slices.Equal(*picked, []string{"a"}) {
		t.Fatal(*picked)
	}
}

func TestViewportAndWheel(t *testing.T) {
	items := make([]Item, 1000)
	for i := range items {
		items[i] = Item{ID: fmt.Sprint(i), Title: fmt.Sprintf("Command %04d", i), Detail: "A command"}
	}
	s, tt, picked := paletteTest(&items)
	click(t, tt, "Open palette")
	tt.SetSize(320, 220)
	tt.SetScale(2)
	tt.SetDirection(ui.RightToLeft)
	r, _ := tt.Find("Command 0000")
	tt.Scroll(r.X+10, r.Y+10, 0, 4800)
	if s.scroll.Y < 1000 {
		t.Fatalf("wheel did not scroll: %+v", s.scroll)
	}
	first := int(s.scroll.Y/s.rowHeight) + 1
	name := fmt.Sprintf("Command %04d", first)
	click(t, tt, name)
	if !slices.Equal(*picked, []string{fmt.Sprint(first)}) {
		t.Fatal(*picked)
	}
}

func FuzzFuzzySubsequence(f *testing.F) {
	for _, s := range [][2]string{{"Open File", "of"}, {"Σ ς σ", "Σσ"}, {"中文设置", "中设"}, {"Kelvin K", "k"}, {"", ""}} {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, text, query string) {
		if len(text) > 512 || len(query) > 512 {
			return
		}
		qs := []rune(query)
		j := 0
		for _, r := range text {
			if j < len(qs) && strings.EqualFold(string(r), string(qs[j])) {
				j++
			}
		}
		_, got := fuzzy(text, qs)
		if want := j == len(qs); got != want {
			t.Fatalf("%q %q: %v want %v", text, query, got, want)
		}
	})
}

func BenchmarkSearch10000(b *testing.B) {
	items := make([]Item, 10000)
	for i := range items {
		items[i] = Item{ID: fmt.Sprint(i), Title: fmt.Sprintf("Open File %05d", i), Keywords: "read browse file"}
	}
	var s State
	s.refresh(items, true)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Query = []string{"of", "op fi"}[i%2]
		s.refresh(items, false)
	}
}

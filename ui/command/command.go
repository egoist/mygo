// Package command builds a searchable command palette on ui's public API.
package command

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/internal/l10n"
	"github.com/egoist/mygo/ui"
)

// Item is an app command. ID must be nonempty and unique in a palette;
// Title is its name, Detail optional secondary text, and Keywords search
// aliases. Shortcut is the key that runs the item, drawn as a menu draws
// one: the app owns the binding itself. A disabled item remains visible but
// cannot be chosen.
type Item struct {
	ID, Title, Detail, Keywords string
	Shortcut                    Shortcut
	Disabled                    bool
}

// Shortcut is a key with modifiers, drawn as a menu draws one: Cmd and K
// read "⌘K" on a Mac and "Ctrl+K" elsewhere. The zero value draws nothing.
type Shortcut struct {
	Mods ui.Modifiers
	Key  ui.Key
}

// State is a palette's state, kept by the app for one view in one window.
// Set Open to show it, and call View every frame, including while closed.
// Closing clears Query, so an app can fill one in before opening the
// palette. Items can change while it is open; the keyboard choice follows
// its ID unless the query changes or the item disappears. As with other UI
// state, use it on the UI thread.
type State struct {
	Open  bool
	Query string
	// Label names the dialog and input; Placeholder and EmptyText override
	// their text. Empty strings use MyGo's localized Search and No results.
	Label, Placeholder, EmptyText string
	// Width and Height are DIPs: 560 and 320 by default, capped by the
	// window. Height is the results list's height, not the whole dialog.
	Width, Height float32

	shown     bool
	query     string
	items     []Item
	matches   []match
	selected  int
	focus     bool
	pending   string
	scroll    ui.ScrollState
	rowHeight float32
}

type match struct{ index, score int }

// View shows a modal palette while s.Open is true. Typing searches titles
// and keywords by case-insensitive subsequences, with exact/prefix and
// word-boundary matches ranked first; whitespace separates search terms.
// Up/Down and Page Up/Down move past disabled results, Enter chooses, and
// Escape or a click outside closes. Only visible result rows are built.
// The input keeps focus, with the chosen option its active descendant.
//
// The ID returned by a choice is reported once, and the palette closes
// before it is returned. Dispatch it after View; actions belong to the app.
// Invalid (empty or duplicate) IDs panic when items change.
func View(c *ui.Context, s *State, items []Item) (string, bool) {
	if !s.Open {
		if s.shown {
			s.Query = ""
		}
		s.shown = false
		return s.take()
	}
	opening := !s.shown
	if opening {
		s.scroll.X, s.scroll.Y = 0, 0
	}
	s.shown = true
	texts := l10n.In(c.Locale())
	label, placeholder, empty := s.Label, s.Placeholder, s.EmptyText
	if label == "" {
		label = texts.Search()
	}
	if placeholder == "" {
		placeholder = texts.Search()
	}
	if empty == "" {
		empty = texts.NoResults()
	}
	width, height := s.Width, s.Height
	if width <= 0 {
		width = 560
	}
	if height <= 0 {
		height = 320
	}
	t := c.Theme()
	rowHeight := max(float32(48), t.FontSize*1.5+28)
	if s.rowHeight != rowHeight {
		s.rowHeight = rowHeight
		s.scroll.Y = float32(max(0, s.selected)) * rowHeight
	}
	ui.DialogBase(c, &s.Open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, .35)).Padding(16)
		panel.Width(width).MaxWidthPercent(100).MaxHeightPercent(100).Padding(8).Gap(8).
			Background(t.Background).Border(1, t.Border).Radius(12).Label(label)
		// DialogBase moves focus to its first field and restores the opener
		// on close. Let the dialog establish that focus scope first.
		field := ui.TextInput(c, &s.Query).Label(label).Placeholder(placeholder).
			Role(ui.RoleComboBox).AutoComplete(ui.AutoCompleteList).HasPopup(ui.PopupListBox).Expanded(true)
		s.refresh(items, opening)
		moved := false
		switch {
		case field.Shortcut(0, ui.KeyDown):
			s.step(1)
			moved = true
		case field.Shortcut(0, ui.KeyUp):
			s.step(-1)
			moved = true
		case field.Shortcut(0, ui.KeyPageDown):
			for range 8 {
				s.step(1)
			}
			moved = true
		case field.Shortcut(0, ui.KeyPageUp):
			for range 8 {
				s.step(-1)
			}
			moved = true
		}
		if (moved || s.focus) && s.selected >= 0 {
			view := height
			if h := s.scroll.Bounds(c).H; h > 0 {
				view = min(view, h)
			}
			top := float32(s.selected) * rowHeight
			s.scroll.Y = max(min(s.scroll.Y, top), top+rowHeight-view)
			s.focus = false
		}
		if field.Submitted() && s.selected >= 0 {
			s.choose(s.items[s.matches[s.selected].index].ID)
		}
		// Rows have a fixed height, so the public ScrollState suffices to
		// virtualize them without List's additional list-item wrappers.
		// This leaves exactly one accessible option per command.
		n := len(s.matches)
		first := min(n, max(0, int(s.scroll.Y/rowHeight)-2))
		last := min(n, first+int(height/rowHeight)+5)
		list := ui.Scroll(c).TrackScroll(&s.scroll).Height(height).MinHeight(0).Shrink(1).
			Role(ui.RoleListBox).Label(label)
		field.Controls(list)
		list.Children(func() {
			ui.Box(c).Height(float32(first) * rowHeight).Shrink(0)
			for i := first; i < last; i++ {
				item := s.items[s.matches[i].index]
				chosen := i == s.selected
				row := ui.Row(c.Key(item.ID)).Height(rowHeight).MinHeight(rowHeight).Shrink(0).Gap(12).Padding(5, 10).AlignItems(ui.Center).
					Role(ui.RoleOption).Label(item.Title).Description(item.Detail).Checked(chosen).AccessSelected(chosen).
					PositionInSet(i+1, len(s.matches)).KeepFocus().Disabled(item.Disabled).Radius(6)
				if item.Shortcut.Key != ui.KeyUnknown {
					row.KeyShortcuts(item.Shortcut.Mods, item.Shortcut.Key)
				}
				if chosen {
					row.Background(t.Accent).TextColor(t.AccentText)
				} else if row.Hovered() {
					row.Background(t.SurfaceHover)
				}
				row.Children(func() {
					ui.Column(c).Grow(1).MinWidth(0).Children(func() {
						ui.Text(c, item.Title).FontWeight(500).SingleLine()
						if item.Detail != "" {
							text := ui.Text(c, item.Detail).FontSize(12).SingleLine()
							if !chosen {
								text.TextColor(t.TextMuted)
							}
						}
					})
					if item.Shortcut.Key != ui.KeyUnknown {
						ui.Text(c, ui.ShortcutText(item.Shortcut.Mods, item.Shortcut.Key)).FontSize(12)
					}
				})
				if chosen {
					field.ActiveDescendant(row)
				}
				if row.Clicked() {
					s.choose(item.ID)
				}
			}
			ui.Box(c).Height(float32(n-last) * rowHeight).Shrink(0)
			if n == 0 {
				ui.Text(c, empty).Padding(12).TextColor(t.TextMuted).Role(ui.RoleStatus)
			}
		})
	})
	if !s.Open {
		s.shown = false
		s.Query = ""
	}
	return s.take()
}

func (s *State) choose(id string)     { s.pending, s.Open = id, false }
func (s *State) take() (string, bool) { id := s.pending; s.pending = ""; return id, id != "" }

func (s *State) refresh(items []Item, force bool) {
	changed := !slices.Equal(s.items, items)
	queryChanged := s.Query != s.query
	if !force && !changed && !queryChanged {
		return
	}
	prev := s.selected
	selected := ""
	if !force && !queryChanged && s.selected >= 0 && s.selected < len(s.matches) {
		selected = s.items[s.matches[s.selected].index].ID
	}
	if changed {
		ids := make(map[string]bool, len(items))
		for _, it := range items {
			if it.ID == "" || ids[it.ID] {
				panic("command: item IDs must be nonempty and unique")
			}
			ids[it.ID] = true
		}
		old := s.items
		s.items = append(s.items[:0], items...)
		if len(items) < len(old) {
			clear(old[len(items):])
		}
	}
	s.query = s.Query
	s.matches = s.matches[:0]
	terms := strings.Fields(s.Query)
	queries := make([][]rune, len(terms))
	for i, term := range terms {
		queries[i] = []rune(term)
	}
	for i, it := range s.items {
		score, ok := itemScore(it, queries)
		if ok {
			s.matches = append(s.matches, match{i, score})
		}
	}
	slices.SortStableFunc(s.matches, func(a, b match) int { return cmp.Compare(b.score, a.score) })
	s.selected = -1
	for i, m := range s.matches {
		it := s.items[m.index]
		if it.Disabled {
			continue
		}
		if s.selected < 0 || it.ID == selected {
			s.selected = i
		}
		if it.ID == selected {
			break
		}
	}
	// Scroll only when the choice itself moves, not when items arrive while
	// the user is reading another part of the list.
	if s.selected != prev || force || queryChanged {
		s.focus = true
	}
}

func (s *State) step(delta int) {
	// Stop at the ends; a held arrow must not wrap unexpectedly.
	for i := s.selected + delta; i >= 0 && i < len(s.matches); i += delta {
		if !s.items[s.matches[i].index].Disabled {
			s.selected = i
			return
		}
	}
}

func itemScore(it Item, terms [][]rune) (int, bool) {
	score := 0
	for _, q := range terms {
		a, okA := fuzzy(it.Title, q)
		b, okB := fuzzy(it.Keywords, q)
		if !okA && !okB {
			return 0, false
		}
		if okA {
			a += 500
		}
		if !okA {
			a = b
		} else if okB {
			a = max(a, b)
		}
		score += a
	}
	return score, true
}

// fuzzy favors contiguous matches and starts of words (including camel
// case). SimpleFold handles Unicode case equivalents without allocating
// a normalized copy of each command for every search term. The match is
// tried from every word start, not only the first letter it finds, so
// "side" reads the whole word "Sidebar" rather than the S of "Show".
func fuzzy(text string, query []rune) (int, bool) {
	if len(query) == 0 {
		return 0, true
	}
	best, found := 0, false
	var prev rune
	pos := 0
	for _, r := range text {
		if foldEqual(r, query[0]) && wordStart(prev, r, pos) {
			if score, ok := fuzzyFrom(text, query, pos); ok && (!found || score > best) {
				best, found = score, true
			}
		}
		prev, pos = r, pos+1
	}
	if found {
		return best, true
	}
	// No word starts with the query's first letter: a match inside a word
	// still counts, as it did before.
	return fuzzyFrom(text, query, 0)
}

// wordStart reports whether a match at pos starts a word: the start of the
// text, the run after something that is not a letter or digit, or a camel
// case hump.
func wordStart(prev, r rune, pos int) bool {
	return pos == 0 || !unicode.IsLetter(prev) && !unicode.IsDigit(prev) || unicode.IsLower(prev) && unicode.IsUpper(r)
}

// fuzzyFrom scores the best subsequence match starting at start.
func fuzzyFrom(text string, query []rune, start int) (int, bool) {
	n := utf8.RuneCountInString(text)
	score, at, last, first := -n/4, 0, -2, -1
	var prev rune
	pos := 0
	for _, r := range text {
		if pos < start {
			prev, pos = r, pos+1
			continue
		}
		if foldEqual(r, query[at]) {
			if first < 0 {
				first = pos
			}
			score += 10 - pos
			if wordStart(prev, r, pos) {
				score += 24
			}
			if pos == last+1 {
				score += 14
			}
			last = pos
			at++
			if at == len(query) {
				if first == 0 && last == len(query)-1 {
					score += 2000
					if n == len(query) {
						score += 2000
					}
				}
				return score, true
			}
		}
		prev = r
		pos++
	}
	return 0, false
}
func foldEqual(a, b rune) bool {
	if a == b {
		return true
	}
	if a < utf8.RuneSelf && b < utf8.RuneSelf {
		if a >= 'A' && a <= 'Z' {
			a += 'a' - 'A'
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		return a == b
	}
	for r := unicode.SimpleFold(a); r != a; r = unicode.SimpleFold(r) {
		if r == b {
			return true
		}
	}
	return false
}

package ui

import (
	"fmt"
	"strconv"
	"time"
)

// TimeInput edits *tm's hours and minutes in the view's locale. Each
// segment takes the focus; Up/Down wrap it, digits set it, and Left/Right
// move between segments. A 12-hour clock also has an AM/PM segment, which
// Up/Down or Space toggle. Changed keeps the date, seconds, nanoseconds
// and location. Assistive technology sees localized spin buttons named
// after the field's developer-supplied Label.
//
//	ui.TimeInput(c, &app.alarm).Label("Alarm")
func TimeInput(c *Context, tm *time.Time) *Element {
	t, l := c.theme, c.Locale()
	f := Row(c).AlignItems(Center).Padding(t.Space(1.5), t.Space(2)).Radius(t.Radius).Background(t.Surface).Border(1, t.Border).Shrink(0).Role(RoleGroup)
	f.widget = "TimeInput"
	set := func(h, m int) {
		y, mo, d := tm.Date()
		next := time.Date(y, mo, d, h, m, tm.Second(), tm.Nanosecond(), tm.Location())
		if !next.Equal(*tm) {
			*tm = next
			f.st.changed = true
			c.rt.consumed = true
		}
	}
	var segs [3]*Element
	var order []int
	var fields [3]rune
	// Shared by segments: a digit can advance focus before its text echo
	// arrives (for example WM_KEYDOWN followed by WM_CHAR).
	echo := Local(f, "key echo", func() string { return "" })
	f.Children(func() {
		for _, part := range patternParts(l.timePattern()) {
			if part.field == 0 {
				Text(c, part.literal).TextColor(t.TextMuted).Padding(0, 1).Role(RoleNone)
				continue
			}
			k, value, lo, hi, name := 0, tm.Hour(), 0, 23, "hours"
			switch part.field {
			case 'h':
				value, lo, hi = tm.Hour()%12, 1, 12
				if value == 0 {
					value = 12
				}
			case 'K':
				value, hi = tm.Hour()%12, 11
			case 'k':
				lo, hi = 1, 24
				if value == 0 {
					value = 24
				}
			case 'm':
				k, value, hi, name = 1, tm.Minute(), 59, "minutes"
			case 'a':
				k, value, hi, name = 2, tm.Hour()/12, 1, "AM/PM"
			case 'H':
			default:
				continue
			}
			label := l.localDigits(fmt.Sprintf("%0*d", part.width, value))
			if k == 2 {
				label = l.data.Periods[value]
			}
			seg := Box(c).Key(k).Padding(0, t.Space(0.5)).Radius(t.Space(1)).Focusable().FocusRing(false).Role(RoleStepper)
			seg.flags |= flagTypeSelect
			if k != 2 {
				// Key events retain the old keyboard behavior; committed
				// text handles localized digits and Linux input methods.
				// A key's following text is an echo, not a second digit.
				s := seg.st
				appendDigits := func(digits string) {
					now := time.Now()
					if now.Sub(s.typedAt) >= typePause {
						s.typed = ""
					}
					s.typed += digits
					s.typedAt, s.typing = now, true
				}
				seg.TextCaret(Rect{W: 1, H: t.FontSize}).HandleInput(func(ev InputEvent) bool {
					switch ev.Kind {
					case InputKeyDown:
						*echo = ""
						if r, ok := typedRune(ev.Mods, ev.Key); ok && r >= '0' && r <= '9' {
							*echo = string(r)
							appendDigits(*echo)
							return true
						}
					case InputKeyUp:
						*echo = ""
					case InputText:
						digits := l.asciiDigits(ev.Text)
						if digits == "" {
							return false
						}
						for _, r := range digits {
							if r < '0' || r > '9' {
								return false
							}
						}
						if digits != *echo {
							appendDigits(digits)
						}
						*echo = ""
						return true
					}
					return false
				})
			}
			seg.label, seg.nameFrom, seg.nameJoin = l.Text(name), f, true
			seg.hasRange, seg.accRange, seg.accStep = true, [3]float64{float64(lo), float64(hi), float64(value)}, 1
			seg.accValue = label
			if seg.Focused() {
				seg.Background(t.Accent).TextColor(t.AccentText)
			}
			seg.Children(func() { Text(c, label).FontFeatures("tnum") })
			segs[k], fields[k] = seg, part.field
			order = append(order, k)
		}
	})
	h, m := tm.Hour(), tm.Minute()
	for pos, k := range order {
		seg := segs[k]
		step := 0
		switch {
		case seg.Shortcut(0, KeyUp):
			step = 1
		case seg.Shortcut(0, KeyDown):
			step = -1
		case k == 2 && seg.Shortcut(0, KeySpace):
			step = 1
		case seg.Shortcut(0, KeyRight) && pos+1 < len(order):
			segs[order[pos+1]].Focus()
			c.rt.focusVisible = true
		case seg.Shortcut(0, KeyLeft) && pos > 0:
			segs[order[pos-1]].Focus()
			c.rt.focusVisible = true
		}
		if step != 0 {
			switch k {
			case 0:
				h = (h + step + 24) % 24
			case 1:
				m = (m + step + 60) % 60
			case 2:
				h = (h + 12) % 24
			}
			set(h, m)
		}
		if s := seg.st; s.typing && s.typed != "" && k != 2 {
			typed := l.asciiDigits(s.typed)
			top := 24
			if k == 1 {
				top = 60
			} else if fields[k] == 'h' || fields[k] == 'K' {
				top = 12
				if fields[k] == 'h' {
					top = 13
				}
			} else if fields[k] == 'k' {
				top = 25
			}
			n, err := strconv.Atoi(typed)
			if err != nil || len(typed) > 2 || n >= top {
				runes := []rune(typed)
				typed = string(runes[len(runes)-1])
				n, err = strconv.Atoi(typed)
				s.typed = typed
			}
			if err == nil && n >= 0 && n < top {
				if k == 1 {
					m = n
				} else {
					switch fields[k] {
					case 'h':
						if n == 0 {
							continue
						}
						h = n%12 + (h/12)*12
					case 'K':
						h = n + (h/12)*12
					case 'k':
						if n == 0 {
							continue
						}
						h = n % 24
					default:
						h = n
					}
				}
				set(h, m)
				if (len(typed) == 2 || n*10 >= top) && k == 0 {
					s.typed = ""
					segs[1].Focus()
				}
			}
		}
	}
	f.styleFn = func(f *Element) {
		for _, seg := range segs {
			if seg != nil && seg.Focused() {
				f.borderC = t.Accent
			}
		}
	}
	return f
}

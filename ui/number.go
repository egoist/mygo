package ui

import (
	"math"
	"strconv"
	"strings"
)

// NumberInput creates a text input editing *value as a number between lo
// and hi, which the arrows Up and Down, and the buttons beside it, step
// by step. What is typed applies as soon as it is a number in range, and
// shows rounded to the decimals of step once the input loses the focus.
// Changed reports a new value.
//
//	ui.NumberInput(c, &app.copies, 1, 99, 1)
func NumberInput(c *Context, value *float64, lo, hi, step float64) *Element {
	t := c.theme
	l := c.Locale()
	row := Row(c).Gap(t.Space(1)).Shrink(0).AlignItems(Center)
	row.widget = "NumberInput"
	decimals := 0
	if s := strconv.FormatFloat(step, 'f', -1, 64); strings.Contains(s, ".") {
		decimals = len(s) - strings.IndexByte(s, '.') - 1
	}
	format := func(v float64) string { return l.FormatNumber(v, decimals) }
	// Grouping and localized digits can be wider than the old fixed field.
	// Reserve room for both bounds so stepping does not move the buttons.
	width := t.Space(20)
	for _, bound := range []float64{lo, hi, *value} {
		w, _ := c.MeasureText(0, Span{Text: format(bound)})
		width = max(width, w+t.Space(6.5)) // padding, border and caret
	}
	text := Local(row, "text", func() string { return format(*value) })
	// A locale switch preserves partial input and the rules under which
	// editing started until blur; the next edit uses the new locale.
	editingLocale := Local(row, "editing locale", func() *Locale { return l })
	inputID := Local(row, "input id", func() uint64 { return 0 })
	if *inputID == 0 || c.rt.focused != *inputID {
		*text = format(*value)
		*editingLocale = l
	}
	set := func(v float64) {
		v = math.Max(lo, math.Min(hi, v))
		if decimals >= 0 {
			p := math.Pow(10, float64(decimals))
			v = math.Round(v*p) / p
		}
		if v != *value {
			*value = v
			row.st.changed = true
			c.rt.consumed = true
		}
	}
	row.Children(func() {
		in := TextInput(c, text).Width(width)
		in.nameFrom = row
		*inputID = in.id
		in.widget = "NumberInput"
		if in.Changed() {
			if v, err := (*editingLocale).ParseNumber(*text); err == nil && v >= lo && v <= hi {
				set(v)
			}
		}
		if in.Shortcut(0, KeyUp) {
			set(*value + step)
			*text = (*editingLocale).FormatNumber(*value, decimals)
		}
		if in.Shortcut(0, KeyDown) {
			set(*value - step)
			*text = (*editingLocale).FormatNumber(*value, decimals)
		}
		if !in.Focused() {
			// What the app set, or what was typed, shown in full.
			*text = format(*value)
			*editingLocale = l
		}
		in.hasRange, in.accRange, in.accStep = true, [3]float64{lo, hi, *value}, step
		for _, b := range []struct {
			label string
			delta float64
		}{{"−", -step}, {"+", step}} {
			btn := Button(c, b.label).Padding(t.Space(1), t.Space(2)).Label(l.Text(map[bool]string{true: "Increase", false: "Decrease"}[b.delta > 0]))
			btn.Disabled(b.delta < 0 && *value <= lo || b.delta > 0 && *value >= hi)
			if btn.Clicked() {
				set(*value + b.delta)
				*text = format(*value)
			}
			btn.TextColor(t.Text)
		}
	})
	return row
}

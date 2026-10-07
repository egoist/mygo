package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

func TestLocaleNumbers(t *testing.T) {
	for _, tc := range []struct {
		tag, formatted string
		inputs         []string
	}{
		{"en-US", "1,234,567.50", []string{"1,234,567.50", "1234567.5", "1.2345675e6"}},
		{"de-DE", "1.234.567,50", []string{"1.234.567,50", "1234567,5", "1,2345675e6"}},
		{"fr-FR", "1\u202f234\u202f567,50", []string{"1 234 567,50", "1\u00a0234\u00a0567,50"}},
		{"hi-IN", "12,34,567.50", []string{"12,34,567.50", "1234567.5"}},
		{"ar-EG", "١٬٢٣٤٬٥٦٧٫٥٠", []string{"١٬٢٣٤٬٥٦٧٫٥٠", "1234567٫50"}},
		{"fa-IR", "۱٬۲۳۴٬۵۶۷٫۵۰", []string{"۱٬۲۳۴٬۵۶۷٫۵۰"}},
		{"en-US-u-nu-fullwide", "１,２３４,５６７.５０", []string{"１,２３４,５６７.５０"}},
		{"hi-IN-u-nu-deva", "१२,३४,५६७.५०", []string{"१२,३४,५६७.५०"}},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			l := NewLocale(tc.tag)
			if got := l.FormatNumber(1234567.5, 2); got != tc.formatted {
				t.Fatalf("formatted %q, want %q", got, tc.formatted)
			}
			for _, input := range tc.inputs {
				v, err := l.ParseNumber(input)
				if err != nil || v != 1234567.5 {
					t.Errorf("parse %q: %v, %v", input, v, err)
				}
			}
			for _, v := range []float64{-1234.5, -0.75, 0, 1, 999.25} {
				text := l.FormatNumber(v, 2)
				got, err := l.ParseNumber(text)
				if err != nil || got != v {
					t.Errorf("round trip %q: %v, %v", text, got, err)
				}
			}
		})
	}
}

func TestLocaleRejectsInvalidNumbers(t *testing.T) {
	for _, tc := range []struct {
		tag     string
		invalid []string
	}{
		{"en-US", []string{"", "-", ".", "NaN", "Inf", "1e999", "1,23", "1,,234", "12,3456", "1,234.5,6", "1 2", "1.2.3", "1e", "1e1e2"}},
		{"de-DE", []string{"1.23", "1,234,5", "1 234,50", "1.234,56.7", "1,2.3"}},
		{"hi-IN", []string{"1,234,567", "123,45,678", "1,234,56"}},
		{"ar-EG", []string{"١٬٢٣", "١٫٢٫٣", "١ ٢"}},
	} {
		for _, input := range tc.invalid {
			if v, err := NewLocale(tc.tag).ParseNumber(input); err == nil {
				t.Errorf("%s accepted %q as %v", tc.tag, input, v)
			}
		}
	}
}

func TestLocaleDatesAndWeekdays(t *testing.T) {
	date := time.Date(2024, 2, 29, 23, 15, 9, 123, time.FixedZone("test", 9*3600))
	for _, tc := range []struct {
		tag, short, month string
		first             time.Weekday
	}{
		{"en-US", "2/29/2024", "February 2024", time.Sunday},
		{"en-GB", "29/02/2024", "February 2024", time.Monday},
		{"de-DE", "29.2.2024", "Februar 2024", time.Monday},
		{"fr-FR", "29/02/2024", "février 2024", time.Monday},
		{"ja-JP", "2024/2/29", "2024年2月", time.Sunday},
		{"ar-EG", "٢٩\u200f/٢\u200f/٢٠٢٤", "فبراير ٢٠٢٤", time.Saturday},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			l := NewLocale(tc.tag)
			if s := l.FormatDate(date, ShortDate); s != tc.short {
				t.Errorf("date %q, want %q", s, tc.short)
			}
			if s := l.FormatDate(date, MonthYear); s != tc.month {
				t.Errorf("heading %q, want %q", s, tc.month)
			}
			if l.FirstDay() != tc.first {
				t.Errorf("first day: %v", l.FirstDay())
			}
			parsed, err := l.ParseDate(tc.short, date.Location())
			if err != nil || !sameDay(parsed, date) || parsed.Hour() != 0 || parsed.Location() != date.Location() {
				t.Errorf("parsed %v, %v", parsed, err)
			}
			invalid := l.FormatDate(time.Date(2023, 2, 28, 0, 0, 0, 0, time.UTC), ShortDate)
			// Replacing the day alone makes the invalid leap date for every pattern.
			invalid = strings.Replace(invalid, l.localDigits("28"), l.localDigits("29"), 1)
			if _, err := l.ParseDate(invalid, time.UTC); err == nil {
				t.Error("accepted a non-leap February 29")
			}
		})
	}
	if NewLocale("en-US-u-fw-mon").FirstDay() != time.Monday {
		t.Error("fw extension ignored")
	}
	if NewLocale("ru-RU").MonthName(time.March) == NewLocale("ru-RU").data.Months[2] {
		t.Error("standalone Russian month lost its grammatical form")
	}
}

func TestLocaleTimeFormats(t *testing.T) {
	ref := time.Date(2026, 12, 31, 13, 5, 27, 123, time.FixedZone("test", -8*3600))
	for _, tc := range []struct {
		tag, text string
		cycle     HourCycle
	}{
		{"en-US", "1:05\u202fPM", HourCycle12},
		{"en-GB", "13:05", HourCycle23},
		{"de-DE", "13:05", HourCycle23},
		{"ar-EG", "١:٠٥ م", HourCycle12},
		{"en-US-u-hc-h23", "13:05", HourCycle23},
		{"ja-JP-u-hc-h12", "午後1:05", HourCycle12},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			l := NewLocale(tc.tag)
			if l.HourCycle() != tc.cycle {
				t.Errorf("cycle %s", l.HourCycle())
			}
			if got := l.FormatTime(ref); got != tc.text {
				t.Errorf("time %q, want %q", got, tc.text)
			}
			for _, hour := range []int{0, 1, 11, 12, 13, 23} {
				value := time.Date(ref.Year(), ref.Month(), ref.Day(), hour, ref.Minute(), ref.Second(), ref.Nanosecond(), ref.Location())
				got, err := l.ParseTime(l.FormatTime(value), ref)
				if err != nil || !got.Equal(value) || got.Location() != ref.Location() {
					t.Errorf("round trip %v: %v, %v", value, got, err)
				}
			}
		})
	}
	for _, tc := range []struct{ tag, invalid string }{{"en-US", "0:05 PM"}, {"en-US", "13:05 PM"}, {"en-US", "1:65 PM"}, {"en-US", "1:05"}, {"en-GB", "24:00"}, {"en-GB", "12:90"}} {
		if _, err := NewLocale(tc.tag).ParseTime(tc.invalid, ref); err == nil {
			t.Errorf("accepted %s %q", tc.tag, tc.invalid)
		}
	}
	for _, cycle := range []HourCycle{HourCycle11, HourCycle24} {
		l := NewLocale("en-US").WithOptions(LocaleOptions{HourCycle: cycle})
		midnight := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		parsed, err := l.ParseTime(l.FormatTime(midnight), midnight)
		if err != nil || !parsed.Equal(midnight) {
			t.Errorf("%s midnight: %v, %v", cycle, parsed, err)
		}
	}
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	ref = time.Date(2026, 3, 8, 0, 0, 7, 8, loc)
	if _, err := NewLocale("en-GB").ParseTime("02:30", ref); err == nil {
		t.Error("accepted a DST-skipped time")
	}
}

func TestLocaleFallbackAndCustomization(t *testing.T) {
	for _, tag := range []string{"", "bogus invalid locale", "zz-ZZ", "tlh"} {
		l := NewLocale(tag)
		if l.ResolvedTag() != "en" || l.FormatNumber(1234.5, 1) != "1,234.5" || l.Text("Copy") != "Copy" {
			t.Errorf("fallback %q: %s, %q", tag, l.ResolvedTag(), l.Text("Copy"))
		}
	}
	if got := NewLocale("zh-TW").MonthName(time.February); got != NewLocale("zh-Hant").MonthName(time.February) {
		t.Errorf("traditional Chinese resolved %q", got)
	}
	if NewLocale("de-DE-u-nu-unknown").FormatNumber(1234.5, 1) != "1.234,5" {
		t.Error("unknown numbering system did not fall back")
	}
	for _, tag := range []string{"fr", "fr-FR", "fr-CA"} {
		if s := NewLocale(tag).Text("Copy"); s != "Copier" {
			t.Errorf("%s catalog fallback: %q", tag, s)
		}
	}
	strings := map[string]string{"Copy": "Duplicate", "hours": "hour part"}
	first := time.Wednesday
	l := NewLocale("de-DE").WithOptions(LocaleOptions{Strings: strings, FirstDay: &first, HourCycle: HourCycle12,
		Translate: func(s string) string {
			if s == "Next month" {
				return "Advance"
			}
			return ""
		},
		FormatNumber: func(v float64, n int) string { return fmt.Sprintf("%.1f units", v) },
		ParseNumber: func(s string) (float64, error) {
			if s == "two" {
				return 2, nil
			}
			return 0, fmt.Errorf("bad unit")
		},
		FormatDate: func(v time.Time, _ DateStyle) string { return v.Format("2006-01-02") },
		ParseDate: func(s string, loc *time.Location) (time.Time, error) {
			return time.ParseInLocation("2006-01-02", s, loc)
		},
	})
	strings["Copy"] = "changed"
	first = time.Friday
	if l.Text("Copy") != "Duplicate" || l.Text("Next month") != "Advance" || l.Text("Paste") == "Paste" || l.FirstDay() != time.Wednesday || l.HourCycle() != HourCycle12 {
		t.Error("customization/copy/fallback failed")
	}
	if l.FormatNumber(2, 0) != "2.0 units" {
		t.Error("number formatter hook ignored")
	}
	if v, err := l.ParseNumber("two"); err != nil || v != 2 {
		t.Error("number parser hook ignored")
	}
	if v, err := l.ParseDate("2024-02-29", time.UTC); err != nil || l.FormatDate(v, ShortDate) != "2024-02-29" {
		t.Error("date hooks ignored")
	}
}

// Check the integration against every generated locale, including numeric
// literals, quoted patterns, numbering systems and regional inheritance.
func TestLocaleDataRoundTrips(t *testing.T) {
	ref := time.Date(2024, 2, 29, 23, 5, 6, 7, time.UTC)
	for tag := range localeRecords {
		l := NewLocale(tag)
		date := l.FormatDate(ref, ShortDate)
		if got, err := l.ParseDate(date, time.UTC); err != nil || !sameDay(got, ref) {
			t.Errorf("%s date %q: %v %v", tag, date, got, err)
		}
		tm := l.FormatTime(ref)
		if got, err := l.ParseTime(tm, ref); err != nil || !got.Equal(ref) {
			t.Errorf("%s time %q: %v %v", tag, tm, got, err)
		}
		number := l.FormatNumber(-1234567.25, 2)
		if got, err := l.ParseNumber(number); err != nil || got != -1234567.25 {
			t.Errorf("%s number %q: %v %v", tag, number, got, err)
		}
	}
}

func TestLocaleNumberInputPreservesEdits(t *testing.T) {
	value, changes := 1234.5, 0
	tt := NewTester(func(c *Context) {
		Column(c).Children(func() {
			if NumberInput(c, &value, 0, 10000, 0.01).Label("Amount").Changed() {
				changes++
			}
			Button(c, "Leave")
		})
	}, 500, 150)
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	text := func() string {
		for _, n := range tt.h.access.Nodes {
			if n.Role == platform.RoleTextField {
				return n.Value
			}
		}
		t.Fatal("no number field")
		return ""
	}
	focus := func() {
		r, ok := tt.Find("Amount")
		if !ok {
			t.Fatal("no amount")
		}
		tt.ClickAt(r.X+20, r.Y+r.H/2)
	}
	tt.SetLocale("de-DE")
	if text() != "1.234,50" {
		t.Fatalf("number shown %q", text())
	}
	focus()
	tt.Command("selectAll")
	tt.Type("2.345,67")
	if value != 2345.67 || changes != 1 {
		t.Fatalf("typed number %v, changes %d", value, changes)
	}
	tt.Command("selectAll")
	tt.Type("-")
	tt.SetLocale("fr-FR")
	if text() != "-" || value != 2345.67 || changes != 1 {
		t.Fatal("locale switch changed an unfinished edit")
	}
	tt.Command("selectAll")
	tt.Type("3.456,78") // locale at focus time is retained
	if value != 3456.78 {
		t.Fatalf("active edit parsed in another locale: %v", value)
	}
	if err := tt.Click("Leave"); err != nil {
		t.Fatal(err)
	}
	if text() != "3\u202f456,78" {
		t.Fatalf("blur did not reformat: %q", text())
	}
	focus()
	tt.Command("selectAll")
	tt.Type("10 001,00")
	if value != 3456.78 {
		t.Error("out-of-range value applied")
	}
	tt.Command("selectAll")
	tt.Type("4 567,89")
	if value != 4567.89 {
		t.Errorf("new edit did not use new locale: %v", value)
	}
}

func TestLocaleNumberInputShowsLeadingDigits(t *testing.T) {
	for _, tag := range []string{"en-US", "fr-FR", "ar-EG", "hi-IN-u-nu-deva", "en-US-u-nu-fullwide"} {
		t.Run(tag, func(t *testing.T) {
			value := 1234567.5
			tt := NewTester(func(c *Context) { c.SetLocale(NewLocale(tag)); NumberInput(c, &value, -1234567.5, 1234567.5, 0.01) }, 500, 100)
			for _, state := range tt.rt.states {
				if ed := state.editor; ed != nil {
					if ed.scrollX != 0 {
						t.Errorf("leading digits clipped by %.2f DIPs", ed.scrollX)
					}
					return
				}
			}
			t.Fatal("no numeric editor")
		})
	}
}

func TestLocaleCalendarAndOverrides(t *testing.T) {
	date := time.Date(2024, 1, 31, 9, 41, 15, 123, time.FixedZone("local", 3600))
	changes := 0
	fr, de := NewLocale("fr-FR"), NewLocale("de-DE")
	tt := NewTester(func(c *Context) {
		c.WithLocale(fr, func() {
			Column(c).Children(func() {
				DateInput(c, &date).Label("Developer date")
				c.WithLocale(de, func() { Text(c, c.Locale().MonthName(time.March)) })
				Text(c, c.Locale().MonthName(time.March))
				if Calendar(c, &date).Changed() {
					changes++
				}
			})
		})
		Text(c, c.Locale().MonthName(time.March))
	}, 450, 600)
	if !tt.HasText("März") || !tt.HasText("mars") || !tt.HasText("March") {
		t.Fatalf("nested locales: %q", tt.Texts())
	}
	if err := tt.Click(fr.Text("Next month")); err != nil {
		t.Fatal(err)
	}
	if date.Day() != 29 || date.Month() != time.February || date.Hour() != 9 || date.Second() != 15 || date.Nanosecond() != 123 || date.Location().String() != "local" || changes != 1 {
		t.Errorf("month boundary: %v (%d changes)", date, changes)
	}
	tt.SetLocale("ja-JP")
	if !tt.HasText("février 2024") || !tt.HasText("3月") || changes != 1 {
		t.Error("host locale overwrote view override or emitted a change")
	}
	tt.send(platform.SurfaceEvent{Kind: platform.AccessibilityOn})
	tree := tt.h.access
	var headers []string
	for _, n := range tree.Nodes {
		if n.Role == platform.RoleColumnHeader {
			headers = append(headers, n.Label)
		}
	}
	if len(headers) < 7 || headers[0] != fr.WeekdayName(time.Monday, false) {
		t.Fatalf("headers %q", headers)
	}
	if n := node(t, tree, platform.RolePopUpButton, "Developer date"); n.Value != "29/02/2024" {
		t.Errorf("date value %q", n.Value)
	}
}

func TestLocaleTimeInput(t *testing.T) {
	value := time.Date(2026, 10, 7, 13, 5, 27, 123, time.UTC)
	changes := 0
	tt := NewTester(func(c *Context) {
		if TimeInput(c, &value).Label("Alarm").Changed() {
			changes++
		}
	}, 400, 100)
	tt.Click("PM")
	tt.Key(0, KeySpace)
	if value.Hour() != 1 || changes != 1 {
		t.Fatalf("day period: %v (%d)", value, changes)
	}
	tt.Click("1")
	tt.Type("11")
	if value.Hour() != 11 || !tt.Focused("05") {
		t.Fatalf("12-hour typing: %v %q", value, tt.Texts())
	}
	tt.SetLocale("ar-EG")
	tt.Click("١١")
	tt.Type("٢")
	if value.Hour() != 2 {
		t.Errorf("localized digit: %v", value)
	}
	tt.SetLocale("en-GB")
	if tt.HasText("AM/PM") || !tt.HasText("02") || value.Second() != 27 || value.Nanosecond() != 123 || value.Day() != 7 {
		t.Errorf("24-hour/time preservation: %v %q", value, tt.Texts())
	}
	before := changes
	tt.SetLocale("en-US")
	if changes != before {
		t.Error("locale switch emitted Changed")
	}
}

func TestLocaleTimeInputKeyTextEcho(t *testing.T) {
	value := time.Date(2026, 1, 1, 3, 45, 0, 0, time.UTC)
	tt := NewTester(func(c *Context) { TimeInput(c, &value) }, 400, 100)
	tt.Click("3")
	tt.TypeKey(0, Key1, "1")
	tt.TypeKey(0, Key1, "1") // focus advances to minutes before the text arrives
	if value.Hour() != 11 || value.Minute() != 45 {
		t.Fatalf("key/text echo edited twice: %v", value)
	}
	tt.TypeKey(0, Key2, "2")
	if value.Minute() != 2 {
		t.Fatalf("minutes typed twice: %v", value)
	}
}

func TestLocaleLateListRows(t *testing.T) {
	var state ListState
	value := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	fr := NewLocale("fr-FR")
	tt := NewTester(func(c *Context) {
		c.WithLocale(fr, func() { List(c, &state, 1000, func(int) { Text(c, c.Locale().FormatDate(value, ShortDate)) }).Fill() })
	}, 300, 180)
	tt.SetLocale("de-DE")
	state.ScrollTo(900, Start)
	tt.Frame()
	if !tt.HasText("07/10/2026") || tt.HasText("7.10.2026") {
		t.Fatalf("late row locale: %q", tt.Texts())
	}
}

func TestLocaleDatePopover(t *testing.T) {
	date := time.Date(2026, 10, 7, 9, 41, 0, 0, time.UTC)
	fr := NewLocale("fr-FR")
	tt := NewTester(func(c *Context) {
		c.WithLocale(fr, func() { DateInput(c, &date).Label("Due") })
	}, 400, 450)
	tt.SetLocale("de-DE")
	tt.Click("Due")
	if !tt.HasText("octobre 2026") || !tt.HasText(fr.Text("Next month")) {
		t.Fatalf("popover locale: %q", tt.Texts())
	}
	if err := tt.Click("15 octobre 2026"); err != nil {
		t.Fatal(err)
	}
	if date.Day() != 15 || date.Hour() != 9 || tt.HasText("octobre 2026") {
		t.Fatalf("popover choice %v", date)
	}
}

func TestLocaleTextMenuAndDeveloperLabels(t *testing.T) {
	text, query := "Copy", "query"
	fr := NewLocale("fr-FR")
	tt := NewTester(func(c *Context) {
		c.WithLocale(fr, func() {
			Column(c).Children(func() {
				TextInput(c, &text).Label("Copy").Placeholder("Developer placeholder")
				SearchField(c, &query).Label("Search mail")
				Button(c, "Paste")
			})
		})
	}, 500, 150)
	tt.Click("Copy")
	tt.Command("selectAll")
	tt.Key(Shift, KeyF10)
	if !slices.Contains(tt.Menu(), fr.Text("Copy")) || slices.Contains(tt.Menu(), "Copy") {
		t.Fatalf("localized menu %q", tt.Menu())
	}
	if err := tt.ChooseMenuItem(fr.Text("Copy")); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != "Copy" || text != "Copy" || !tt.HasText("Paste") || !tt.HasText("Search mail") {
		t.Fatal("developer labels/text changed")
	}
	if !tt.HasText(fr.Text("Clear")) {
		t.Error("search clear action is not localized")
	}
}

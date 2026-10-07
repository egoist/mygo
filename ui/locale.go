package ui

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/text/language"
)

//go:generate python3 ../scripts/locales.py

// DateStyle selects the representation of a Gregorian date.
type DateStyle uint8

const (
	// ShortDate uses the locale's numeric date with a full year.
	ShortDate DateStyle = iota
	// LongDate uses the locale's full month name, day and year.
	LongDate
	// MonthYear is the heading of a calendar.
	MonthYear
)

// HourCycle selects the hours a time input displays. The zero value uses
// the locale's CLDR default (or its Unicode hc extension).
type HourCycle string

const (
	HourCycle11 HourCycle = "h11" // 0–11, with a day period
	HourCycle12 HourCycle = "h12" // 1–12, with a day period
	HourCycle23 HourCycle = "h23" // 0–23
	HourCycle24 HourCycle = "h24" // 1–24
)

// LocaleOptions customizes a Locale. Empty fields keep the CLDR data and
// built-in catalog. WithOptions copies Strings and FirstDay; callbacks
// must be safe to use wherever the Locale is used. Widget callbacks run
// on the host's thread. Formatters and parsers should be supplied together.
type LocaleOptions struct {
	// Strings maps built-in English source strings to translations. Only
	// framework strings pass through it; app labels and values never do.
	Strings map[string]string
	// Translate is tried before Strings. An empty result falls back.
	Translate       func(source string) string
	FirstDay        *time.Weekday
	HourCycle       HourCycle
	NumberingSystem string
	FormatNumber    func(value float64, decimals int) string
	ParseNumber     func(text string) (float64, error)
	FormatDate      func(value time.Time, style DateStyle) string
	ParseDate       func(text string, location *time.Location) (time.Time, error)
	FormatTime      func(value time.Time) string
	ParseTime       func(text string, reference time.Time) (time.Time, error)
}

// Locale contains immutable formatting data and translations. It can be
// shared by views and goroutines. NewLocale accepts a BCP 47 tag, including
// nu (digits), hc (hour cycle) and fw (first weekday) Unicode extensions.
// A window uses its override, then the app's, then App.Locale's OS default.
// Headless views default to en-US for repeatable tests.
type Locale struct {
	tag      language.Tag
	resolved string
	data     *localeData
	digits   []rune
	symbols  numberSymbols
	firstDay time.Weekday
	cycle    HourCycle
	opts     LocaleOptions
	catalog  map[string]string
}

type numberSymbols struct {
	Decimal       string `json:"decimal"`
	Group         string `json:"group"`
	Plus          string `json:"plusSign"`
	Minus         string `json:"minusSign"`
	TimeSeparator string `json:"timeSeparator"`
}

type localeData struct {
	Date, LongDate, MonthYear, Time, Time12, Time24 string
	Months, StandaloneMonths                        [12]string
	Days, WideDays                                  [7]string
	Periods                                         [2]string
	Numbering, Grouping, Hours, Minutes             string
	Symbols                                         map[string]numberSymbols
}

var localeCache sync.Map

// NewLocale returns the locale for tag. Empty, invalid and unsupported tags
// fall back to English formatting. Supported tags inherit missing data
// through CLDR parents and inferred scripts; missing messages use English.
func NewLocale(tag string) *Locale {
	if tag == "" {
		tag = "en-US"
	}
	t, err := language.Parse(strings.ReplaceAll(tag, "_", "-"))
	if err != nil || t == language.Und {
		t = language.AmericanEnglish
	}
	key := t.String()
	if cached, ok := localeCache.Load(key); ok {
		return cached.(*Locale)
	}
	resolved := resolveLocale(t, localeRecords)
	data := new(localeData)
	if err := json.Unmarshal([]byte(localeRecords[resolved]), data); err != nil {
		panic(err)
	}
	l := &Locale{tag: t, resolved: resolved, data: data}
	l.configure()
	l.catalog = localeCatalog(t)
	actual, _ := localeCache.LoadOrStore(key, l)
	return actual.(*Locale)
}

func localeCandidates(t language.Tag) []string {
	base, _ := t.Base()
	script, _ := t.Script()
	region, _ := t.Region()
	plain := t.String()
	plain, _, _ = strings.Cut(plain, "-u-")
	plain, _, _ = strings.Cut(plain, "-x-")
	return []string{plain, base.String() + "-" + script.String() + "-" + region.String(), base.String() + "-" + region.String(), base.String() + "-" + script.String(), base.String()}
}

func resolveLocale(t language.Tag, records map[string]string) string {
	for _, candidate := range localeCandidates(t) {
		for candidate != "" && candidate != "root" {
			if _, ok := records[candidate]; ok {
				return candidate
			}
			if p := localeParents[candidate]; p != "" {
				candidate = p
				continue
			}
			break
		}
	}
	return "en"
}

// Merge catalogs per message, so a region-specific number label does not
// hide the base language's editing or calendar translations.
func localeCatalog(t language.Tag) map[string]string {
	candidates := localeCandidates(t)
	result := make(map[string]string)
	for i := len(candidates) - 1; i >= 0; i-- {
		if record := localeCatalogs[candidates[i]]; record != "" {
			var messages map[string]string
			if err := json.Unmarshal([]byte(record), &messages); err != nil {
				panic(err)
			}
			for k, v := range messages {
				result[k] = v
			}
		}
	}
	return result
}

func (l *Locale) configure() {
	nu := l.opts.NumberingSystem
	if nu == "" {
		nu = l.tag.TypeForKey("nu")
	}
	if localeDigits[nu] == "" {
		nu = l.data.Numbering
	}
	digits := localeDigits[nu]
	if digits == "" {
		digits = "0123456789"
	}
	l.digits = []rune(digits)
	l.symbols = l.data.Symbols[nu]
	if l.symbols.Decimal == "" {
		l.symbols = l.data.Symbols["latn"]
	}
	region, _ := l.tag.Region()
	day := localeFirstDays[region.String()]
	if day == "" {
		day = localeFirstDays["001"]
	}
	if fw := l.tag.TypeForKey("fw"); fw != "" {
		for _, candidate := range []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"} {
			if fw == candidate {
				day = fw
				break
			}
		}
	}
	l.firstDay = weekday(day)
	if l.opts.FirstDay != nil && *l.opts.FirstDay >= time.Sunday && *l.opts.FirstDay <= time.Saturday {
		l.firstDay = *l.opts.FirstDay
	}
	l.cycle = l.opts.HourCycle
	if l.cycle == "" {
		l.cycle = HourCycle(l.tag.TypeForKey("hc"))
	}
	switch l.cycle {
	case HourCycle11, HourCycle12, HourCycle23, HourCycle24:
	default:
		l.cycle = HourCycle23
		if strings.Contains(l.data.Time, "h") {
			l.cycle = HourCycle12
		}
	}
}

func weekday(s string) time.Weekday {
	for i, day := range []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"} {
		if day == s {
			return time.Weekday(i)
		}
	}
	return time.Monday
}

// Tag is the canonical requested locale, including extensions. It is
// suitable for preview configuration and future layout-direction policy.
func (l *Locale) Tag() string { return l.tag.String() }

// ResolvedTag is the CLDR locale supplying date and number data.
func (l *Locale) ResolvedTag() string { return l.resolved }

// WithOptions returns an independent Locale with these customizations.
func (l *Locale) WithOptions(opts LocaleOptions) *Locale {
	copy := *l
	copy.opts = opts
	copy.opts.Strings = make(map[string]string, len(opts.Strings))
	for k, v := range opts.Strings {
		copy.opts.Strings[k] = v
	}
	if opts.FirstDay != nil {
		day := *opts.FirstDay
		copy.opts.FirstDay = &day
	}
	copy.configure()
	return &copy
}

// FirstDay is the first weekday of the calendar, from CLDR's region data.
func (l *Locale) FirstDay() time.Weekday { return l.firstDay }

// HourCycle is the effective 12- or 24-hour cycle.
func (l *Locale) HourCycle() HourCycle { return l.cycle }

// Text translates a built-in source string, falling back to English. It
// accepts fmt-style arguments for messages such as "Remove %s". Developer
// labels, placeholders, validation errors and content are never translated.
func (l *Locale) Text(source string, args ...any) string {
	s := ""
	if l.opts.Translate != nil {
		s = l.opts.Translate(source)
	}
	if s == "" {
		s = l.opts.Strings[source]
	}
	base, _ := l.tag.Base()
	if s == "" && base.String() == "en" {
		s = source
	}
	if s == "" {
		s = l.catalog[source]
	}
	if s == "" {
		switch source {
		case "Remove %s":
			s = l.Text("Remove") + " %s"
		case "hours":
			s = l.data.Hours
		case "minutes":
			s = l.data.Minutes
		default:
			s = source
		}
	}
	if len(args) != 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// WeekdayName returns a full or short localized name; Sunday is zero.
func (l *Locale) WeekdayName(day time.Weekday, short bool) string {
	if day < time.Sunday || day > time.Saturday {
		return ""
	}
	if short {
		return l.data.Days[day]
	}
	return l.data.WideDays[day]
}

// MonthName returns a standalone month name, for a calendar heading.
func (l *Locale) MonthName(month time.Month) string {
	if month < time.January || month > time.December {
		return ""
	}
	return l.data.StandaloneMonths[month-1]
}

func (l *Locale) localDigits(s string) string {
	if string(l.digits) == "0123456789" {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			r = l.digits[r-'0']
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (l *Locale) asciiDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\u200e' || r == '\u200f' || r == '\u061c' {
			continue
		}
		for i, digit := range l.digits {
			if r == digit {
				r = rune('0' + i)
				break
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (l *Locale) grouping() (primary, secondary int) {
	p, _, _ := strings.Cut(l.data.Grouping, ".")
	parts := strings.Split(p, ",")
	if len(parts) < 2 {
		return 0, 0
	}
	count := func(s string) int {
		n := 0
		for _, r := range s {
			if r == '#' || r == '0' {
				n++
			}
		}
		return n
	}
	primary = count(parts[len(parts)-1])
	secondary = primary
	if len(parts) > 2 {
		secondary = count(parts[len(parts)-2])
	}
	return
}

// FormatNumber formats a decimal with grouping and localized digits.
// decimals is the fixed fraction width, or -1 for the shortest decimal.
func (l *Locale) FormatNumber(value float64, decimals int) string {
	if l.opts.FormatNumber != nil {
		return l.opts.FormatNumber(value, decimals)
	}
	s := strconv.FormatFloat(value, 'f', decimals, 64)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = l.symbols.Minus, s[1:]
	}
	whole, fraction, hasFraction := strings.Cut(s, ".")
	primary, secondary := l.grouping()
	if primary > 0 && len(whole) > primary && !math.IsInf(value, 0) && !math.IsNaN(value) {
		var parts []string
		width := primary
		for len(whole) > width {
			parts = append(parts, whole[len(whole)-width:])
			whole = whole[:len(whole)-width]
			width = secondary
		}
		for i := len(parts) - 1; i >= 0; i-- {
			whole += l.symbols.Group + parts[i]
		}
	}
	if hasFraction {
		whole += l.symbols.Decimal + fraction
	}
	return sign + l.localDigits(whole)
}

// ParseNumber accepts localized or ASCII digits, the locale's decimal and
// grouping separators, signs and ASCII scientific notation. Grouping is
// validated; malformed groups and nonfinite values are rejected. Whitespace
// grouping also accepts ordinary and nonbreaking spaces when pasted.
func (l *Locale) ParseNumber(text string) (float64, error) {
	if l.opts.ParseNumber != nil {
		return l.opts.ParseNumber(text)
	}
	s := l.asciiDigits(strings.TrimSpace(text))
	minus, plus := l.asciiDigits(l.symbols.Minus), l.asciiDigits(l.symbols.Plus)
	if strings.HasPrefix(s, minus) {
		s = "-" + strings.TrimPrefix(s, minus)
	}
	if strings.HasPrefix(s, plus) {
		s = "+" + strings.TrimPrefix(s, plus)
	}
	sign := ""
	if len(s) > 0 && (s[0] == '-' || s[0] == '+') {
		sign, s = s[:1], s[1:]
	}
	exponent := ""
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		exponent, s = s[i:], s[:i]
	}
	whole, fraction, hasFraction := strings.Cut(s, l.symbols.Decimal)
	group := l.symbols.Group
	if group != "" && unicode.IsSpace([]rune(group)[0]) {
		for _, space := range []string{" ", "\u00a0", "\u202f"} {
			whole = strings.ReplaceAll(whole, space, group)
		}
	}
	if group != "" && strings.Contains(whole, group) {
		parts := strings.Split(whole, group)
		primary, secondary := l.grouping()
		if primary == 0 || len(parts[len(parts)-1]) != primary || len(parts[0]) < 1 || len(parts[0]) > secondary {
			return 0, fmt.Errorf("ui: invalid number grouping %q", text)
		}
		for _, part := range parts[1 : len(parts)-1] {
			if len(part) != secondary {
				return 0, fmt.Errorf("ui: invalid number grouping %q", text)
			}
		}
		whole = strings.Join(parts, "")
	}
	digitsOnly := func(s string) bool {
		for _, r := range s {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}
	if !digitsOnly(whole) || hasFraction && !digitsOnly(fraction) {
		return 0, fmt.Errorf("ui: invalid number %q", text)
	}
	s = sign + whole
	if hasFraction {
		s += "." + fraction
	}
	v, err := strconv.ParseFloat(s+exponent, 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, fmt.Errorf("ui: invalid number %q", text)
	}
	return v, nil
}

type datePart struct {
	field   rune
	width   int
	literal string
}

// patternParts implements the LDML fields needed by the generated
// Gregorian date and time patterns, including quoted literals.
func patternParts(pattern string) []datePart {
	var parts []datePart
	runes := []rune(pattern)
	literal := func(s string) {
		if len(parts) > 0 && parts[len(parts)-1].field == 0 {
			parts[len(parts)-1].literal += s
		} else {
			parts = append(parts, datePart{literal: s})
		}
	}
	quoted := false
	for i := 0; i < len(runes); {
		r := runes[i]
		if r == '\'' {
			if i+1 < len(runes) && runes[i+1] == '\'' {
				literal("'")
				i += 2
				continue
			}
			quoted = !quoted
			i++
			continue
		}
		if !quoted && strings.ContainsRune("yMLdEHhKkams", r) {
			j := i + 1
			for j < len(runes) && runes[j] == r {
				j++
			}
			parts = append(parts, datePart{field: r, width: j - i})
			i = j
		} else {
			literal(string(r))
			i++
		}
	}
	return parts
}

func (l *Locale) formatPattern(value time.Time, pattern string, standalone bool) string {
	var out strings.Builder
	for _, part := range patternParts(pattern) {
		v := 0
		switch part.field {
		case 0:
			out.WriteString(part.literal)
			continue
		case 'y':
			v = value.Year()
		case 'M', 'L':
			if part.width >= 3 {
				months := l.data.Months
				if standalone || part.field == 'L' {
					months = l.data.StandaloneMonths
				}
				out.WriteString(months[value.Month()-1])
				continue
			}
			v = int(value.Month())
		case 'd':
			v = value.Day()
		case 'E':
			out.WriteString(l.data.WideDays[value.Weekday()])
			continue
		case 'H':
			v = value.Hour()
		case 'h':
			v = value.Hour() % 12
			if v == 0 {
				v = 12
			}
		case 'K':
			v = value.Hour() % 12
		case 'k':
			v = value.Hour()
			if v == 0 {
				v = 24
			}
		case 'm':
			v = value.Minute()
		case 's':
			v = value.Second()
		case 'a':
			out.WriteString(l.data.Periods[value.Hour()/12])
			continue
		}
		out.WriteString(l.localDigits(fmt.Sprintf("%0*d", part.width, v)))
	}
	return out.String()
}

// FormatDate formats a Gregorian date without changing its time zone.
func (l *Locale) FormatDate(value time.Time, style DateStyle) string {
	if l.opts.FormatDate != nil {
		return l.opts.FormatDate(value, style)
	}
	pattern := l.data.Date
	switch style {
	case LongDate:
		pattern = l.data.LongDate
	case MonthYear:
		pattern = l.data.MonthYear
	}
	return l.formatPattern(value, pattern, style == MonthYear)
}

// ParseDate parses the ShortDate representation into midnight in location
// (time.Local when nil), rejecting invalid dates instead of normalizing.
func (l *Locale) ParseDate(text string, location *time.Location) (time.Time, error) {
	if l.opts.ParseDate != nil {
		return l.opts.ParseDate(text, location)
	}
	if location == nil {
		location = time.Local
	}
	values, err := l.parsePattern(text, l.data.Date)
	if err != nil {
		return time.Time{}, err
	}
	y, m, d := values['y'], values['M'], values['d']
	if m == 0 {
		m = values['L']
	}
	date := time.Date(y, time.Month(m), d, 0, 0, 0, 0, location)
	if y < 1 || y > 9999 || date.Year() != y || int(date.Month()) != m || date.Day() != d {
		return time.Time{}, fmt.Errorf("ui: invalid date %q", text)
	}
	return date, nil
}

func (l *Locale) timePattern() string {
	pattern := l.data.Time24
	if l.cycle == HourCycle11 || l.cycle == HourCycle12 {
		pattern = l.data.Time12
		// Flexible day periods (B) need more than an AM/PM segment. Use
		// the locale's ordinary AM/PM names, retaining their position.
		pattern = strings.ReplaceAll(pattern, "B", "a")
		pattern = strings.ReplaceAll(pattern, "K", "h")
		if !strings.Contains(pattern, "a") {
			pattern += " a"
		}
	}
	if l.cycle == HourCycle11 {
		pattern = strings.ReplaceAll(pattern, "h", "K")
	}
	if l.cycle == HourCycle24 {
		pattern = strings.ReplaceAll(pattern, "H", "k")
	}
	return pattern
}

// FormatTime formats hours and minutes, with a localized day period when
// the locale uses a 12-hour clock. It keeps the value's time zone.
func (l *Locale) FormatTime(value time.Time) string {
	if l.opts.FormatTime != nil {
		return l.opts.FormatTime(value)
	}
	return l.formatPattern(value, l.timePattern(), false)
}

// ParseTime parses FormatTime's hours and minutes, keeping reference's
// date, seconds, nanoseconds and location. Invalid/DST-skipped times fail.
func (l *Locale) ParseTime(text string, reference time.Time) (time.Time, error) {
	if l.opts.ParseTime != nil {
		return l.opts.ParseTime(text, reference)
	}
	v, err := l.parsePattern(text, l.timePattern())
	if err != nil {
		return time.Time{}, err
	}
	h := v['H']
	switch l.cycle {
	case HourCycle12:
		if v['h'] < 1 || v['h'] > 12 {
			return time.Time{}, fmt.Errorf("ui: invalid hour")
		}
		h = v['h']%12 + v['a']*12
	case HourCycle11:
		if v['K'] > 11 {
			return time.Time{}, fmt.Errorf("ui: invalid hour")
		}
		h = v['K'] + v['a']*12
	case HourCycle24:
		if v['k'] < 1 || v['k'] > 24 {
			return time.Time{}, fmt.Errorf("ui: invalid hour")
		}
		h = v['k'] % 24
	}
	if h < 0 || h > 23 || v['m'] > 59 {
		return time.Time{}, fmt.Errorf("ui: invalid time %q", text)
	}
	y, mo, d := reference.Date()
	next := time.Date(y, mo, d, h, v['m'], reference.Second(), reference.Nanosecond(), reference.Location())
	if next.Hour() != h || next.Minute() != v['m'] || !sameDay(next, reference) {
		return time.Time{}, fmt.Errorf("ui: invalid local time %q", text)
	}
	return next, nil
}

func (l *Locale) parsePattern(text, pattern string) (map[rune]int, error) {
	s := l.asciiDigits(strings.TrimSpace(text))
	values := make(map[rune]int)
	invalid := func() (map[rune]int, error) { return nil, fmt.Errorf("ui: invalid date or time %q", text) }
	for _, part := range patternParts(pattern) {
		if part.field == 0 {
			literal := l.asciiDigits(part.literal)
			if strings.TrimSpace(literal) == "" {
				s = strings.TrimLeftFunc(s, unicode.IsSpace)
				continue
			}
			if !strings.HasPrefix(s, literal) {
				return invalid()
			}
			s = strings.TrimPrefix(s, literal)
			continue
		}
		if part.field == 'a' {
			matched := false
			// Some names are prefixes of others (Akan AN / ANW).
			order := []int{0, 1}
			if len(l.data.Periods[1]) > len(l.data.Periods[0]) {
				order = []int{1, 0}
			}
			for _, i := range order {
				period := l.data.Periods[i]
				period = l.asciiDigits(period)
				if len(s) >= len(period) && strings.EqualFold(s[:len(period)], period) {
					values['a'] = i
					s = s[len(period):]
					matched = true
					break
				}
			}
			if !matched {
				return invalid()
			}
			continue
		}
		if !strings.ContainsRune("yMLdHhKkms", part.field) {
			return invalid()
		}
		i := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == 0 {
			return invalid()
		}
		n, err := strconv.Atoi(s[:i])
		if err != nil {
			return invalid()
		}
		values[part.field] = n
		s = s[i:]
	}
	if strings.TrimSpace(s) != "" {
		return invalid()
	}
	return values, nil
}

// Locale returns the effective locale of the view being built.
func (c *Context) Locale() *Locale { return c.locale }

// SetLocale changes the locale for elements built after this call in the
// current view scope. nil restores the host's app/window locale. Like
// SetTheme, call it while building a frame; each frame starts at the host.
func (c *Context) SetLocale(locale *Locale) {
	if locale == nil {
		locale = c.rt.hostLocale()
	}
	c.locale = locale
}

// WithLocale builds a view with an override, then restores the surrounding
// locale. Overlays, menus and rows built later inherit their owner's locale.
func (c *Context) WithLocale(locale *Locale, view func()) {
	saved := c.locale
	c.SetLocale(locale)
	defer func() { c.locale = saved }()
	view()
}

// Hosts may provide a locale without extending the common host contract;
// preview hosts can opt into this or use Context.SetLocale.
func (rt *engine) hostLocale() *Locale {
	tag := "en-US"
	if h, ok := rt.host.(interface{ locale() string }); ok {
		if locale := h.locale(); locale != "" {
			tag = locale
		}
	}
	if rt.locale == nil || rt.localeTag != tag {
		rt.locale, rt.localeTag = NewLocale(tag), tag
	}
	return rt.locale
}

# Localization

Native UI follows the app's preferred OS locale, using Unicode CLDR data
for Gregorian dates, numbers, month and weekday names, the first weekday,
and 12/24-hour time. App labels, placeholders, validation messages, menu
items and content remain exactly as supplied by the developer.

Set a BCP 47 locale for the whole app, including windows already open:

```go
mygo.App.SetLocale("de-DE") // safe before Run and from any goroutine
mygo.App.SetLocale("")      // inherit the OS again
```

A window can override the app with `WindowOptions.Locale` or
`window.SetLocale("fr-CA")`. `window.Locale()` returns its effective tag;
an empty override inherits the app. These methods are goroutine-safe and
redraw native content without replacing its view state.

For a view, call `c.SetLocale(ui.NewLocale("ja-JP"))` before building its
elements, or scope an override:

```go
c.WithLocale(ui.NewLocale("fr-FR"), func() {
    ui.Column(c).Children(func() {
        ui.DateInput(c, &app.meeting).Label("Meeting date")
        ui.TimeInput(c, &app.meeting).Label("Meeting time")
        ui.NumberInput(c, &app.amount, 0, 10000, 0.01)
    })
})
```

`c.Locale()` returns the effective `*ui.Locale`. New elements capture it,
including their popovers, text context menus and virtualized rows built
later. Every frame starts at the window/app locale; view overrides are
applied again as the view builds. `SetLocale(nil)` restores the host.

`NumberInput` displays grouping and a localized decimal separator. It
accepts localized and ASCII digits, valid grouping, signs and scientific
notation. French grouping accepts a normal or nonbreaking space when
pasted. Invalid or out-of-range input keeps the last valid value, as
before. A locale change while editing retains the partial text and its
original parsing rules until blur, then reformats in the new locale.

`DateInput` retains its calendar interaction and time zone. Calendar month
navigation clamps to the last day of shorter months. `TimeInput` retains
its segments and adds a localized AM/PM segment for 12-hour clocks; arrows
or Space change the period. Values, focus and validation state survive
locale changes; a change of locale alone never emits `Changed`.

Unicode tag extensions provide common overrides:

```go
ui.NewLocale("ar-EG-u-nu-latn") // Latin digits in an Arabic locale
ui.NewLocale("en-US-u-hc-h23")  // 24-hour time
ui.NewLocale("en-US-u-fw-mon")  // Monday first
```

For text outside a widget, use `FormatNumber(value, decimals)`,
`FormatDate(value, ui.ShortDate/LongDate/MonthYear)` and `FormatTime(value)`.
`ParseNumber`, `ParseDate(text, location)` and `ParseTime(text, reference)`
use the same data. Dates parse to midnight in the given location; times
keep the reference date, seconds, nanoseconds and location. They reject
invalid calendar dates and skipped local times during DST transitions.
The supported date calendar is Gregorian; parsing supports years 1–9999.
Day periods use AM/PM rather than CLDR's flexible morning/evening periods.

## Customization and fallback

Locales are immutable and safe to share. `WithOptions` returns a copy with
custom message strings, translation callbacks, first weekday, hour cycle,
numbering system or formatter/parser functions. It copies supplied maps
and weekday pointers. Callbacks must be safe in the contexts using them.

```go
locale := ui.NewLocale("en-GB").WithOptions(ui.LocaleOptions{
    Strings: map[string]string{
        "Increase": "Add",
        "Decrease": "Subtract",
        "Next month": "Forward one month",
    },
    FormatNumber: func(value float64, decimals int) string {
        return strconv.FormatFloat(value, 'f', decimals, 64)
    },
    ParseNumber: func(text string) (float64, error) {
        return strconv.ParseFloat(strings.TrimSpace(text), 64)
    },
})
```

Number and date formatter/parser hooks are used by the matching widgets.
Time formatting/parsing hooks customize the helper methods; `TimeInput`
uses the locale's segment pattern, digits and `HourCycle` override.
`Locale.Text(source, args...)` translates framework strings using their
English source as the key, with `fmt` arguments for messages such as
`"%s of %s"`. `Translate` is tried first, then `Strings`, built-in
regional/language catalogs, and English. Missing messages fall back
individually, so a partial catalog keeps the available translations.
The inspector's developer UI remains in English.

Formatting data covers 766 CLDR locales; built-in catalogs are extracted
from Chromium and WinUI. Catalog coverage differs by message. Unavailable
labels (including some color names and auxiliary control strings) use
English and can be supplied through `Strings` or `Translate`. Unsupported
locales fall back to English data; invalid numbering/hour/weekday
extensions fall back to the locale defaults. `Tag()` retains the canonical
requested tag, and `ResolvedTag()` identifies the formatting data used.

The checked-in generated files need no runtime network access or external
locale library. [Data sources and regeneration](../../ui/locale-data/README.md)
describe the pinned versions, checksums and licenses.

## Testing and gallery

Headless views default to `en-US` for repeatable tests. Switch a running
tester with `tester.SetLocale("fr-FR")`; it preserves input state. A preview
host can use this method or apply `Context.SetLocale` to its sample view.
The canonical tag is available for future layout-direction integration;
locale changes do not mirror layout or navigation.

Run `go run ./examples/gallery` and open **Controls → Dates, times and
colors** to switch locale and compare the calendar, date/time segments and
grouped number input.

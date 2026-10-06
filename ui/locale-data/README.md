# Native UI locale data

`ui/locale_data_generated.go` is a compact subset of
[Unicode CLDR JSON 48.2.0](https://github.com/unicode-org/cldr-json):
`cldr-core`, `cldr-dates-full` and `cldr-numbers-full`. It includes all
766 Gregorian locales, numeric date and time patterns, standalone and
formatting month names, weekday and AM/PM names, number symbols/grouping,
numeric numbering systems, regional first weekdays and locale parents.
The archives' SHA-256 checksums are pinned in `scripts/locales.py`.

`ui/locale_strings_generated.go` contains maintained control translations
extracted from [Chromium](https://github.com/chromium/chromium/tree/0fb6e7c804f5c3191ec838c940e50e223d57b0b9)
(`ui_strings`, `ax_strings`, `blink_strings`, `components_strings` and
`generated_resources`) and
[WinUI](https://github.com/microsoft/microsoft-ui-xaml/tree/bfb0e2afa07627505c87f56d687b0483fe917d53)
(`NumberBox` and `ColorPicker` resource catalogs). The source commits and
explicit message mappings are pinned in the generator. Missing individual
messages and unsupported languages fall back to English; application
catalogs may fill or replace any message through `LocaleOptions`.

Regenerate with `go generate ./ui` (Python 3, network access for uncached
sources), then `gofmt -w ui/locale_*generated.go` and
`CGO_ENABLED=0 go test ./ui -run Locale`. Downloads are cached by immutable
URL under `~/.cache/mygo/locales`. The generator parses JSON/XML only; it
does not execute code from data providers. Update the pinned release,
archive hashes, source commits and this document together when refreshing
the data. Ordinary Go builds use the committed tables and require neither
Python nor Bun nor network access.

License notices are retained here: [Unicode](UNICODE-LICENSE),
[Chromium](CHROMIUM-LICENSE) and [WinUI](WINUI-LICENSE). Blink's date-picker
label also carries the Apple notice in [APPLE-LICENSE](APPLE-LICENSE).

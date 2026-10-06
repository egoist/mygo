# System text services

Native `TextInput`, `TextArea`, and their unstyled bases can use installed
spelling dictionaries and text substitutions. Services are opt-in; existing
inputs keep their behavior.

```go
input := ui.TextArea(c, &app.notes).Height(160).TextServices(ui.TextServicesOptions{
	Language:            "en-US", // empty uses mygo.App.Locale()
	SpellChecking:       true,
	AutomaticCorrection: true,
	SmartQuotes:         true,
	SmartDashes:         true,
	TextReplacement:     true,
})

status := input.TextServiceStatus()
if status.Error != nil {
	ui.Text(c, status.Error.Error())
}
if ui.Button(c, "Check spelling").Disabled(!status.Availability.Spelling).Clicked() {
	input.CheckSpelling()
}
```

`TextServiceStatus` reports the actual provider and dictionary language,
installed `Languages`, each feature's availability, the input's effective
options, checking progress, current issues, errors, and truncation. Its
slices are copies. A missing service returns `ui.ErrTextServicesUnavailable`;
a missing dictionary returns `ui.ErrTextLanguageUnavailable`, retaining the
installed language list. Both work with `errors.Is`.

Tags accept `en-US` or `en_US`. An exact installed locale wins; otherwise a
dictionary for the same language may be chosen. The returned `Language`
names that dictionary. MyGo never switches silently to another language.
All zero options disable automatic services. Unsupported options are skipped;
an explicit check requesting only unavailable services returns an error.

## Checking and editing

After 250 ms without an edit, the input checks an immutable snapshot of its
committed text. Automatic correction/substitution checks start immediately at
the typed boundary. `CheckSpelling` checks immediately, even with continuous
checking off. Misspellings have dotted underlines. The context menu adds
**Spelling Suggestions**, **Ignore Spelling**, **Learn Spelling**, **Check
Spelling**, and **Spelling and Substitutions**. Unavailable actions are disabled.
`ContextMenu` replaces the default menu, including these actions.

Menu toggles belong to one input and survive frames until the app supplies
different options. Ignore belongs to that input and actual dictionary
language. Learn persists in the user's native dictionary and can affect
other apps; MyGo only learns after the user chooses it or the app calls
`mygo.TextServices.LearnWord`.

Suggestions name `[Start, End)` ranges in **runes** of the checked snapshot.
MyGo rejects malformed ranges and replacements that split graphemes. It
discards results and menu choices after text, options, language, or composition
changes, including when text changes away and back to the same string.
Removing an input or closing a window cancels its check.

Correction and substitutions apply only next to the unchanged caret after
typing one punctuation or whitespace character. They do not rewrite pasted
text, app-set values, earlier words, undo/redo, or IME commits. A replacement
has a separate undo step: Undo restores the original word and selection while
keeping what the user typed. Redo restores the replacement. Moving the caret
or extending the selection while checking suppresses automatic application.

Composition text is never checked, and results cannot change the editor
while an input method composes. A composition change invalidates outstanding
results even if committed text did not change. Password, read-only, disabled,
and hidden inputs do not submit text to a service.

## Platform availability

| Service | macOS | Windows | Linux |
|---|---|---|---|
| Spelling and suggestions | NSSpellChecker, installed languages | Windows Spell Checking API, installed providers | Optional Enchant 2, installed provider dictionaries |
| Automatic correction | Native correction results | Only provider-designated replacement/deletion results | Unavailable |
| Smart quotes and dashes | Native checking, user's quote preferences | Unavailable | Unavailable |
| Text replacement | Native user's replacement dictionary | Unavailable | Unavailable |
| Learn spelling | Native user's dictionary | Provider's user dictionary | Enchant personal word list |

No dictionary is bundled and no runtime needs Bun. On Linux, install
`libenchant-2` and a dictionary/provider for the desired language (for example
Hunspell or Nuspell with English dictionaries). Missing libraries or symbols
leave services unavailable without affecting ordinary text editing.
Unsupported platforms also report unavailable. Grammar checking, prediction,
and the system spelling/substitution panels are outside this API.

## Custom editors and testing

Custom editors can use the same service through `mygo.TextServices`:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
result, err := mygo.TextServices.Check(ctx, snapshot, mygo.TextCheckOptions{
	Language: "en-US",
	Spelling: true,
})
// result.Text is precisely snapshot. Validate the editor's revision,
// options, selection and composition before applying result.Issues.
```

`Info`, `Check`, and `LearnWord` require a running app and are safe from any
goroutine. Call `Info` from `App.WhenReady` for a settings picker. `Check` is
cancellable; on the main thread it pumps native events while waiting. Prefer
a goroutine for custom editor checks, returning state changes through
`Window.Update`.

Preparation scans text off the UI thread. Native requests and completions
run on the main thread; macOS checking uses AppKit's background requests.
Linux and Windows check bounded chunks, yielding between calls. Chunks may
limit linguistic context for substitutions in long paragraphs. Checks accept
valid UTF-8 without NUL, at most 8 MiB, and return at most 1024 issues and eight
suggestions per issue. Tokens longer than 512 runes are omitted.
`Truncated` reports omitted tokens or additional issues, so a partial result
is never mistaken for a complete clean document.

`Tester` reports unavailable by default. `SetTextServices` accepts a
deterministic `ui.TextServiceProvider` so tests can supply suggestions or
retain a completion to exercise cancellation and stale results. Call its
completion in the Tester's owning goroutine. The gallery's **Text** page
includes language and feature controls and both kinds of input.

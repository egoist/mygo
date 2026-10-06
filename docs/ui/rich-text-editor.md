# Rich-text editor

`ui.RichTextEditor` edits a `ui.RichDocument` in native UI, with character
styles, paragraphs, links, selection, undo and input methods. A
`ui.RichEditorState` owns the document and history:

```go
// Create once, as part of your app's state.
doc := ui.NewRichDocument("A styled document\nStart writing here.")
doc = doc.WithStyle(ui.TextRange{Start: 2, End: 8}, ui.RichStyle{
	Weight: 700,
	Color: ui.RGB(40, 90, 180),
})
app.editor = ui.NewRichEditorState(doc)

// In the view:
input := ui.RichTextEditor(c, app.editor).Height(240).Label("Notes")
if input.Changed() {
	app.notes = app.editor.Document()
}
```

The zero value of `RichDocument` is empty; the zero value of
`RichEditorState` edits an empty document. Mount a state in one editor at a
time. Its methods are safe from any goroutine and invalidate its mounted
editor. After it unmounts, the state can still be edited and mounted again.

Run `go run ./examples/richtext` for a formatting toolbar, paragraph
alignment, editable links, mixed scripts and clipboard interchange.

## Documents and positions

Documents are immutable values. `WithStyle`, `TransformStyle`,
`WithParagraphStyle`, `Replace` and `Slice` return new documents; existing
values remain valid. `Runs()` and `Paragraphs()` return copies. `String()`
returns plain text and `Len()` counts Unicode runes.

`TextRange` is half-open: `{Start: 2, End: 5}` includes runes 2, 3 and 4.
All ranges are clamped and ordered. Nonempty ranges expand to complete
graphemes, so an edit never splits a combining accent, skin tone or ZWJ
emoji. A collapsed position inside a grapheme snaps to its start.
`TextSelection{Anchor, Caret}` preserves the direction of a selection;
`Range()` returns its ordered range.

```go
app.editor.Select(ui.TextSelection{Anchor: 0, Caret: 8})
fragment := app.editor.Document().Slice(app.editor.Selection().Range())
app.editor.ReplaceSelection(fragment) // one undoable styled replacement
app.editor.Insert("plain text")       // uses the current typing style
```

`RichStyle` supplies font family, size in DIPs, weight, italic, underline,
strikethrough, foreground color, background color and a link. Zero font,
size, weight and colors inherit the editor's style. Adjacent equal ranges
merge. Graphemes joined by inserting combining marks take the style of
their first rune. Newlines are normalized to LF; NUL and invalid UTF-8
become replacement characters.

`ParagraphStyle` supplies logical `Start`, `Center` or `End` alignment and
`LineHeight`, a font-size multiplier (zero uses natural font metrics).
Direction follows the paragraph's text through Core Text, Pango or
DirectWrite. Selecting up to the start of the next paragraph styles only
the preceding paragraphs. Splitting a paragraph uses the inserted
fragment's paragraph styles; joining retains the leading paragraph's.

## Formatting and links

```go
app.editor.Format(ui.FormatBold)
app.editor.Format(ui.FormatItalic)
app.editor.Format(ui.FormatUnderline)
app.editor.Format(ui.FormatStrikethrough)
app.editor.SetLink("https://example.com/guide")
app.editor.SetParagraphStyle(ui.ParagraphStyle{Alignment: ui.Center})
```

Formatting toggles the attribute throughout the selection. A mixed
selection turns it on; a fully formatted selection turns it off. At a
caret, formatting changes `TypingStyle()` for subsequent text. `SetStyle`
replaces the complete character style; `FormatClear` clears all character
attributes, including links, while keeping paragraph formatting.

The editor handles Cmd/Ctrl+B, I and U, the usual editing shortcuts and
Edit-menu commands. Left/Right move through grapheme carets in visual
order; Shift extends the selection. Mixed-direction selections highlight
each selected visual run separately. Word movement follows the same
platform conventions as `TextArea`.

Links support `http`, `https` and `mailto`. An empty or unsupported URL
removes the link. Links use the accent color unless given a color and have
an underline. Cmd/Ctrl-click opens one through the existing URL-opening
service; ordinary clicking and dragging select text.

`ReadOnly(true)`, `Placeholder`, `Composing`, element styling and context
menus work as on `TextArea`. `Changed()` reports document edits and
formatting performed by the widget. State methods already update the
document, so a toolbar can read `Document()` immediately after a command.

## Undo and input methods

Typing coalesces until a caret movement, formatting command or a pause of
one second. `Undo()` and `Redo()` restore the styled document, paragraph
formatting, directional selection and typing style. `CanUndo()` and
`CanRedo()` are suitable for toolbar buttons. A new edit clears redo.

```go
app.editor.BeginUndoGroup()
app.editor.Format(ui.FormatBold)
app.editor.SetLink("https://example.com/guide")
app.editor.EndUndoGroup() // one undo restores both changes
```

Groups may nest; balance every begin with an end. Undo/redo ends any open
group. `SetDocument` loads a new document and clears history. History keeps
up to 200 immutable document snapshots; this editor is intended for notes
and ordinary rich documents, rather than very large logs.

IME preedit is a transient styled preview. `Document()` stays unchanged
until commit. Cancelling restores the selected content and formatting;
committing replaces the selected or IME-specified range in one undo step.
GTK surrounding-text deletions during preedit are staged with the composition,
so cancellation and undo restore their styles too. Standalone deletions commit
immediately; an adjacent native text commit shares their undo step and style.
Keys while composing remain the input method's. An external document or
selection update cancels an outstanding preview.

## Clipboard and interchange

Copy/cut offers plain text, HTML and RTF together. Paste prefers HTML,
then RTF, then plain text. Plain text takes the typing style. The system
clipboard uses NSPasteboard on macOS, GTK ownership targets on Linux and
CF_UNICODETEXT/CF_HTML/registered RTF on Windows.

`doc.HTML()` and `doc.RTF()` also serialize fragments directly;
`ui.ParseRichHTML` and `ui.ParseRichRTF` import them. The app-level
`mygo.Clipboard.WriteRichText(text, html, rtf)` publishes all three
representations atomically in one clipboard write. `ReadRTF()` accompanies
the existing `ReadText()` and `ReadHTML()`.

The interchange subset covers basic character styles, fonts, colors,
paragraph alignment/proportional line spacing and links. Physical left/right
clipboard alignment is converted to logical start/end for RTL text. HTML imports inline CSS and
common formatting tags; scripts, external stylesheets, embedded objects
and images are ignored, and nothing is fetched or executed. RTF supports
Unicode/surrogate escapes, ANSI code page 1252, UTF-8, font/color tables
and hyperlink fields; unsupported code pages return an error so paste
can fall back to plain text. RTF rounds sizes to half-points, maps weights
to normal/bold and uses opaque colors. Interchange treats newlines as
paragraph separators, so their character style is not preserved. Images,
tables, lists and embedded objects are outside this document model.
Imports are limited to 8 MiB and 128 nested groups/elements.

`ui.Tester.SetRichClipboard` and `RichClipboard` exercise those alternative
representations without changing the system clipboard. Existing
`TextInput`, `TextArea` and display-only `RichText` keep their APIs.

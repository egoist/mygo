# Fuzzy matching

Package `ui/fuzzy` ranks the items of a list by what the user types, as a
command palette, a file finder or a code editor's completions do: an item
matches when it holds the query's letters in order, whatever their case,
so `gbi` finds `getElementById` and `maingo` finds `src/main.go`.

`fuzzy.NewPattern` reads the query once; its `Score` tells whether an
item matches and how well, a higher score being a better match, and
allocates nothing, to rank thousands of items on every key. `Positions`
gives the byte offsets of the letters matched, for the rows shown to
bold:

```go
p := fuzzy.NewPattern(app.query)
app.found = app.found[:0]
scores := map[string]int{}
for _, f := range app.files {
	if s, ok := p.Score(f); ok {
		app.found = append(app.found, f)
		scores[f] = s
	}
}
slices.SortStableFunc(app.found, func(a, b string) int { return scores[b] - scores[a] })
```

A row shows its letters matched in bold:

```go
func matchRow(c *ui.Context, p fuzzy.Pattern, item string) {
	var spans []ui.Span
	last := 0
	for _, at := range p.Positions(item) {
		_, n := utf8.DecodeRuneInString(item[at:])
		spans = append(spans, ui.Span{Text: item[last:at]}, ui.Span{Text: item[at : at+n], Weight: 700})
		last = at + n
	}
	ui.RichText(c, append(spans, ui.Span{Text: item[last:]})...)
}
```

A letter counts more at the start of the item, at the start of a word in
it (after a space or punctuation, as in `snake_case` and `a/path`, or a
capital after a small letter, as in `camelCase`) and right after the
letter matched before it, and more in a path's last part, after its last
slash, so a file's name beats its folders'. Gaps between the letters and
a longer item count against it, and letters typed in the item's own case
a little for it: `Set` puts `SetTimeout` before `setTimeout`. The query is
matched whole, spaces too: to match words in any order, score each word
and add the scores. Scores compare between the items one pattern
matched; their values mean nothing else.

The [code editor](code-editor.md#completions-hovers-and-diagnostics)
ranks its completions so. A [combobox](combobox.md) keeps its own order,
the options starting with the text first; `ui.ComboboxBase` takes the
options an app filters, which can be ranked so.

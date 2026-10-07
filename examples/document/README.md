# Document editor

Run `go run ./examples/document`. New/Open creates one window per typed
document. Edit text, then use Save, Save As or Revert. Closing a dirty
document offers Save, Discard and Cancel; canceled or failed saves keep it
open. The Recent menu also reopens the last closed file. Cmd/Ctrl+N, O, S,
Shift+S and P work from the editor.

Print opens the system dialog. Export PDF writes the same pages without a
printer. Text is wrapped at shaped clusters and split into pages by the
printable area's height, with a title and page number. Output is a lossless
raster PDF; its text is not searchable. Large jobs can exceed the documented
pixel limit; reduce DPI or print fewer pages.

The example writes documents through a temporary file and rename. It stores
recents and the named documents open at quit in `documents.json` under
`mygo.App.Path(mygo.PathUserData)`. On launch it restores that session from
disk. Closing a document explicitly removes it from the session; discarded
edits and unsaved untitled documents are not restored.

To package the file associations, run `go run ./cmd/mygo build examples/document`.
The `mygo.json` declares text/Markdown files. On macOS files can also be
dropped on the Dock icon; the single-instance lock forwards later launches
on Linux and Windows. No frontend or Bun is needed.

# Documents and native printing

`mygo.Documents[T]` is an opt-in lifecycle for document applications. The
application defines its model and file codec; MyGo coordinates file identity,
dirty state, save/revert, close prevention, recent files and reopening. It
works with both native UI and webview windows. Existing windows keep their
usual lifecycle unless a document is attached.

Run the complete [document editor](https://github.com/egoist/mygo/tree/main/examples/document)
with `go run ./examples/document`. It edits text in native UI, saves files
atomically, opens recent documents, restores a session, prints paginated
text and exports PDF. It has no frontend or Bun dependency.

## Define the application

Create a controller before `App.Run`, so open-file events from launch reach
it. `Read`, `Write`, `OpenWindow`, `ConfirmClose`, `OnError` and change
listeners run on the main thread. Keep callbacks short and never wait for a
goroutine that needs the main thread. Public methods can be called from any
goroutine, including UI callbacks; dialogs keep the native event loop moving.

```go
files := mygo.NewDocuments(mygo.DocumentsOptions[string]{
	Read: func(path string) (string, error) {
		b, err := os.ReadFile(path)
		return string(b), err
	},
	Write: writeTextAtomically, // func(path, value string) error, defined by the app
	HandleOpenFiles: true,
	OpenWindow: func(doc *mygo.Document[string]) *mygo.Window {
		return mygo.NewWindow(mygo.WindowOptions{
			Content: ui.View(func(c *ui.Context) {
				value := doc.Value()
				if ui.TextArea(c, &value).Fill().Changed() {
					doc.SetValue(value)
				}
			}),
		})
	},
})
mygo.App.WhenReady(func() {
	opened, err := files.Restore()
	if err != nil { log.Print(err) } // missing files do not prevent other opens
	if len(opened) == 0 { files.New() }
})
```

`Read` returns a new value. `Write` must preserve the original file on
failure; use a temporary file in the same directory followed by rename, as
the example does. MyGo commits lifecycle state only after the codec succeeds;
it cannot roll back a codec's partial filesystem writes.

`T` is copied shallowly. Strings and immutable structs are convenient models.
For slices, maps and pointer-containing models, replace them with new values
or mutate only inside `doc.Update(func(value *T) { … })`. A model passed to
`Write` must remain an immutable snapshot while that callback runs.

## Dirty state and file operations

`files.New()` creates a clean untitled document, using `DocumentsOptions.New`
or the zero value of `T`. `files.Open(path)` reads a named clean document. It
focuses an already open document instead of loading another copy. Paths are
absolute and resolve existing symlinks, including the parent of a new Save As
target. `files.OpenDialog(parent)` uses the configured open dialog and can
return successful documents alongside joined errors for a multiple selection.
Existing hard links and case aliases also reuse the same document identity.

`doc.State()` returns `Path`, `Title`, `Dirty`, `Busy` and `Closed`.
`SetValue` and `Update` mark an edit. `SetDirty(true)` records an external
change; `SetDirty(false)` marks the current revision as an app-defined saved
checkpoint, such as an undo that returns to saved content. `OnChange` observes
committed state and automatically attached windows redraw after changes.

| Operation | Successful result | Canceled or failed result |
| --- | --- | --- |
| `Save()` | Writes the current file; untitled documents get a Save dialog | Keeps identity and dirty state |
| `SaveAs()` | Always asks for a path, writes it and changes identity | Keeps the original identity and dirty state |
| `SaveTo(path)` | Writes a specified path without a dialog | Keeps identity and dirty state; the app chooses overwrite policy |
| `Revert()` | Reloads the named file and becomes clean | Keeps the value and dirty state |
| `Close()` | Asks existing close listeners, then saves or discards and closes | Keeps the window/document open |

Every edit has a revision. A save becomes clean only for the revision it
wrote: a newer edit made while a callback pumps events remains dirty. Edits
made during a Save dialog are included in the snapshot taken after the dialog
returns. Revert refuses to overwrite an edit made while `Read` pumps events.
Overlapping save/revert/close operations return `ErrDocumentBusy` immediately,
so a nested event never waits on an operation that needs the main thread.

Canceled file operations return `ErrDocumentCanceled`. Revert on an untitled
document returns `ErrDocumentUntitled`; another document's file identity is
protected by `ErrDocumentPathInUse`. Closed documents ignore edits and reject
file operations with `ErrDocumentClosed`.

## Windows, close and quit

`OpenWindow`'s result is attached automatically. Alternatively use
`doc.Attach(win)`. One document attaches to one window. macOS shows AppKit's
edited dot and represented filename/proxy icon; Linux and Windows append ` *`
to a dirty document's title. Document titles default to the file's basename
or `UntitledTitle` (default `"Untitled"`).

Existing `Window.OnClose` listeners run first. If they allow closing, a dirty
document offers Save, Discard and Cancel. A canceled Save dialog or failed
write keeps it open and cancels quitting. `ConfirmClose` can replace that
dialog and return `DocumentSave`, `DocumentDiscard` or `DocumentCancel`.
`DocumentSave` runs the coordinated save itself. Returning Cancel is the safe
zero value. `Window.Destroy` and `App.Exit` deliberately bypass protection.
Documents without windows also participate in the app's quit sequence.

A normal parent-window close also checks its descendant documents. If a
later dialog allows a newer edit in an already prepared document, the family
stays open. Attached document windows keep their document titles when a
web page updates its HTML title; page-title listeners still receive the event.

`OnError` handles window-close failures, open-file event errors and history
persistence errors; it defaults to logging. Explicit operations return their
own errors. A history write failure does not turn a successfully saved file
into a failed save. `files.Dispose()` removes app hooks once all documents
have closed.

`HandleOpenFiles` routes existing `App.OnOpenFile` events to `Open`, including
Dock drops on macOS and packaged file associations. Other open-file listeners
still receive those events. Use `App.RequestSingleInstanceLock` on Linux and
Windows; [file associations](app.md) require packaging declarations.

## Recents and reopening

`Recent()` returns a copy of successful opened/saved paths, most recent first.
The default limit is ten; a negative `RecentLimit` disables recents.
`ForgetRecent(path)` removes an entry, or clears the list for `""`.
Missing files remain listed until removed, and a failed open does not reorder
the list. The app can build a menu from these paths and call `Open` on a choice.

`ReopenClosed()` reopens the last closed named file from disk. A failed read
keeps that entry available for retry. Discarded edits are not resurrected.
`Restore()` reopens the named documents open when the previous quit began,
including paths assigned by Save As during the quit. Call it before creating
the initial untitled document. A canceled quit remembers the documents that
remain open; a normal close removes that document from the session.
Failed restore entries remain available for another `Restore` attempt.

History lives in `documents.json` in `PathUserData`, or `StatePath`.
Controllers need separate state paths. `MemoryOnly` disables persistence.
Writes use a temporary file and rename. No unsaved draft content is persisted:
session restoration reloads saved files, and cannot recover edits after a
crash. This is an application-owned list, independent of OS-global recents.

## Define printable pages

Native printing uses a separate static view of the document. It does not
capture the editor's viewport or scroll offset. A single page can use
`ui.PrintView(func(c *ui.Context) { … })`. For pagination,
`ui.PrintPages` receives the available content width and height, in points
(72 points per inch), and returns one view per page:

```go
snapshot := doc.Value()
pages := ui.PrintPages(func(size ui.PrintLayout) []func(*ui.Context) {
	// Split the snapshot at logical page breaks chosen by the app.
	parts := paginate(snapshot, size.Width, size.Height)
	var views []func(*ui.Context)
	for i, part := range parts {
		views = append(views, func(c *ui.Context) {
			ui.Column(c).Fill().Gap(12).Children(func() {
				ui.Text(c, part).FontSize(12)
				ui.Textf(c, "Page %d", i+1).FontSize(10)
			})
		})
	}
	return views
})
opts := mygo.PrintOptions{PageSize: mygo.PageA4, DPI: 144}
err := win.Print(pages, opts)
pdf, err := win.PrintToPDF(pages, opts)
```

Page views use one UI layout unit per point, an isolated state per page,
light appearance and disabled motion. They can draw native text, images,
paths, gradients and effects through the existing CPU renderer. Backgrounds
are included. Overflow clips to the content area, leaving white margins.
Capture a model snapshot before pagination so every page has the same
revision. The example wraps text at shaped cluster boundaries and groups
lines by the available height.

`PrintOptions` defaults to Letter, portrait, 0.4-inch margins and 144 DPI.
Lengths in options are inches, matching `PDFOptions`; `Margins: &Margins{}`
removes margins. DPI must be 72–600. Empty page lists, invalid sizes/margins,
more than 1,000 pages or jobs above 64 million pixels (256 MiB of page RGBA)
return errors before printing. Reduce DPI or split large jobs.

`Window.Print` opens a system dialog and returns after submission, or
`ErrPrintCanceled` when dismissed. Changing printer paper fits each fixed
page to the printable area, preserving page breaks. Physical printer
completion remains with the spooler. macOS uses AppKit/PDFKit; Linux uses
`GtkPrintOperation` and cairo; Windows uses `PrintDlgExW` and GDI. They start
no webview, and require the platform's normal printer setup.

`Window.PrintToPDF` requires no printer or dialog. The same fixed pages are
embedded losslessly into a portable PDF on every supported desktop platform.
The output is raster, with sRGB colors; text is not searchable/selectable and
PDF semantic accessibility is not provided. This keeps native drawings and
effects consistent across printers and exports. Webview printing remains on
`Page.Print` and `Page.PrintToPDF`, with the page's print style sheets.

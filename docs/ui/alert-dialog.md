# Alert dialog

<!-- preview alert-dialog 240 -->

`ui.AlertDialog` asks about something important over the window while a
`*bool` is true, as AppKit's alerts: a title, a message, and buttons, the
last of which is the default, in the accent color, with the focus, which
Enter clicks. Escape closes the alert choosing no button, as canceling does,
whatever the buttons say, in any language; a click outside the alert does
nothing. It returns the index of the button clicked, in the frame it is,
which closes the alert, `ui.AlertDismissed` in the frame Escape closes it,
and -1 otherwise.

```go
if ui.Button(c, "Move to Trash").Clicked() {
	app.asking = true
}
switch ui.AlertDialog(c, &app.asking, "Move “Notes” to the Trash?", "You can put it back from the Trash.", "Cancel", "Move to Trash") {
case 1:
	app.trash()
}
```

An alert that must be answered stays open by setting the `*bool` back to
true as Escape dismisses it:

```go
if ui.AlertDialog(c, &app.updating, "Update required", "Restart to finish updating.", "Restart") == ui.AlertDismissed {
	app.updating = true
}
```

The alert grows past its usual width as far as the window allows to keep its
buttons on one line, with its title and message wrapping to that width. In a
window too narrow for that the buttons wrap onto more lines, and a label too
long for the alert ends in an ellipsis.

For a dialog of your own content, use a [dialog](dialog.md); for a choice
the user can undo, act at once and offer it in a [toast](toast.md).

## Destructive actions

`ui.AlertDialogButtons` takes `ui.AlertButton`s, of which those that are
`Destructive`, for an action that deletes data or can't be undone, show
their label in the theme's `Danger` color and are never the default, as
AppKit's alerts with `hasDestructiveAction`: the default is the last button
that is not destructive, as Cancel, so that Enter keeps the data. An alert
whose buttons are all destructive has no default: it holds the focus
itself, and Enter clicks none. A destructive button takes Enter, as any
button does, only once the user moves the focus to it with Tab. The color
alone tells no one using assistive technology: let the label say what the
button does, as "Delete" rather than "OK".

```go
switch ui.AlertDialogButtons(c, &app.asking, "Delete “Notes”?", "You can't undo this.",
	ui.AlertButton{Label: "Cancel"}, ui.AlertButton{Label: "Delete", Destructive: true}) {
case 1:
	app.delete()
}
```

## Accessibility

Assistive technology sees an alert dialog named by its title and described
by its message, with the focus on its default button, or on the alert when
it has none.

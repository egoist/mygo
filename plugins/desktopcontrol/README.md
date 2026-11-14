# desktopcontrol

The local, logged-in desktop, for [MyGo](https://github.com/egoist/mygo)
apps: its displays and windows, JPEG screenshots of them, synthesized
input aimed at one display, process or window, and the static labels and
buttons of dialogs. macOS 14 and later; it is all Go, through purego, and
returns `ErrUnsupported` elsewhere. It opens no sockets and grants no
remote access: the app owns consent, sessions and whatever it sends
elsewhere.

```go
import "github.com/egoist/mygo/plugins/desktopcontrol"

wins, err := desktopcontrol.Windows()
if err == nil && len(wins) > 0 {
	jpeg, err := desktopcontrol.CaptureWindow(ctx, wins[0], desktopcontrol.CaptureOptions{MaxDimension: 1280, Quality: 0.7})
	// ...
}
```

It needs Screen Recording for windows and screenshots, and Accessibility
for input and dialogs, and only reports their absence
(`ErrScreenRecording`, `ErrAccessibility`): ask for them with
[permissionflow](../permissionflow).

See [the documentation](../../docs/plugins/desktopcontrol.md), and
`examples/desktopcontrol`. `go test ./plugins/desktopcontrol` reads
metadata and dialogs only; it never sends input.

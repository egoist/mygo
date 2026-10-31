package mobile

import (
	"errors"
	"strings"

	"github.com/egoist/mygo/internal/mobile/wire"
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/unsupported"
	"github.com/egoist/mygo/transfer"
)

type appController struct {
	unsupported.App
	b *Backend
}

func (a appController) IsActive() bool { return a.b.front() != nil }

func (a appController) PreferredLanguages() []string {
	if l := a.b.appInfo().Languages; len(l) > 0 {
		return l
	}
	return []string{"en-US"}
}

func (a appController) Region() platform.Region {
	i := a.b.appInfo()
	return platform.Region{Locale: i.RegionLocale, FirstWeekday: i.FirstWeekday, HourCycle: i.HourCycle}
}

func (a appController) Package() (platform.PackageInfo, bool) {
	i := a.b.appInfo()
	if i.Identifier == "" {
		return platform.PackageInfo{}, false
	}
	return platform.PackageInfo{Name: i.Name, Version: i.Version, Identifier: i.Identifier}, true
}

// URL schemes are the bundle's (Info.plist) and the manifest's: the
// system knows them as the app is installed.
func (a appController) RegisterURLScheme(string, string, string) error    { return nil }
func (a appController) IsURLSchemeRegistered(string, string, string) bool { return true }

type dialogs struct {
	unsupported.Dialogs
	b *Backend
}

func (d dialogs) ShowMessageBox(_ platform.Window, o *platform.MessageBoxOptions, cb func(platform.MessageBoxResult, error)) {
	buttons := o.Buttons
	if len(buttons) == 0 {
		buttons = []string{"OK"}
	}
	req := d.b.request(func(r wire.Reader) {
		var e wire.MessageBoxResultEvent
		e.Decode(&r)
		cb(platform.MessageBoxResult{Response: e.Response, CheckboxChecked: o.CheckboxChecked}, nil)
	})
	d.b.call(&wire.MessageBox{Req: req, Type: o.Type, Title: o.Title, Message: o.Message, Detail: o.Detail,
		Buttons: buttons, DefaultID: o.DefaultID, CancelID: o.CancelID}, nil)
}

// ShowOpenDialog shows the system's picker of documents, which gives the
// app copies of the files chosen, in its temporary directory: phones give
// apps no paths of other apps' files.
func (d dialogs) ShowOpenDialog(_ platform.Window, o *platform.OpenDialogOptions, cb func([]string, error)) {
	var exts []string
	for _, f := range o.Filters {
		for _, e := range f.Extensions {
			if e != "*" {
				exts = append(exts, strings.TrimPrefix(e, "."))
			}
		}
	}
	req := d.b.request(func(r wire.Reader) {
		var e wire.FilesResultEvent
		e.Decode(&r)
		cb(e.Paths, errorOf(e.Error))
	})
	d.b.call(&wire.OpenFiles{Req: req, Multiple: o.Multiple, Directories: o.OpenDirectories && !o.OpenFiles, Extensions: exts}, nil)
}

type clipboard struct {
	unsupported.Clipboard
	b *Backend
}

func (c clipboard) ReadText() string {
	var r wire.ClipboardTextReply
	c.b.call(&wire.ClipboardText{}, &r)
	return r.Text
}

func (c clipboard) WriteText(s string) { c.b.call(&wire.SetClipboardText{Text: s}, nil) }

func (c clipboard) Clear() { c.WriteText("") }

func (c clipboard) AvailableFormats() []string {
	if _, ok := c.text(); ok {
		return []string{"text/plain"}
	}
	return nil
}

func (c clipboard) text() (string, bool) {
	var r wire.ClipboardTextReply
	c.b.call(&wire.ClipboardText{}, &r)
	return r.Text, r.OK
}

func (c clipboard) Formats() []transfer.Format {
	if _, ok := c.text(); ok {
		return []transfer.Format{transfer.Text}
	}
	return nil
}

func (c clipboard) ReadData(formats []transfer.Format) (transfer.Data, error) {
	var d transfer.Data
	for _, f := range formats {
		if f == transfer.Text {
			if s, ok := c.text(); ok {
				d = transfer.TextData(s)
			}
		}
	}
	return d, nil
}

func (c clipboard) WriteData(d transfer.Data, _ func()) error {
	b, err := d.Read(transfer.Text)
	if err != nil {
		return errUnsupported
	}
	c.WriteText(string(b))
	return nil
}

func (c clipboard) Flush() error { return nil }

type shell struct {
	unsupported.Shell
	b *Backend
}

func (s shell) OpenExternal(url string) error {
	var r wire.OpenURLReply
	s.b.call(&wire.OpenURL{URL: url}, &r)
	if !r.OK {
		return errors.New("mygo: no app opens " + url)
	}
	return nil
}

// OpenPath shares the file, as phones open another app's documents.
func (s shell) OpenPath(path string) error {
	s.b.call(&wire.ShareFile{Path: path}, nil)
	return nil
}

func (s shell) ShowItemInFolder(path string) { s.b.call(&wire.ShareFile{Path: path}, nil) }

func (s shell) Beep() { s.b.call(&wire.Haptic{Kind: "warning"}, nil) }

type screen struct {
	unsupported.Screen
	b *Backend
}

func (s screen) Displays() []platform.Display {
	var r wire.ScreenReply
	s.b.call(&wire.Screen{}, &r)
	rect := platform.Rect{Width: int(r.Width + 0.5), Height: int(r.Height + 0.5)}
	work := rect
	if w := s.b.front(); w != nil {
		work = platform.Rect{X: int(w.insets.Left), Y: int(w.insets.Top),
			Width: int(r.Width - w.insets.Left - w.insets.Right), Height: int(r.Height - w.insets.Top - w.insets.Bottom)}
	}
	return []platform.Display{{ID: 1, Label: s.b.appInfo().Model, Bounds: rect, WorkArea: work, ScaleFactor: r.Scale,
		Internal: true, Primary: true}}
}

type theme struct {
	unsupported.Theme
	b *Backend
}

func (t theme) IsDark() bool { return t.b.dark }

func (t theme) SetSource(source string) { t.b.call(&wire.SetAppearance{Source: source}, nil) }

func (t theme) Preferences() platform.Preferences {
	var r wire.ThemeReply
	t.b.call(&wire.Theme{}, &r)
	return platform.Preferences{Accent: colorOf(r.Accent), ReduceMotion: r.ReduceMotion,
		ReduceTransparency: r.ReduceTransparency, HighContrast: r.IncreaseContrast, TextScale: r.TextScale}
}

type power struct {
	unsupported.Power
	b *Backend
}

// KeepAwake keeps the screen on, as phones keep apps running while their
// screens show them.
func (p power) KeepAwake(_ bool, _ string) func() {
	p.b.call(&wire.KeepAwake{On: true}, nil)
	return func() { p.b.call(&wire.KeepAwake{On: false}, nil) }
}

type secrets struct {
	unsupported.Secrets
	b *Backend
}

func (s secrets) SetSecret(service, account string, value []byte) error {
	var r wire.SecretSetReply
	s.b.call(&wire.SecretSet{Service: service, Account: account, Value: value}, &r)
	return errorOf(r.Error)
}

func (s secrets) Secret(service, account string) ([]byte, error) {
	var r wire.SecretGetReply
	s.b.call(&wire.SecretGet{Service: service, Account: account}, &r)
	if err := errorOf(r.Error); err != nil {
		return nil, err
	}
	if !r.Found {
		return nil, platform.ErrSecretNotFound
	}
	return r.Value, nil
}

func (s secrets) DeleteSecret(service, account string) error {
	var r wire.SecretDeleteReply
	s.b.call(&wire.SecretDelete{Service: service, Account: account}, &r)
	return errorOf(r.Error)
}

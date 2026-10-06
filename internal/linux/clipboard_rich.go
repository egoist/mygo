//go:build linux && (amd64 || arm64)

package linux

import (
	"unsafe"

	"github.com/ebitengine/purego"
)

func clipboardString(data ptr, n int32) string {
	return string(unsafe.Slice(*(**byte)(unsafe.Pointer(&data)), n))
}

// GTK keeps Go-owned immutable representations by an integer token. The two
// callbacks are allocated once at backend startup, never per clipboard write.
type clipboardTarget struct {
	target *byte
	flags  uint32
	info   uint32
}

type richClipboardData struct{ text, html, rtf string }

var (
	cbRichClipboardGet, cbRichClipboardClear ptr
	nextRichClipboard                        ptr
	richClipboardWrites                      = map[ptr]richClipboardData{}
)

func initRichClipboardCallbacks() {
	cbRichClipboardGet = purego.NewCallback(func(cb, sd ptr, info uint32, data ptr) {
		d, ok := richClipboardWrites[data]
		if !ok {
			return
		}
		s, target := d.text, "UTF8_STRING"
		switch info {
		case 1:
			target = "text/plain;charset=utf-8"
		case 2:
			target = "text/plain"
		case 3:
			s, target = d.html, "text/html"
		case 4:
			s, target = d.rtf, "text/rtf"
		case 5:
			s, target = d.rtf, "application/rtf"
		}
		gtkSelectionDataSet(sd, gdkAtomIntern(cs(target), false), 8, cs(s), int32(len(s)))
	})
	cbRichClipboardClear = purego.NewCallback(func(cb, data ptr) { delete(richClipboardWrites, data) })
}

func (clipboard) ReadRTF() string {
	c := clipboard{}
	if s := c.readRichFormat("text/rtf"); s != "" {
		return s
	}
	return c.readRichFormat("application/rtf")
}

func (clipboard) readRichFormat(format string) string {
	sd := gtkClipboardWaitForContents(clip(), gdkAtomIntern(cs(format), false))
	if sd == 0 {
		return ""
	}
	defer gtkSelectionDataFree(sd)
	n, data := gtkSelectionDataGetLength(sd), gtkSelectionDataGetData(sd)
	if n <= 0 || data == 0 {
		return ""
	}
	return clipboardString(data, n)
}

func (clipboard) WriteRichText(text, markup, rtf string) {
	names := []string{"UTF8_STRING", "text/plain;charset=utf-8", "text/plain"}
	targets := make([]clipboardTarget, 0, 6)
	for i, name := range names {
		targets = append(targets, clipboardTarget{target: cs(name), info: uint32(i)})
	}
	if markup != "" {
		targets = append(targets, clipboardTarget{target: cs("text/html"), info: 3})
	}
	if rtf != "" {
		targets = append(targets, clipboardTarget{target: cs("text/rtf"), info: 4}, clipboardTarget{target: cs("application/rtf"), info: 5})
	}
	nextRichClipboard++
	id := nextRichClipboard
	richClipboardWrites[id] = richClipboardData{text, markup, rtf}
	if !gtkClipboardSetWithData(clip(), &targets[0], uint32(len(targets)), cbRichClipboardGet, cbRichClipboardClear, id) {
		delete(richClipboardWrites, id)
	}
}

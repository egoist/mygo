//go:build windows && (amd64 || arm64)

package control

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/egoist/mygo"
)

var user = syscall.NewLazyDLL("user32.dll")
var create = user.NewProc("CreateWindowExW")
var setText = user.NewProc("SetWindowTextW")
var getText = user.NewProc("GetWindowTextW")
var getTextLen = user.NewProc("GetWindowTextLengthW")
var send = user.NewProc("SendMessageW")
var font = syscall.NewLazyDLL("gdi32.dll").NewProc("GetStockObject")

func utf16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

func Options(initial string, changed func(string)) mygo.NativeViewOptions {
	return mygo.NativeViewOptions{
		Create: func(c mygo.NativeViewContext) (uintptr, error) {
			// A standard EDIT sends EN_CHANGE to MyGo's supplied Parent.
			field, _, err := create.Call(0, uintptr(unsafe.Pointer(utf16("EDIT"))), uintptr(unsafe.Pointer(utf16(initial))),
				0x40000000|0x10000000|0x00010000|0x00800000|0x0080, 0, 0, 1, 1, c.Parent, 0, 0, 0)
			if field == 0 {
				return 0, fmt.Errorf("CreateWindowExW: %w", err)
			}
			f, _, _ := font.Call(17)       // DEFAULT_GUI_FONT, borrowed
			send.Call(field, 0x0030, f, 1) // WM_SETFONT
			return field, nil
		},
		Message: func(c mygo.NativeViewContext, m mygo.NativeViewMessage) (uintptr, bool) {
			if m.Message == 0x0111 && uint16(m.WParam>>16) == 0x0300 && m.LParam == c.View && changed != nil {
				changed(Text(c))
			}
			return 0, false
		},
	}
}
func Text(c mygo.NativeViewContext) string {
	n, _, _ := getTextLen.Call(c.View)
	b := make([]uint16, n+1)
	getText.Call(c.View, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return syscall.UTF16ToString(b)
}
func SetText(c mygo.NativeViewContext, text string) {
	setText.Call(c.View, uintptr(unsafe.Pointer(utf16(text))))
}

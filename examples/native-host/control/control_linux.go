//go:build linux && (amd64 || arm64)

package control

import (
	"sync"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
)

var once sync.Once
var entryNew func() uintptr
var entryText func(uintptr) uintptr
var entrySetText func(uintptr, *byte)
var widgetAccessible func(uintptr) uintptr
var accessibleSetName func(uintptr, *byte)
var connect func(uintptr, *byte, uintptr, uintptr, uintptr, uint32) uint64
var disconnect func(uintptr, uint64)
var changedCallback uintptr
var changes = map[uintptr]func(){}

func load() {
	once.Do(func() {
		gtk, err := purego.Dlopen("libgtk-3.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			panic(err)
		}
		object, err := purego.Dlopen("libgobject-2.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			panic(err)
		}
		atk, err := purego.Dlopen("libatk-1.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			panic(err)
		}
		purego.RegisterLibFunc(&entryNew, gtk, "gtk_entry_new")
		purego.RegisterLibFunc(&entryText, gtk, "gtk_entry_get_text")
		purego.RegisterLibFunc(&entrySetText, gtk, "gtk_entry_set_text")
		purego.RegisterLibFunc(&widgetAccessible, gtk, "gtk_widget_get_accessible")
		purego.RegisterLibFunc(&accessibleSetName, atk, "atk_object_set_name")
		purego.RegisterLibFunc(&connect, object, "g_signal_connect_data")
		purego.RegisterLibFunc(&disconnect, object, "g_signal_handler_disconnect")
		changedCallback = purego.NewCallback(func(widget, data uintptr) {
			if fn := changes[data]; fn != nil {
				fn()
			}
		})
	})
}

func Options(initial string, changed func(string)) mygo.NativeViewOptions {
	var signal uint64
	return mygo.NativeViewOptions{
		Create: func(c mygo.NativeViewContext) (uintptr, error) {
			load()
			field := entryNew() // floating reference, adopted by MyGo
			entrySetText(field, cBytes(initial))
			accessibleSetName(widgetAccessible(field), cBytes("Native editor"))
			changes[c.Parent] = func() {
				if changed != nil {
					changed(Text(mygo.NativeViewContext{View: field}))
				}
			}
			signal = connect(field, cBytes("changed"), changedCallback, c.Parent, 0, 0)
			return field, nil
		},
		Dispose: func(c mygo.NativeViewContext) { disconnect(c.View, signal); delete(changes, c.Parent) },
	}
}
func Text(c mygo.NativeViewContext) string          { return cString(entryText(c.View)) }
func SetText(c mygo.NativeViewContext, text string) { entrySetText(c.View, cBytes(text)) }

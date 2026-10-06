//go:build darwin

package control

import (
	"sync"
	"unsafe"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

var once sync.Once
var delegateClass objc.Class
var changes = map[objc.ID]func(){}

// Options creates an owned NSTextField. One delegate class/callback is
// shared by every instance; the delegate object routes it to that field.
func Options(initial string, changed func(string)) mygo.NativeViewOptions {
	var field, delegate objc.ID
	return mygo.NativeViewOptions{
		Create: func(c mygo.NativeViewContext) (uintptr, error) {
			once.Do(func() {
				var err error
				delegateClass, err = objc.RegisterClass("MyGoExampleTextDelegate", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
					{Cmd: objc.RegisterName("controlTextDidChange:"), Fn: func(self objc.ID, _ objc.SEL, note objc.ID) {
						if fn := changes[self]; fn != nil {
							fn()
						}
					}},
				})
				if err != nil {
					panic(err)
				}
			})
			pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(objc.RegisterName("new"))
			defer pool.Send(objc.RegisterName("drain"))
			field = objc.ID(objc.GetClass("NSTextField")).Send(objc.RegisterName("new"))
			field.Send(objc.RegisterName("setBezeled:"), 1)
			field.Send(objc.RegisterName("setEditable:"), 1)
			field.Send(objc.RegisterName("setSelectable:"), 1)
			delegate = objc.ID(delegateClass).Send(objc.RegisterName("new"))
			changes[delegate] = func() {
				if changed != nil {
					changed(Text(mygo.NativeViewContext{View: uintptr(field)}))
				}
			}
			field.Send(objc.RegisterName("setDelegate:"), uintptr(delegate))
			setString(field, "setAccessibilityLabel:", "Native editor")
			setString(field, "setStringValue:", initial)
			return uintptr(field), nil
		},
		Dispose: func(c mygo.NativeViewContext) {
			field.Send(objc.RegisterName("setDelegate:"), 0)
			delete(changes, delegate)
			delegate.Send(objc.RegisterName("release"))
		},
	}
}

func setString(obj objc.ID, selector, text string) {
	str := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("alloc")).Send(objc.RegisterName("initWithUTF8String:"), uintptr(unsafe.Pointer(cBytes(text))))
	obj.Send(objc.RegisterName(selector), uintptr(str))
	str.Send(objc.RegisterName("release"))
}
func Text(c mygo.NativeViewContext) string {
	return cString(uintptr(objc.ID(c.View).Send(objc.RegisterName("stringValue")).Send(objc.RegisterName("UTF8String"))))
}
func SetText(c mygo.NativeViewContext, text string) {
	setString(objc.ID(c.View), "setStringValue:", text)
}

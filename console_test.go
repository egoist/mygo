//go:build !js

package mygo

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/fake"
)

// TestOnConsoleMessage: the first listener makes the page's world wrap
// the console, the bridge's configuration tells the pages it loads next,
// and the current page hears it; what the page logs reaches the listener,
// and the last listener to go unwraps it again.
func TestOnConsoleMessage(t *testing.T) {
	w, fw := readyWindow(t, WindowOptions{Hidden: true})
	p := w.Page()

	// Without a listener, the pages the window loads say nothing about it.
	if scriptsSayConsole(fw) {
		t.Error("the bridge captures the console before anyone listens")
	}

	var got []ConsoleMessage
	off := p.OnConsoleMessage(func(m *ConsoleMessage) { got = append(got, *m) })
	if !scriptsSayConsole(fw) {
		t.Error("the first listener did not turn the capture on for the next pages")
	}
	if m := received(t, fw, func(m map[string]any) bool {
		return m["n"] == "mygo:console-capture" && m["p"] == true
	}); m == nil {
		t.Fatal("the current page was not told to capture")
	}

	page(fw, `{"t":"console","lv":"warn","tx":"careful","u":"app.js","n":7}`)
	want := []ConsoleMessage{{Level: ConsoleWarn, Text: "careful", URL: "app.js", Line: 7}}
	if !slices.Equal(got, want) {
		t.Errorf("messages %v, want %v", got, want)
	}

	// A second listener hears the same, and taking it off changes nothing.
	var also []ConsoleMessage
	off2 := p.OnConsoleMessage(func(m *ConsoleMessage) { also = append(also, *m) })
	page(fw, `{"t":"console","lv":"log","tx":"hi"}`)
	if !slices.Equal(got, append(want, ConsoleMessage{Level: ConsoleLog, Text: "hi"})) ||
		len(also) != 1 {
		t.Errorf("a second listener: %v and %v", got, also)
	}
	off2()
	if !scriptsSayConsole(fw) {
		t.Error("taking one of two listeners off turned the capture off")
	}

	// The last listener to go stops the capture.
	got = nil
	off()
	if scriptsSayConsole(fw) {
		t.Error("the last listener did not turn the capture off")
	}
	if m := received(t, fw, func(m map[string]any) bool {
		return m["n"] == "mygo:console-capture" && m["p"] == false
	}); m == nil {
		t.Fatal("the current page was not told to stop capturing")
	}
	page(fw, `{"t":"console","lv":"error","tx":"unheard"}`)
	if len(got) != 0 {
		t.Errorf("messages after the last listener went: %v", got)
	}
}

// scriptsSayConsole tells whether the pages fw loads next send what they
// log, as their bridge configuration says.
func scriptsSayConsole(fw *fake.Window) bool {
	return strings.Contains(fw.UserScripts()[0].Source, `"console":true`)
}

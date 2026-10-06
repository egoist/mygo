package e2e

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// TestNativeLocale exercises locale plumbing, localized values and native
// accessibility labels on a real surface, including a live window switch.
func TestNativeLocale(t *testing.T) {
	var frames atomic.Int32
	date := time.Date(2024, 2, 29, 13, 5, 0, 0, time.UTC)
	amount := 1234.5
	view := func(c *ui.Context) {
		ui.Column(c).Padding(12).Gap(8).Children(func() {
			ui.DateInput(c, &date).Label("Due")
			ui.TimeInput(c, &date).Label("Alarm")
			ui.NumberInput(c, &amount, 0, 10000, 0.1).Label("Amount")
		})
		frames.Add(1)
	}
	w := newWindow(t, mygo.WindowOptions{Title: "Locale widgets", Locale: "de-DE", Width: 500, Height: 350, Content: ui.View(view)})
	eventually(t, "a localized native frame", func() bool { return frames.Load() > 0 })
	if _, ok := accessibility(w); !ok {
		t.Skip("native accessibility automation unavailable")
	}
	find := func(label, value string) bool {
		nodes, _ := accessibility(w)
		for _, n := range nodes {
			if n.label == label && (value == "" || n.value == value) {
				return true
			}
		}
		return false
	}
	// ATK buttons have no string-value interface. macOS and UIA expose
	// the field's value; on all platforms open its native action and
	// verify the calendar's localized heading through accessibility.
	if runtime.GOOS != "linux" {
		eventually(t, "German native date", func() bool { return find("Due", "29.2.2024") })
	}
	if !accessPerform(w, "Due", "press", "") {
		t.Fatal("cannot open the date picker")
	}
	eventually(t, "German calendar heading", func() bool { return find("Februar 2024", "") })
	eventually(t, "German number increment label", func() bool { return find(ui.NewLocale("de-DE").Text("Increase"), "") })
	before := frames.Load()
	w.SetLocale("fr-FR")
	eventually(t, "locale redraw", func() bool { return frames.Load() > before })
	if runtime.GOOS != "linux" {
		eventually(t, "French native date", func() bool { return find("Due", "29/02/2024") })
	}
	eventually(t, "French calendar heading after live switch", func() bool { return find("février 2024", "") })
	eventually(t, "French native time label", func() bool { return find("Alarm "+ui.NewLocale("fr-FR").Text("hours"), "") })
	if _, err := w.CapturePage(); err != nil {
		t.Fatalf("capture localized surface: %v", err)
	}
	mygo.RunOnMain(func() {
		if amount != 1234.5 || date.Hour() != 13 || date.Day() != 29 {
			t.Error("locale switch changed a value")
		}
	})
}

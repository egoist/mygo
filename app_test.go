//go:build !js

package mygo

import (
	"testing"
)

// TestAppSetters checks what the app says of itself: the name and version
// it is given, the ones the packaged app would give otherwise, and the
// activation policy it was left with.
func TestAppSetters(t *testing.T) {
	defer func(name, version string) { App.SetName(name); App.SetVersion(version) }(App.Name(), App.Version())
	defer func(p ActivationPolicy) { App.SetActivationPolicy(p) }(App.ActivationPolicy())

	if App.IsPackaged() {
		t.Fatal("the test binary is not packaged")
	}
	App.SetName("")
	if got := App.Name(); got == "" || got == "Probe" {
		t.Errorf("Name without one set = %q, want the executable's", got)
	}
	App.SetName("Probe")
	if got := App.Name(); got != "Probe" {
		t.Errorf("Name = %q, want %q", got, "Probe")
	}

	// A version the app was not given is the packaged app's, which the
	// test binary has none of.
	if got := App.Version(); got != "" {
		t.Errorf("Version without one set = %q, want %q", got, "")
	}
	App.SetVersion("4.2.0")
	if got := App.Version(); got != "4.2.0" {
		t.Errorf("Version = %q, want %q", got, "4.2.0")
	}

	if got := App.ActivationPolicy(); got != ActivationPolicyRegular {
		t.Errorf("the policy left alone is %q, want %q", got, ActivationPolicyRegular)
	}
	App.SetActivationPolicy(ActivationPolicyAccessory)
	if got := App.ActivationPolicy(); got != ActivationPolicyAccessory {
		t.Errorf("ActivationPolicy = %q, want %q", got, ActivationPolicyAccessory)
	}
}

// TestAppMenuSetWhileRunning checks that a menu set while the app runs is
// the app's and reaches the backend, as one set before Run does.
func TestAppMenuSetWhileRunning(t *testing.T) {
	defer App.SetMenu(App.Menu())
	m := NewMenu([]*MenuItem{{Label: "Probe", ID: "probe"}})
	App.SetMenu(m)
	if App.Menu() != m {
		t.Error("App.Menu is not the menu SetMenu was given")
	}
	got := onMainValue(fb.AppMenu)
	if got == nil || len(got.Items) != 1 || got.Items[0].Label != "Probe" {
		t.Errorf("the backend's menu = %+v, want the one item Probe", got)
	}
	App.SetMenu(nil)
	if App.Menu() != nil {
		t.Error("SetMenu(nil) kept the menu")
	}
	if got := onMainValue(fb.AppMenu); got != nil {
		t.Errorf("the backend kept the menu: %+v", got)
	}
}

// TestAppActive checks that focusing the app makes it the active one, as
// the system taking the activity away makes it not.
func TestAppActive(t *testing.T) {
	onMain(func() { fb.SetActive(false) })
	if App.IsActive() {
		t.Error("the app is active while the system says it is not")
	}
	App.Focus()
	if !onMainValue(func() bool { return fb.Active }) {
		t.Error("Focus did not ask the backend to activate")
	}
	if !App.IsActive() {
		t.Error("the app is not active after Focus")
	}
	onMain(func() { fb.SetActive(false) })
	if App.IsActive() {
		t.Error("the app stays active after the system took the activity away")
	}
}

// TestAppLifecycleEvents checks what the app hears of its own life: being
// activated and losing the activity, the URLs and files it is asked to
// open, and a shortage of memory. Listeners are told until turned off.
func TestAppLifecycleEvents(t *testing.T) {
	var activated []bool
	var became, resigned, warned int
	var urls, files []string
	offActivate := App.OnActivate(func(visible bool) { activated = append(activated, visible) })
	defer offActivate()
	offBecame := App.OnDidBecomeActive(func() { became++ })
	defer offBecame()
	offResigned := App.OnDidResignActive(func() { resigned++ })
	defer offResigned()
	offWarned := App.OnMemoryWarning(func() { warned++ })
	defer offWarned()
	offURL := App.OnOpenURL(func(u string) { urls = append(urls, u) })
	defer offURL()
	offFile := App.OnOpenFile(func(p string) { files = append(files, p) })
	defer offFile()

	onMain(func() { appHandler{}.Activated(true) })
	onMain(func() { appHandler{}.Activated(false) })
	if len(activated) != 2 || !activated[0] || activated[1] {
		t.Errorf("OnActivate heard %v, want [true false]", activated)
	}

	// The system making the app active, and taking the activity away,
	// tells the app of both, as a window's focus does not.
	onMain(func() { fb.SetActive(true) })
	onMain(func() { fb.SetActive(false) })
	if became != 1 || resigned != 1 {
		t.Errorf("the app became active %d times and resigned %d, want 1 and 1", became, resigned)
	}

	onMain(func() { appHandler{}.OpenURLs([]string{"mygo://a", "mygo://b"}) })
	if len(urls) != 2 || urls[0] != "mygo://a" || urls[1] != "mygo://b" {
		t.Errorf("OnOpenURL heard %q", urls)
	}
	onMain(func() { appHandler{}.OpenFiles([]string{"/tmp/a.md"}) })
	if len(files) != 1 || files[0] != "/tmp/a.md" {
		t.Errorf("OnOpenFile heard %q", files)
	}
	onMain(func() { appHandler{}.MemoryWarning() })
	if warned != 1 {
		t.Errorf("OnMemoryWarning ran %d times, want 1", warned)
	}

	offBecame()
	onMain(func() { fb.SetActive(true) })
	if became != 1 {
		t.Errorf("the turned off listener was told of the app becoming active again: %d times", became)
	}
}

// TestDockHideShow checks that hiding and showing the Dock icon are the
// app's activation policy changes they say they are.
func TestDockHideShow(t *testing.T) {
	defer func(p ActivationPolicy) { App.SetActivationPolicy(p) }(App.ActivationPolicy())
	App.Dock.Hide()
	if got := App.ActivationPolicy(); got != ActivationPolicyAccessory {
		t.Errorf("hiding the Dock icon left the policy %q", got)
	}
	App.Dock.Show()
	if got := App.ActivationPolicy(); got != ActivationPolicyRegular {
		t.Errorf("showing the Dock icon left the policy %q", got)
	}
}

//go:build !js

package mygo

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// What OnSaveState gives is written where the next launch finds it: under
// mygo dev the file is written, read back whole, and left for a restart
// before the next write, and giving nothing removes it; a desktop writes
// nothing and reads nothing.
func TestRestoredState(t *testing.T) {
	old, had := App.paths[PathUserData]
	dir := t.TempDir()
	App.SetPath(PathUserData, dir)
	t.Cleanup(func() {
		if had {
			App.SetPath(PathUserData, old)
		} else {
			delete(App.paths, PathUserData)
		}
	})
	path := filepath.Join(dir, "restoration.state")

	// OnSaveState while the app is not under mygo dev starts no keeping;
	// the flag of it makes writeState and RestoredState act.
	var saved []byte
	App.OnSaveState(func() []byte { return saved })
	t.Cleanup(func() { App.saveState = nil })
	onMain(func() { devLaunched = true })
	t.Cleanup(func() { onMain(func() { devLaunched = false }) })

	saved = []byte("tab=3")
	onMain(func() { appHandler{}.PowerEvent("suspend") })
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, saved) {
		t.Fatalf("kept %q: %v", data, err)
	}
	if got := App.RestoredState(); !bytes.Equal(got, saved) {
		t.Fatalf("restored %q", got)
	}
	// mygo dev may end the app before it wrote anew: the file stays, as it
	// stays through a resume, which forgets it only off dev.
	if _, err := os.Stat(path); err != nil {
		t.Error("the kept state was removed with the read")
	}
	onMain(func() { appHandler{}.PowerEvent("resume") })
	if _, err := os.Stat(path); err != nil {
		t.Error("the kept state was removed with the resume")
	}

	// Giving nothing removes it.
	saved = nil
	onMain(func() { App.writeState() })
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("nothing given kept the state: %v", err)
	}

	// Not under mygo dev, a desktop writes nothing and reads nothing.
	onMain(func() { devLaunched = false })
	saved = []byte("tab=4")
	onMain(func() { App.writeState() })
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a desktop kept state: %v", err)
	}
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := App.RestoredState(); got != nil {
		t.Fatalf("a desktop restored %q", got)
	}
}

// KeepState writes the value down as it goes, and the next launch comes
// back with it.
func TestKeepState(t *testing.T) {
	old, had := App.paths[PathUserData]
	dir := t.TempDir()
	App.SetPath(PathUserData, dir)
	t.Cleanup(func() {
		if had {
			App.SetPath(PathUserData, old)
		} else {
			delete(App.paths, PathUserData)
		}
	})
	path := filepath.Join(dir, "restoration.state")
	t.Cleanup(func() { onMain(func() { App.saveState, devLaunched = nil, false }) })

	type kept struct {
		Tab   int
		Title string
	}
	// Off mygo dev, KeepState starts no keeping every second, which would
	// outlive the test: the flag of it is set only as the state is read back.
	v := &kept{Tab: 3, Title: "Docs"}
	KeepState(v)

	// The value is written down as it is when it is asked for, not as it
	// was when KeepState was called. KeepState reads it on the main thread.
	onMain(func() { v.Tab = 5 })
	data := onMainValue(func() []byte { return App.saveState() })
	if data == nil {
		t.Fatal("KeepState gave nothing to keep")
	}

	// The next launch, under mygo dev, reads it back whole as the app is
	// ready, which runs after this call on the main thread.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	back := &kept{}
	onMain(func() {
		KeepState(back)
		devLaunched = true
	})
	waitFor(t, "the kept state to come back", func() bool {
		return onMainValue(func() bool { return *back == kept{Tab: 5, Title: "Docs"} })
	})
	if onMainValue(func() bool { return App.keepingState }) {
		t.Error("the test started keeping the state every second")
	}
}

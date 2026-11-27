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

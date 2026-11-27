package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/wait"
)

// viewProject writes a native app with one .view file and no frontend:
// its main calls the View the markup stands for, and nothing of the app
// compiles before the views are generated (#1457).
func viewProject(t *testing.T, view string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.FromSlash("../.."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	files := map[string]string{
		"mygo.json": `{"name": "counter", "identifier": "com.example.counter"}`,
		"go.mod":    "module counter\n\ngo 1.25\n\nrequire github.com/egoist/mygo v0.0.0\n\nreplace github.com/egoist/mygo => " + filepath.ToSlash(root) + "\n",
		"counter.go": `package main

type counter struct{ n int }

func (s *counter) Increment() { s.n++ }
`,
		"counter.view": view,
		"main.go": `package main

import "github.com/egoist/mygo/ui"

func main() { _ = ui.View((&counter{}).View) }
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %s", out)
	}
	return dir
}

const testCounterView = `<view type="counter">
<column fill>
	<text>{n}</text>
	<button on-clicked="Increment">Up</button>
</column>
</view>
`

// TestGenerateViewsOfANativeApp checks the CLI round: a pure native app
// with a .view file generates its views and no client, where before it
// had no frontend to generate.
func TestGenerateViewsOfANativeApp(t *testing.T) {
	dir := viewProject(t, testCounterView)
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatalf("mygo generate: %v", err)
	}
	src, err := os.ReadFile(filepath.Join(dir, "counter_view.go"))
	if err != nil {
		t.Fatal("no counter_view.go written")
	}
	if !strings.Contains(string(src), "func (s *counter) View(c *ui.Context) {") {
		t.Fatalf("generated: %s", src)
	}
	// The app builds with what was generated.
	if out := runGo(t, dir, "build", "./..."); out != "" {
		t.Fatalf("go build: %s", out)
	}
	// Again: nothing changes.
	before, _ := os.Stat(filepath.Join(dir, "counter_view.go"))
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(filepath.Join(dir, "counter_view.go"))
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("the second generate rewrote an unchanged view")
	}
}

// TestGenerateWithoutViewsKeepsItsError keeps the no-frontend error for
// apps with nothing to generate.
func TestGenerateWithoutViewsKeepsItsError(t *testing.T) {
	dir := viewProject(t, "")
	if err := os.Remove(filepath.Join(dir, "counter.view")); err != nil {
		t.Fatal(err)
	}
	err := runGenerate([]string{dir})
	if err == nil || !strings.Contains(err.Error(), "no frontend") {
		t.Fatalf("err: %v", err)
	}
}

// TestBuildGeneratesViewsFirst checks that a release build of an app
// without its generated files writes them before compiling.
func TestBuildGeneratesViewsFirst(t *testing.T) {
	if testing.Short() {
		t.Skip("release build")
	}
	dir := viewProject(t, testCounterView)
	err := runBuild([]string{"-skip-dmg", "-skip-installer", dir})
	signing := err != nil && (strings.Contains(err.Error(), "codesign") || strings.Contains(err.Error(), "signing identity"))
	if err != nil && !signing {
		t.Fatalf("mygo build: %v", err)
	}
	if signing {
		t.Skipf("no signing identity: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "counter_view.go")); err != nil {
		t.Fatal("the build did not generate the views first")
	}
}

func runGo(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out)
	}
	return ""
}

// TestLastViewDeletedKeepsItsOutput: with every .view gone the
// generator owns nothing any more — a package that left the .view world
// keeps every file, hand-edited or not.
func TestLastViewDeletedKeepsItsOutput(t *testing.T) {
	dir := viewProject(t, testCounterView)
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "counter.view")); err != nil {
		t.Fatal(err)
	}
	if err := runGenerate([]string{dir}); err == nil || !strings.Contains(err.Error(), "no frontend") {
		t.Fatalf("second generate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "counter_view.go")); err != nil {
		t.Fatal("the last view's output was taken with it")
	}
}

// TestWatchViewsSkipsOwnedOutputsAndWatchesHandEdits checks that mygo
// dev's watcher does not report untouched *_view.go outputs (even when
// the package also embeds files in the same directory and refreshes its
// inputs after a build), while reporting .view edits and hand-edited
// *_view.go files.
func TestWatchViewsSkipsOwnedOutputsAndWatchesHandEdits(t *testing.T) {
	dir := viewProject(t, testCounterView)
	if err := os.WriteFile(filepath.Join(dir, "asset.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "embed.go"), []byte("package main\n\nimport _ \"embed\"\n\n//go:embed asset.txt\nvar asset string\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	in, err := listBuildInputs(c)
	if err != nil {
		t.Fatal(err)
	}
	w := &watcher{}
	w.set(in)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes := w.watch(ctx, 20*time.Millisecond)

	expectQuiet := func(what string) {
		t.Helper()
		select {
		case got := <-changes:
			t.Fatalf("%s: unexpected change reported: %v", what, got)
		case <-time.After(200 * time.Millisecond):
		}
	}
	expectQuiet("initial baseline with generated counter_view.go")

	// Simulate devSession refreshing buildInputs after a build.
	in2, err := listBuildInputs(c)
	if err != nil {
		t.Fatal(err)
	}
	w.set(in2)
	expectQuiet("after refreshing buildInputs")

	// Editing the .view file triggers one rebuild; regenerating its
	// *_view.go during that build does not trigger a second one.
	updatedView := strings.Replace(testCounterView, "Up", "More", 1)
	if err := os.WriteFile(filepath.Join(dir, "counter.view"), []byte(updatedView), 0o644); err != nil {
		t.Fatal(err)
	}
	got := wait.For(t, ".view edit reported", changes)
	if len(got) != 1 || filepath.Base(got[0]) != "counter.view" {
		t.Fatalf("got changes %v, want counter.view", got)
	}
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatal(err)
	}
	expectQuiet("regenerated counter_view.go stays ignored")

	// Deleting counter.view and hand-editing counter_view.go transfers
	// ownership to the author: edits to counter_view.go now report.
	if err := os.Remove(filepath.Join(dir, "counter.view")); err != nil {
		t.Fatal(err)
	}
	_ = wait.For(t, ".view removal reported", changes)

	raw, err := os.ReadFile(filepath.Join(dir, "counter_view.go"))
	if err != nil {
		t.Fatal(err)
	}
	handEdited := strings.Replace(string(raw), `"More"`, `"HandEdited"`, 1)
	if err := os.WriteFile(filepath.Join(dir, "counter_view.go"), []byte(handEdited), 0o644); err != nil {
		t.Fatal(err)
	}
	got = wait.For(t, "hand-edited counter_view.go reported", changes)
	if !slices.ContainsFunc(got, func(p string) bool { return filepath.Base(p) == "counter_view.go" }) {
		t.Fatalf("hand-edited counter_view.go was not reported: %v", got)
	}
}

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
	if err := runBuild([]string{}); err != nil {
		// No code signing identity on a test machine is fine: the views
		// must still have been written before the build reached it.
		if !strings.Contains(err.Error(), "sign") && !strings.Contains(err.Error(), "certificate") {
			t.Fatalf("mygo build: %v", err)
		}
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

// TestLastViewDeletedCleansItsOutput: with every .view gone, generate
// takes the generator's own outputs with them, and a project with none
// of it stays a fast no-op.
func TestLastViewDeletedCleansItsOutput(t *testing.T) {
	dir := viewProject(t, testCounterView)
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "counter_view.go")); err != nil {
		t.Fatal("no output written")
	}
	if err := os.Remove(filepath.Join(dir, "counter.view")); err != nil {
		t.Fatal(err)
	}
	// The project has no frontend any more: the error says so, and the
	// generator's output went with its source.
	err := runGenerate([]string{dir})
	if err == nil || !strings.Contains(err.Error(), "no frontend") {
		t.Fatalf("second generate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "counter_view.go")); err == nil {
		t.Fatal("the deleted view's output stayed")
	}
}

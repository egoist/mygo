package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the CLI's default release configuration, including linker
// reachability: developer hooks must not retain playground code in an
// ordinary native app even though it uses the same UI engine.
func TestProductionBuildExcludesPlayground(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a native application")
	}
	t.Setenv("GOFLAGS", "")
	t.Setenv("MYGO_INSPECTOR", "")
	bin := filepath.Join(t.TempDir(), "counter")
	args := []string{"build", "-buildvcs=false", "-trimpath", "-o", bin}
	args = append(args, productionTags(false)...)
	args = append(args, "../../examples/counter-native")
	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("default production build: %v\n%s", err, out)
	}
	out, err := exec.Command("go", "tool", "nm", bin).CombinedOutput()
	if err != nil {
		t.Fatalf("read release symbols: %v\n%s", err, out)
	}
	symbols := string(out)
	if !strings.Contains(symbols, "ui.(*engine).runFrame") {
		t.Fatal("fixture did not link the ordinary native UI engine")
	}
	for _, symbol := range []string{
		"ui.NewPreview", "ui.PreviewEnvironment", "ui.(*Preview)",
		"ui.previewSession", "ui.previewHost", "ui.previewRuntime",
		"ui.previewAttach", "ui.previewPrepare", "ui.previewAppearance", "ui.previewPreferences",
	} {
		if strings.Contains(symbols, symbol) {
			t.Errorf("default release retained developer symbol %s", symbol)
		}
	}
}

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateUIPreviewWriteAndIgnoredDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	source := []byte("package main\nimport \"github.com/egoist/mygo/ui\"\nfunc view(c *ui.Context) { ui.Text(c,\"Hello\") }\n")
	if err := os.WriteFile(path, source, 0600); err != nil {
		t.Fatal(err)
	}
	ignored := filepath.Join(dir, "node_modules")
	if err := os.Mkdir(ignored, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ignored, "invalid.go"), []byte("invalid Go"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runMigrateUI([]string{dir}); err != nil {
		t.Fatal(err)
	}
	preview, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(preview) != string(source) {
		t.Fatal("preview wrote the app")
	}
	if err := runMigrateUI([]string{"-write", dir}); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "c ui.Frame") {
		t.Fatal("write did not migrate the view")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("migration changed file permissions")
	}
}

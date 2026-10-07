package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWrapLinesPreservesTextAndClusterBoundaries(t *testing.T) {
	for _, value := range []string{"", "A long ordinary sentence with spaces", "e\u0301 e\u0301 e\u0301", "日本語の文書"} {
		lines := wrapLines(value, 30)
		if len(lines) == 0 || strings.Join(lines, "") != value {
			t.Fatalf("wrapping lost content: %q => %q", value, lines)
		}
		for _, line := range lines {
			if strings.HasPrefix(line, "\u0301") {
				t.Fatalf("wrapping split a combining cluster: %q", lines)
			}
		}
	}
	if got := wrapLines("one\r\n\r\ntwo", 1000); strings.Join(got, "\n") != "one\n\ntwo" {
		t.Fatal("paragraphs changed", got)
	}
}

func TestAtomicSavePreservesOriginalOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "text.txt")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(path, "new"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "new" {
		t.Fatalf("save: %q %v", data, err)
	}
	// Replacing a nonempty directory fails after writing the temporary file.
	if err := atomicWrite(dir, "invalid"); err == nil {
		t.Fatal("replaced a directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "text.txt" {
		t.Fatal("temporary save leaked", entries)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "new" {
		t.Fatal("failed save damaged the original")
	}
}

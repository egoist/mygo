//go:build darwin

package darwin

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/transfer"
)

// Format translation needs Foundation/AppKit, but no application or window.
func TestDataDragFormatMappings(t *testing.T) {
	load()
	withPool(func() {
		registerDataDragClasses()
		for _, f := range []transfer.Format{transfer.Text, transfer.HTML, transfer.PNG, transfer.URIList, transfer.FileList, "application/vnd.mygo.test+json"} {
			typ := macType(f)
			if got := macFormat(typ); got != f {
				t.Errorf("%s -> %s -> %s", f, typ, got)
			}
			item := autorelease(send(send(class("NSPasteboardItem"), "alloc"), "init"))
			if !sendBool(item, "setData:forType:", uintptr(nsData([]byte("a\r\nb"))), uintptr(nsString(typ))) {
				t.Errorf("pasteboard rejected type %s", typ)
			}
		}
		if macType(transfer.URIList) == "public.url" {
			t.Fatal("URI lists were narrowed to a single URL")
		}
		payload := []byte("https://example.com/a\r\nhttps://example.com/b")
		src := &macDataSource{r: platform.DragRequest{Data: transfer.New(transfer.NewItem(transfer.Bytes(transfer.URIList, payload)))}}
		item := src.pasteboardItems()[0]
		defer func() {
			for _, p := range src.providers {
				delete(macDragProviders, p)
				release(p)
			}
		}()
		actual := goBytes(send(item, "dataForType:", uintptr(nsString(macType(transfer.URIList)))))
		if !bytes.Equal(actual, payload) {
			t.Errorf("URI-list provider: %q", actual)
		}
		first := goBytes(send(item, "dataForType:", uintptr(nsString("public.url"))))
		if string(first) != "https://example.com/a" {
			t.Errorf("public URL fallback: %q", first)
		}
	})
}

func TestHoverFilesFromPasteboard(t *testing.T) {
	load()
	dir := t.TempDir()
	// Resolve symlinks on TempDir (/var -> /private/var) so fileReferenceURL matches.
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	sub := filepath.Join(dir, "sub dir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	p1 := filepath.Join(dir, "my photo #1 %20.png")
	p2 := filepath.Join(sub, "note.txt")
	for _, p := range []string{p1, p2} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	withPool(func() {
		pb := send(class("NSPasteboard"), "pasteboardWithUniqueName")
		defer send(pb, "releaseGlobally")

		refURL := send(fileURL(p1), "fileReferenceURL")
		if refURL == 0 {
			t.Fatal("fileReferenceURL returned nil")
		}
		if !sendBool(pb, "writeObjects:", uintptr(nsArray(refURL, fileURL(p2)))) {
			t.Fatal("writeObjects with file URLs failed")
		}

		items := arrayItems(send(pb, "pasteboardItems"))
		s := &surface{}
		got := s.hoverFiles(pb, items)
		want := []string{p1, p2}
		if !slices.Equal(got, want) {
			t.Fatalf("hoverFiles(ref+path) = %#v; want %#v", got, want)
		}

		// Caller mutation must not corrupt cached paths; cache hit ignores nil items.
		got[0] = "mutated"
		cached := s.hoverFiles(pb, nil)
		if !slices.Equal(cached, want) {
			t.Fatalf("cached hoverFiles = %#v; want %#v", cached, want)
		}

		// Clearing cache + writing non-file data yields nil.
		s.clearDragFiles()
		if s.dragFilesPB != 0 || s.dragFilesChange != 0 || s.dragFiles != nil {
			t.Fatalf("clearDragFiles left state: pb=%v change=%v files=%v", s.dragFilesPB, s.dragFilesChange, s.dragFiles)
		}
		send(pb, "clearContents")
		if !sendBool(pb, "writeObjects:", uintptr(nsArray(nsString("plain text")))) {
			t.Fatal("writeObjects with string failed")
		}
		if nonFile := s.hoverFiles(pb, arrayItems(send(pb, "pasteboardItems"))); nonFile != nil {
			t.Fatalf("non-file hoverFiles = %#v; want nil", nonFile)
		}

		// Legacy NSFilenamesPboardType fallback without public.file-url pasteboard items.
		pbLegacy := send(class("NSPasteboard"), "pasteboardWithUniqueName")
		defer send(pbLegacy, "releaseGlobally")
		send(pbLegacy, "declareTypes:owner:", uintptr(nsArray(nsString("NSFilenamesPboardType"))), 0)
		if !sendBool(pbLegacy, "setPropertyList:forType:", uintptr(nsArray(nsString(p1), nsString(p2))), uintptr(nsString("NSFilenamesPboardType"))) {
			t.Fatal("setPropertyList NSFilenamesPboardType failed")
		}
		if legacy := macPasteboardFiles(pbLegacy, nil); !slices.Equal(legacy, want) {
			t.Fatalf("NSFilenamesPboardType fallback = %#v; want %#v", legacy, want)
		}
	})
}

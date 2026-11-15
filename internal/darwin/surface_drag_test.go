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

type dragEventHandler struct {
	platform.WindowHandler
	over *platform.DataDragEvent
	drop *platform.DataDragEvent
}

func (h *dragEventHandler) SurfaceEvent(ev platform.SurfaceEvent) bool {
	switch ev.Kind {
	case platform.DataDragOver:
		h.over = ev.Drag
		if slices.Contains(ev.Drag.Offer.Formats, transfer.FileList) {
			ev.Drag.Formats = []transfer.Format{transfer.FileList}
			ev.Drag.Operation = transfer.Copy
			return true
		}
	case platform.DataDrop:
		h.drop = ev.Drag
		return true
	}
	return false
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
		registerDataDragClasses()
		initTestDragInfo()

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
		h := &dragEventHandler{}
		s := &surface{w: &window{b: &Backend{}, h: h}}
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

		// External drag through MyGoTestDraggingInfo (Finder-style, draggingSource == 0)
		// populates Offer.Files during hover.
		testDragMask = 1
		testDragSource = 0
		send(testDragPB, "clearContents")
		if !sendBool(testDragPB, "writeObjects:", uintptr(nsArray(refURL, fileURL(p2)))) {
			t.Fatal("writeObjects to testDragPB failed")
		}
		if !s.nativeDataEvent(testDragInfo, platform.DataDragOver) {
			t.Fatal("external DataDragOver returned false")
		}
		if h.over == nil || !h.over.HasFiles || !slices.Equal(h.over.Offer.Files, want) {
			t.Fatalf("external DataDragOver: over=%+v; want HasFiles=true Files=%#v", h.over, want)
		}
		s.nativeDataEvent(testDragInfo, platform.DataDragLeave)

		// In-app drag with a lazy FileList provider must leave Offer.Files nil
		// during hover and never invoke the provider until drop.
		fileData, err := transfer.FileData(p1)
		if err != nil {
			t.Fatal(err)
		}
		fileBytes, err := fileData.Read(transfer.FileList)
		if err != nil {
			t.Fatal(err)
		}
		lazyCalls := 0
		lazyData := transfer.New(transfer.NewItem(transfer.Lazy(transfer.FileList, func() ([]byte, error) {
			lazyCalls++
			return fileBytes, nil
		})))
		src := &macDataSource{r: platform.DragRequest{Data: lazyData.Snapshot(), Operations: transfer.Copy}}
		defer func() {
			testDragSource = 0
			send(testDragPB, "clearContents")
			for _, p := range src.providers {
				delete(macDragProviders, p)
				release(p)
			}
		}()
		testDragSource = testDragInfo
		send(testDragPB, "clearContents")
		if !sendBool(testDragPB, "writeObjects:", uintptr(nsArray(src.pasteboardItems()...))) {
			t.Fatal("writeObjects with in-app pasteboardItems failed")
		}
		h.over, h.drop = nil, nil
		if !s.nativeDataEvent(testDragInfo, platform.DataDragOver) {
			t.Fatal("in-app DataDragOver returned false")
		}
		if lazyCalls != 0 {
			t.Fatalf("in-app hover invoked lazy FileList provider %d times; want 0", lazyCalls)
		}
		if h.over == nil || !h.over.HasFiles || h.over.Offer.Files != nil {
			t.Fatalf("in-app DataDragOver: HasFiles=%v Files=%#v; want true, nil", h.over != nil && h.over.HasFiles, h.over.Offer.Files)
		}
		if direct := s.hoverFiles(testDragPB, arrayItems(send(testDragPB, "pasteboardItems"))); direct != nil || lazyCalls != 0 {
			t.Fatalf("in-app hoverFiles = %#v, lazyCalls = %d; want nil, 0", direct, lazyCalls)
		}
		if !s.nativeDataEvent(testDragInfo, platform.DataDrop) {
			t.Fatal("in-app DataDrop returned false")
		}
		if lazyCalls != 1 {
			t.Fatalf("in-app DataDrop invoked lazy FileList provider %d times; want 1", lazyCalls)
		}
		if droppedFiles, err := h.drop.Data.Files(); err != nil || !slices.Equal(droppedFiles, []string{p1}) {
			t.Fatalf("in-app DataDrop Files() = %#v, %v; want %#v", droppedFiles, err, []string{p1})
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
		for _, p := range src.providers {
			delete(macDragProviders, p)
			release(p)
		}
		src.providers = nil
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

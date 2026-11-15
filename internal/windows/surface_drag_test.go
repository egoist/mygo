//go:build windows && (amd64 || arm64)

package windows

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"syscall"
	"testing"
	"unsafe"

	"github.com/egoist/mygo/transfer"
)

func TestTransferDataStreamPosition(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dropOnce.Do(initDropTarget)

	format := uint16(registerClipboardFormat("application/vnd.mygo.stream-test"))
	for _, clipboard := range []bool{false, true} {
		for _, payload := range [][]byte{nil, {0, 1, 2, 0}, bytes.Repeat([]byte{1, 0}, 64<<10)} {
			checkTransferDataStream(t, format, clipboard, payload)
		}
	}
}

func checkTransferDataStream(t *testing.T, format uint16, clipboard bool, payload []byte) {
	t.Helper()
	data := transfer.New(transfer.NewItem(transfer.Bytes("application/vnd.mygo.stream-test", payload)))
	object := newTransferData(data, "", clipboard, false)
	defer release(object)
	f := nativeFormat(format)
	f.Tymed = tymedStream
	var medium stgMedium
	if hr := comCall(object, dataGetData, uintptr(unsafe.Pointer(&f)), uintptr(unsafe.Pointer(&medium))); failed(hr) {
		t.Fatal(hresultError("IDataObject::GetData", hr))
	}
	defer procReleaseStgMedium.Call(uintptr(unsafe.Pointer(&medium)))
	if medium.Tymed != tymedStream || medium.Handle == 0 {
		t.Fatalf("GetData returned an invalid stream: %+v", medium)
	}
	var position uint64
	if hr := comCall(medium.Handle, 5, 0, 1, uintptr(unsafe.Pointer(&position))); failed(hr) { // STREAM_SEEK_CUR
		t.Fatal(hresultError("IStream::Seek", hr))
	}
	if position != uint64(len(payload)) {
		t.Fatalf("clipboard=%t: GetData stream position %d, want payload length %d", clipboard, position, len(payload))
	}
	if got, err := readDragStream(medium.Handle); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("clipboard=%t: readDragStream returned %d bytes: %v; want %d", clipboard, len(got), err, len(payload))
	}
	if got, err := oleDataBytes(object, format); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("clipboard=%t: oleDataBytes returned %d bytes: %v; want %d", clipboard, len(got), err, len(payload))
	}
}

type failHDropObject struct {
	vtbl *[12]uintptr
}

func TestOleOfferHoverFiles(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "my photo #1 %20.png")
	p2 := filepath.Join(dir, "note.txt")
	for _, p := range []string{p1, p2} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	paths := []string{p1, p2}
	obj := newTestDataObject(paths)
	defer release(obj)
	drag, _ := oleOffer(obj, dropEffectCopy, 0)
	if !drag.HasFiles || !slices.Equal(drag.Offer.Files, paths) {
		t.Fatalf("newTestDataObject oleOffer: HasFiles=%v Files=%#v; want true, %#v", drag.HasFiles, drag.Offer.Files, paths)
	}

	fileData, err := transfer.FileData(p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	fileObj := newTransferData(fileData, "", false, false)
	defer release(fileObj)
	fileDrag, _ := oleOffer(fileObj, dropEffectCopy, 0)
	if !fileDrag.HasFiles || !slices.Equal(fileDrag.Offer.Files, paths) {
		t.Fatalf("FileData oleOffer: HasFiles=%v Files=%#v; want true, %#v", fileDrag.HasFiles, fileDrag.Offer.Files, paths)
	}

	textObj := newTransferData(transfer.TextData("hello"), "", false, false)
	defer release(textObj)
	textDrag, _ := oleOffer(textObj, dropEffectCopy, 0)
	if textDrag.HasFiles || textDrag.Offer.Files != nil {
		t.Fatalf("TextData oleOffer: HasFiles=%v Files=%#v; want false, nil", textDrag.HasFiles, textDrag.Offer.Files)
	}

	// Simulated promised/unreadable CF_HDROP object: QueryGetData advertises
	// cfHDrop, while GetData returns eFail so Offer.Files remains nil.
	stub := syscall.NewCallback(func(this, a, b, c, d uintptr) uintptr { return eFail })
	vtbl := [12]uintptr{
		syscall.NewCallback(func(this, riid, out uintptr) uintptr { return eNoInterface }),
		syscall.NewCallback(func(this uintptr) uintptr { return 2 }),
		syscall.NewCallback(func(this uintptr) uintptr { return 1 }),
		syscall.NewCallback(func(this, fePtr, medPtr uintptr) uintptr { return eFail }), // GetData
		stub, // GetDataHere
		syscall.NewCallback(func(this, fePtr uintptr) uintptr { // QueryGetData
			fe := (*formatEtc)(native(fePtr))
			if fe.Format == cfHDrop && fe.Tymed&tymedHGlobal != 0 {
				return sOK
			}
			return eFail
		}),
		stub, stub,
		syscall.NewCallback(func(this, dir, out uintptr) uintptr { return eFail }), // EnumFormatEtc
		stub, stub, stub,
	}
	failObj := &failHDropObject{vtbl: &vtbl}
	failDrag, _ := oleOffer(uintptr(unsafe.Pointer(failObj)), dropEffectCopy, 0)
	runtime.KeepAlive(failObj)
	if !failDrag.HasFiles || failDrag.Offer.Files != nil {
		t.Fatalf("failing CF_HDROP oleOffer: HasFiles=%v Files=%#v; want true, nil", failDrag.HasFiles, failDrag.Offer.Files)
	}
}

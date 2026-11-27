// Package squashfstest reads SquashFS images in tests, to check what
// package squashfs writes.
package squashfstest

import (
	"bytes"
	"encoding/binary"
	"io/fs"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// The kinds of Entry.
const (
	Dir     = 1
	File    = 2
	Symlink = 3
)

// Entry is a directory, a file or a link of an image.
type Entry struct {
	Kind   int
	Perm   fs.FileMode
	Data   string // a file's
	Target string // a link's
}

// Constants of the format.
const (
	magic        = 0x73717368
	zstdID       = 6
	blockSize    = 128 << 10
	metaRaw      = 0x8000
	dataRaw      = 1 << 24
	noFragment   = 0xFFFFFFFF
	typeDir      = 1
	typeFile     = 2
	typeSymlink  = 3
	typeLongDir  = 8
	typeLongFile = 9
)

// Read reads an image, from its superblock, and returns its entries by
// path, failing t on what does not follow the format: a reader of its own
// for tests, written from the specification rather than from the writer.
func Read(t testing.TB, img []byte) map[string]Entry {
	t.Helper()
	le := binary.LittleEndian
	if le.Uint32(img) != magic || le.Uint16(img[20:]) != zstdID || le.Uint16(img[28:]) != 4 {
		t.Fatal("not a SquashFS 4.0 image compressed with zstd")
	}
	root := le.Uint64(img[32:])
	used := le.Uint64(img[40:])
	inodeStart, dirStart, fragStart := le.Uint64(img[64:]), le.Uint64(img[72:]), le.Uint64(img[80:])
	idTable := le.Uint64(img[48:])
	if used > uint64(len(img)) {
		t.Fatalf("%d bytes used of %d", used, len(img))
	}
	dec, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	// A table's blocks, uncompressed one after the other, and where each
	// block starts in that.
	table := func(from, to uint64) ([]byte, map[uint64]int) {
		var data []byte
		starts := map[uint64]int{}
		for p := from; p < to; {
			h := le.Uint16(img[p:])
			size := uint64(h &^ metaRaw)
			block := img[p+2 : p+2+size]
			if h&metaRaw == 0 {
				var err error
				if block, err = dec.DecodeAll(block, nil); err != nil {
					t.Fatal(err)
				}
			}
			starts[p-from] = len(data)
			data = append(data, block...)
			p += 2 + size
		}
		return data, starts
	}
	inodes, inodeBlocks := table(inodeStart, dirStart)
	dirs, dirBlocks := table(dirStart, fragStart)
	idBlock := le.Uint64(img[idTable:])
	if ids, _ := table(idBlock, idTable); len(ids) != 4 || le.Uint32(ids) != 0 {
		t.Errorf("IDs %v, want root alone", ids)
	}
	at := func(ref uint64, starts map[uint64]int) int {
		s, ok := starts[ref>>16]
		if !ok {
			t.Fatalf("no metadata block at %d", ref>>16)
		}
		return s + int(ref&0xFFFF)
	}
	out := map[string]Entry{}
	var walk func(name string, ref uint64)
	walk = func(name string, ref uint64) {
		b := inodes[at(ref, inodeBlocks):]
		kind, perm := le.Uint16(b), fs.FileMode(le.Uint16(b[2:]))
		if le.Uint16(b[4:]) != 0 || le.Uint16(b[6:]) != 0 {
			t.Errorf("%s not owned by root", name)
		}
		b = b[16:]
		switch kind {
		case typeFile, typeLongFile:
			var start, size uint64
			if kind == typeFile {
				start, size = uint64(le.Uint32(b)), uint64(le.Uint32(b[12:]))
				if le.Uint32(b[4:]) != noFragment {
					t.Errorf("%s has a fragment", name)
				}
				b = b[16:]
			} else {
				start, size = le.Uint64(b), le.Uint64(b[8:])
				b = b[40:]
			}
			var data []byte
			p := start
			for range (size + blockSize - 1) / blockSize {
				s := le.Uint32(b)
				b = b[4:]
				block := img[p : p+uint64(s&^dataRaw)]
				p += uint64(s &^ dataRaw)
				if s&dataRaw == 0 {
					var err error
					if block, err = dec.DecodeAll(block, nil); err != nil {
						t.Fatal(err)
					}
				}
				data = append(data, block...)
			}
			if uint64(len(data)) != size {
				t.Errorf("%s: blocks of %d bytes, size %d", name, len(data), size)
			}
			out[name] = Entry{Kind: File, Perm: perm, Data: string(data)}
		case typeSymlink:
			n := le.Uint32(b[4:])
			out[name] = Entry{Kind: Symlink, Perm: perm, Target: string(b[8 : 8+n])}
		case typeDir, typeLongDir:
			var block, size, offset uint32
			if kind == typeDir {
				block, size, offset = le.Uint32(b), uint32(le.Uint16(b[8:])), uint32(le.Uint16(b[10:]))
			} else {
				size, block, offset = le.Uint32(b[4:]), le.Uint32(b[8:]), uint32(le.Uint16(b[18:]))
			}
			if name != "" {
				out[name] = Entry{Kind: Dir, Perm: perm}
			}
			l := dirs[at(uint64(block)<<16|uint64(offset), dirBlocks):]
			l = l[:size-3]
			last := ""
			for len(l) > 0 {
				count, iblock, base := le.Uint32(l)+1, le.Uint32(l[4:]), le.Uint32(l[8:])
				l = l[12:]
				for range count {
					off, delta := le.Uint16(l), int16(le.Uint16(l[2:]))
					n := int(le.Uint16(l[6:])) + 1
					child := string(l[8 : 8+n])
					l = l[8+n:]
					if child <= last {
						t.Errorf("%s: %q after %q", name, child, last)
					}
					last = child
					if want := le.Uint32(inodes[at(uint64(iblock)<<16|uint64(off), inodeBlocks)+12:]); uint32(int64(base)+int64(delta)) != want {
						t.Errorf("%s/%s: entry of inode %d, inode %d", name, child, int64(base)+int64(delta), want)
					}
					walk(strings.TrimPrefix(name+"/"+child, "/"), uint64(iblock)<<16|uint64(off))
				}
			}
		default:
			t.Fatalf("%s: inode type %d", name, kind)
		}
	}
	walk("", root)
	if !bytes.Equal(img[used:], make([]byte, len(img)-int(used))) {
		t.Error("the padding is not zeros")
	}
	return out
}

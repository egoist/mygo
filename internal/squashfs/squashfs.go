// Package squashfs writes SquashFS 4.0 images compressed with zstd, the
// file system an AppImage carries after its runtime: directories, regular
// files and symbolic links, all owned by root, without fragments, extended
// attributes or an export table, which an AppImage's runtime (squashfuse)
// and the kernel do without.
//
// The layout is the one mksquashfs writes: the superblock, the files'
// data blocks, then the inode table, the directory table and the table of
// owner IDs, each table a run of metadata blocks of at most 8 KiB before
// compression. Inodes are written after the files they describe and a
// directory's after its entries, so that each refers only to what is
// already written; the root comes last.
package squashfs

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

const (
	magic     = 0x73717368 // "hsqs"
	blockSize = 128 << 10  // mksquashfs's default
	blockLog  = 17
	metaSize  = 8192
	zstdID    = 6

	flagNoFragments = 0x0010
	flagNoXattrs    = 0x0200

	// A data block's size has this bit set when the block is stored as it
	// is, which it is when compressing does not make it smaller; a metadata
	// block's header has metaRaw set for the same reason.
	dataRaw = 1 << 24
	metaRaw = 0x8000

	noFragment = 0xFFFFFFFF
	noXattr    = 0xFFFFFFFF
	noTable    = ^uint64(0)

	superblockSize = 96
)

// Inode types; a directory entry names the basic type of its inode
// whichever it is.
const (
	typeDir      = 1
	typeFile     = 2
	typeSymlink  = 3
	typeLongDir  = 8
	typeLongFile = 9
)

// Writer writes an image to an io.WriteSeeker, from where it is positioned
// when NewWriter is called: after an AppImage's runtime, for one. Files'
// data is written as they are added; Close writes the tables and the
// superblock.
type Writer struct {
	w     io.WriteSeeker
	start int64 // where the image starts in w
	pos   int64 // bytes of the image written
	mtime uint32
	enc   *zstd.Encoder
	root  *node
	block []byte
	err   error
}

// node is a directory, a file or a link of the image.
type node struct {
	kind     uint16 // typeDir, typeFile or typeSymlink
	perm     fs.FileMode
	children map[string]*node // of a directory
	// A file's data: where its blocks start in the image, its size and the
	// stored size of each block.
	start  uint64
	size   uint64
	blocks []uint32
	target string // a link's

	number uint32 // the inode's number
	ref    uint64 // where the inode is: its metadata block << 16 | offset
}

// NewWriter starts an image at w's position; mtime is the time of every
// entry.
func NewWriter(w io.WriteSeeker, mtime time.Time) (*Writer, error) {
	start, err := w.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBestCompression), zstd.WithEncoderConcurrency(1))
	if err != nil {
		return nil, err
	}
	sw := &Writer{
		w:     w,
		start: start,
		mtime: uint32(max(mtime.Unix(), 0)),
		enc:   enc,
		root:  &node{kind: typeDir, perm: 0o755, children: map[string]*node{}},
	}
	// The superblock, written last, once its offsets are known.
	sw.write(make([]byte, superblockSize))
	return sw, sw.err
}

func (w *Writer) write(p []byte) {
	if w.err != nil {
		return
	}
	n, err := w.w.Write(p)
	w.pos += int64(n)
	w.err = err
}

// Mkdir adds the directory name, a slash-separated path, and the
// directories above it that are not there yet.
func (w *Writer) Mkdir(name string, perm fs.FileMode) error {
	if name == "." {
		w.root.perm = perm.Perm()
		return nil
	}
	n, err := w.add(name, typeDir)
	if err != nil {
		return err
	}
	n.perm = perm.Perm()
	if n.children == nil {
		n.children = map[string]*node{}
	}
	return nil
}

// Create adds the file name with the contents of r.
func (w *Writer) Create(name string, perm fs.FileMode, r io.Reader) error {
	n, err := w.add(name, typeFile)
	if err != nil {
		return err
	}
	n.perm, n.start = perm.Perm(), uint64(w.pos)
	if w.block == nil {
		w.block = make([]byte, blockSize)
	}
	for {
		k, rerr := io.ReadFull(r, w.block)
		if k > 0 {
			data := w.enc.EncodeAll(w.block[:k], nil)
			stored := uint32(len(data))
			if len(data) >= k {
				data, stored = w.block[:k], uint32(k)|dataRaw
			}
			w.write(data)
			n.blocks = append(n.blocks, stored)
			n.size += uint64(k)
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			return rerr
		}
		if w.err != nil {
			return w.err
		}
	}
	return w.err
}

// Symlink adds the link name to target.
func (w *Writer) Symlink(target, name string) error {
	if target == "" {
		return fmt.Errorf("squashfs: link %s has no target", name)
	}
	n, err := w.add(name, typeSymlink)
	if err != nil {
		return err
	}
	n.perm, n.target = 0o777, target
	return nil
}

// add adds a new entry of kind at name, with the directories above it.
func (w *Writer) add(name string, kind uint16) (*node, error) {
	if w.err != nil {
		return nil, w.err
	}
	if !fs.ValidPath(name) || name == "." {
		return nil, fmt.Errorf("squashfs: invalid path %q", name)
	}
	dir := w.root
	parts := strings.Split(name, "/")
	for _, p := range parts[:len(parts)-1] {
		next := dir.children[p]
		if next == nil {
			next = &node{kind: typeDir, perm: 0o755, children: map[string]*node{}}
			dir.children[p] = next
		}
		if next.kind != typeDir {
			return nil, fmt.Errorf("squashfs: %s: %s is not a directory", name, p)
		}
		dir = next
	}
	base := parts[len(parts)-1]
	// SquashFS takes 256 bytes; Linux file systems, which an image is
	// unpacked to, 255.
	if len(base) > 255 {
		return nil, fmt.Errorf("squashfs: %s: name longer than 255 bytes", name)
	}
	if old := dir.children[base]; old != nil {
		// A directory made for the paths under it is made again by Mkdir
		// with its own mode.
		if kind == typeDir && old.kind == typeDir {
			return old, nil
		}
		return nil, fmt.Errorf("squashfs: %s added twice", name)
	}
	n := &node{kind: kind}
	dir.children[base] = n
	return n, nil
}

// Close writes the tables and the superblock, and leaves w at the end of
// the image, which it pads to a multiple of 4 KiB as mksquashfs does, so
// that it can be mounted from a loop device. It does not close w.
func (w *Writer) Close() error {
	defer w.enc.Close()
	if w.err != nil {
		return w.err
	}
	// Numbered as they will be written: children before their directory.
	count := uint32(0)
	var number func(n *node)
	number = func(n *node) {
		for _, name := range sortedNames(n) {
			if c := n.children[name]; c.kind == typeDir {
				number(c)
			} else {
				count++
				c.number = count
			}
		}
		count++
		n.number = count
	}
	number(w.root)

	inodes, dirs := newMeta(w.enc), newMeta(w.enc)
	var writeDir func(n *node, parent uint32) error
	writeDir = func(n *node, parent uint32) error {
		subdirs := uint32(0)
		names := sortedNames(n)
		for _, name := range names {
			c := n.children[name]
			switch c.kind {
			case typeDir:
				subdirs++
				if err := writeDir(c, n.number); err != nil {
					return err
				}
			case typeFile:
				c.ref = inodes.ref()
				inodes.write(w.fileInode(c))
			case typeSymlink:
				c.ref = inodes.ref()
				b := w.header(typeSymlink, c)
				b = binary.LittleEndian.AppendUint32(b, 1)
				b = binary.LittleEndian.AppendUint32(b, uint32(len(c.target)))
				inodes.write(append(b, c.target...))
			}
		}
		listing := dirs.ref()
		size := writeListing(dirs, n, names)
		n.ref = inodes.ref()
		inodes.write(w.dirInode(n, parent, subdirs, listing, size))
		return nil
	}
	if err := writeDir(w.root, count+1); err != nil {
		return err
	}

	inodeTable := uint64(w.pos)
	w.write(inodes.bytes())
	dirTable := uint64(w.pos)
	w.write(dirs.bytes())
	// No fragments: an empty table, which nothing reads.
	fragmentTable := uint64(w.pos)
	// One owner, root, for every entry.
	ids := newMeta(w.enc)
	ids.write(binary.LittleEndian.AppendUint32(nil, 0))
	idBlock := uint64(w.pos)
	w.write(ids.bytes())
	idTable := uint64(w.pos)
	w.write(binary.LittleEndian.AppendUint64(nil, idBlock))
	used := uint64(w.pos)
	if pad := (4096 - w.pos%4096) % 4096; pad > 0 {
		w.write(make([]byte, pad))
	}
	if w.err != nil {
		return w.err
	}
	end := w.start + w.pos

	sb := make([]byte, 0, superblockSize)
	le := binary.LittleEndian
	sb = le.AppendUint32(sb, magic)
	sb = le.AppendUint32(sb, count)
	sb = le.AppendUint32(sb, w.mtime)
	sb = le.AppendUint32(sb, blockSize)
	sb = le.AppendUint32(sb, 0) // fragments
	sb = le.AppendUint16(sb, zstdID)
	sb = le.AppendUint16(sb, blockLog)
	sb = le.AppendUint16(sb, flagNoFragments|flagNoXattrs)
	sb = le.AppendUint16(sb, 1) // IDs
	sb = le.AppendUint16(sb, 4)
	sb = le.AppendUint16(sb, 0)
	sb = le.AppendUint64(sb, w.root.ref)
	sb = le.AppendUint64(sb, used)
	sb = le.AppendUint64(sb, idTable)
	sb = le.AppendUint64(sb, noTable) // extended attributes
	sb = le.AppendUint64(sb, inodeTable)
	sb = le.AppendUint64(sb, dirTable)
	sb = le.AppendUint64(sb, fragmentTable)
	sb = le.AppendUint64(sb, noTable) // export
	if _, err := w.w.Seek(w.start, io.SeekStart); err != nil {
		return err
	}
	if _, err := w.w.Write(sb); err != nil {
		return err
	}
	_, err := w.w.Seek(end, io.SeekStart)
	return err
}

// header is the part of every inode before its type's own fields.
func (w *Writer) header(kind uint16, n *node) []byte {
	b := make([]byte, 0, 64)
	b = binary.LittleEndian.AppendUint16(b, kind)
	b = binary.LittleEndian.AppendUint16(b, uint16(n.perm))
	b = binary.LittleEndian.AppendUint16(b, 0) // owner: the first ID
	b = binary.LittleEndian.AppendUint16(b, 0) // group
	b = binary.LittleEndian.AppendUint32(b, w.mtime)
	return binary.LittleEndian.AppendUint32(b, n.number)
}

// fileInode is the inode of file n: the basic one while its data starts
// and ends within the first 4 GiB, the extended one beyond.
func (w *Writer) fileInode(n *node) []byte {
	le := binary.LittleEndian
	var b []byte
	if n.start+n.size <= 0xFFFFFFFF {
		b = w.header(typeFile, n)
		b = le.AppendUint32(b, uint32(n.start))
		b = le.AppendUint32(b, noFragment)
		b = le.AppendUint32(b, 0) // offset in the fragment
		b = le.AppendUint32(b, uint32(n.size))
	} else {
		b = w.header(typeLongFile, n)
		b = le.AppendUint64(b, n.start)
		b = le.AppendUint64(b, n.size)
		b = le.AppendUint64(b, 0) // sparse bytes
		b = le.AppendUint32(b, 1) // links
		b = le.AppendUint32(b, noFragment)
		b = le.AppendUint32(b, 0)
		b = le.AppendUint32(b, noXattr)
	}
	for _, s := range n.blocks {
		b = le.AppendUint32(b, s)
	}
	return b
}

// dirInode is the inode of directory n, whose listing of size bytes is at
// listing in the directory table: the basic one while the size fits its
// 16 bits, the extended one, without an index, beyond. The size counts 3
// bytes more than the listing, as the kernel expects.
func (w *Writer) dirInode(n *node, parent, subdirs uint32, listing uint64, size int) []byte {
	le := binary.LittleEndian
	block, offset := uint32(listing>>16), uint16(listing)
	var b []byte
	if size+3 <= 0xFFFF {
		b = w.header(typeDir, n)
		b = le.AppendUint32(b, block)
		b = le.AppendUint32(b, 2+subdirs)
		b = le.AppendUint16(b, uint16(size+3))
		b = le.AppendUint16(b, offset)
		b = le.AppendUint32(b, parent)
	} else {
		b = w.header(typeLongDir, n)
		b = le.AppendUint32(b, 2+subdirs)
		b = le.AppendUint32(b, uint32(size+3))
		b = le.AppendUint32(b, block)
		b = le.AppendUint32(b, parent)
		b = le.AppendUint16(b, 0) // index entries
		b = le.AppendUint16(b, offset)
		b = le.AppendUint32(b, noXattr)
	}
	return b
}

// writeListing writes the entries of directory n, of names in order, to
// the directory table and returns the bytes written. Entries go in runs
// under a header, which names the metadata block of their inodes and a
// base their numbers differ from by 16 bits: a new run starts when either
// would not do, or after 256 entries.
func writeListing(dirs *meta, n *node, names []string) int {
	le := binary.LittleEndian
	var out []byte
	for i := 0; i < len(names); {
		first := n.children[names[i]]
		block, base := uint32(first.ref>>16), first.number
		j := i
		for j < len(names) && j-i < 256 {
			c := n.children[names[j]]
			if uint32(c.ref>>16) != block || int64(c.number)-int64(base) > 32767 || int64(c.number)-int64(base) < -32768 {
				break
			}
			j++
		}
		out = le.AppendUint32(out, uint32(j-i-1))
		out = le.AppendUint32(out, block)
		out = le.AppendUint32(out, base)
		for _, name := range names[i:j] {
			c := n.children[name]
			out = le.AppendUint16(out, uint16(c.ref))
			out = le.AppendUint16(out, uint16(int16(int64(c.number)-int64(base))))
			out = le.AppendUint16(out, c.kind)
			out = le.AppendUint16(out, uint16(len(name)-1))
			out = append(out, name...)
		}
		i = j
	}
	dirs.write(out)
	return len(out)
}

func sortedNames(n *node) []string {
	names := make([]string, 0, len(n.children))
	for name := range n.children {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// meta is a table of metadata blocks: what is written to it is cut into
// blocks of 8 KiB, each compressed unless that makes it no smaller, after
// a 16-bit header with its stored size.
type meta struct {
	enc     *zstd.Encoder
	out     bytes.Buffer
	pending []byte
}

func newMeta(enc *zstd.Encoder) *meta { return &meta{enc: enc} }

// ref is where the next byte written goes: the offset of its block in the
// table, shifted 16 bits up, and its offset in the block.
func (m *meta) ref() uint64 {
	return uint64(m.out.Len())<<16 | uint64(len(m.pending))
}

func (m *meta) write(p []byte) {
	m.pending = append(m.pending, p...)
	for len(m.pending) >= metaSize {
		m.flush(m.pending[:metaSize])
		m.pending = append(m.pending[:0], m.pending[metaSize:]...)
	}
}

func (m *meta) flush(b []byte) {
	data := m.enc.EncodeAll(b, nil)
	header := uint16(len(data))
	if len(data) >= len(b) {
		data, header = b, uint16(len(b))|metaRaw
	}
	m.out.Write(binary.LittleEndian.AppendUint16(nil, header))
	m.out.Write(data)
}

// bytes ends the table and returns it.
func (m *meta) bytes() []byte {
	if len(m.pending) > 0 {
		m.flush(m.pending)
		m.pending = nil
	}
	return m.out.Bytes()
}

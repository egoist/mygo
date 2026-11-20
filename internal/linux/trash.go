//go:build linux && !android && (amd64 || arm64)

package linux

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// TrashItemResult moves path to the trash and returns where it landed.
// g_file_trash does not report the destination, so it is found by the
// freedesktop.org trash specification: the info file whose Path names the
// original, newest first when the same path was trashed more than once.
func (shell) TrashItemResult(path string) (string, error) {
	f := gFileNewForPath(cs(path))
	defer gObjectUnref(f)
	var gerr ptr
	if !gFileTrash(f, 0, &gerr) {
		return "", gErr(gerr)
	}
	if p, ok := findTrashed(path, trashDirs(path)); ok {
		return p, nil
	}
	return "", fmt.Errorf("mygo: trashed %s but cannot find it in the trash", path)
}

// trashDirs returns where the trash of path may be: the home trash, and
// the trash of its volume's top directory, which the specification puts
// in .Trash/<uid> or .Trash-<uid> there.
func trashDirs(path string) []string {
	dirs := []string{filepath.Join(xdgDir("XDG_DATA_HOME", ".local/share"), "Trash")}
	if top, ok := mountTop(path); ok && top != "/" {
		uid := strconv.Itoa(os.Getuid())
		dirs = append(dirs, filepath.Join(top, ".Trash", uid), filepath.Join(top, ".Trash-"+uid))
	}
	return dirs
}

// mountTop returns the mount point path lies on, the longest one that
// contains it, from the kernel's mount table.
func mountTop(path string) (string, bool) {
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", false
	}
	best := ""
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		mount := unescapeMount(fields[4])
		if mount == "/" || path == mount || strings.HasPrefix(path, mount+"/") {
			if len(mount) > len(best) {
				best = mount
			}
		}
	}
	return best, best != ""
}

// unescapeMount decodes the octal escapes (\040 and friends) the kernel
// writes for space, tab, newline and backslash in mount points.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// findTrashed returns the file in the files directory of one of dirs
// whose info file names path as its original location. The info file
// written last wins, so a path trashed twice returns its newest item.
func findTrashed(path string, dirs []string) (string, bool) {
	best := ""
	var bestTime int64
	for _, dir := range dirs {
		infos, err := os.ReadDir(filepath.Join(dir, "info"))
		if err != nil {
			continue
		}
		for _, e := range infos {
			name := e.Name()
			if !strings.HasSuffix(name, ".trashinfo") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, "info", name))
			if err != nil {
				continue
			}
			if orig, ok := trashInfoPath(b); !ok || orig != path {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if when := info.ModTime().UnixNano(); best == "" || when > bestTime {
				best = filepath.Join(dir, "files", strings.TrimSuffix(name, ".trashinfo"))
				bestTime = when
			}
		}
	}
	return best, best != ""
}

// trashInfoPath reads the Path of a .trashinfo file, which the
// specification percent-encodes; GIO writes it absolute.
func trashInfoPath(b []byte) (string, bool) {
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "Path="); ok {
			p, err := url.PathUnescape(v)
			return p, err == nil
		}
	}
	return "", false
}
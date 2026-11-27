//go:build unix

package mygo

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// singleInstanceSocket returns the socket path of the app named name. It
// is in a folder only the user can reach, made if missing: "mygo" in
// XDG_RUNTIME_DIR when that folder is the user's own as it should be,
// else "mygo-<uid>" in the temporary folder, then in /tmp. In a shared
// folder such as /tmp itself another user could listen on the socket
// first and take the app's command lines, or hold its lock. Unix socket
// paths are limited to about 100 bytes.
func singleInstanceSocket(name string) (string, error) {
	file := singleInstanceHash(name) + ".sock"
	uid := os.Getuid()
	own := "mygo-" + strconv.Itoa(uid)
	var dirs []string
	if runtime := privateRuntimeDir(uid); runtime != "" {
		if d := filepath.Join(runtime, "mygo"); len(filepath.Join(d, file)) < 100 {
			dirs = append(dirs, d)
		}
	}
	temp := filepath.Join(os.TempDir(), own)
	if len(filepath.Join(temp, file)) < 100 {
		dirs = append(dirs, temp)
	}
	if tmp := filepath.Join("/tmp", own); tmp != temp {
		dirs = append(dirs, tmp)
	}
	var errs []error
	for _, dir := range dirs {
		err := privateFolder(dir, uid)
		if err == nil {
			return filepath.Join(dir, file), nil
		}
		errs = append(errs, err)
	}
	return "", errors.Join(errs...)
}

// privateRuntimeDir returns XDG_RUNTIME_DIR if the user uid owns it and no
// other user can reach it, as the XDG Base Directory Specification asks,
// else "". It is not, for one, under sudo -E as root.
func privateRuntimeDir(uid int) string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return ""
	}
	info, err := os.Stat(dir)
	if err != nil || checkPrivate(dir, info, uid) != nil {
		return ""
	}
	return dir
}

// privateFolder makes the folder at path with mode 0700 if it is missing,
// and checks that it is not a link and only the user uid can reach it,
// nor rename it: its parent must be root's or the user's, and sticky if
// others can write to it.
func privateFolder(path string, uid int) error {
	parent := filepath.Dir(path)
	info, err := os.Stat(parent)
	if err != nil {
		return err
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !ok || st.Uid != 0 && int(st.Uid) != uid {
		return fmt.Errorf("%s belongs to another user", parent)
	}
	if info.Mode().Perm()&0o022 != 0 && info.Mode()&fs.ModeSticky == 0 {
		return fmt.Errorf("%s has mode %#o: other users can rename what is in it", parent, info.Mode().Perm())
	}
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	info, err = os.Lstat(path)
	if err != nil {
		return err
	}
	return checkPrivate(path, info, uid)
}

// checkPrivate checks that info, of the file at path, is a folder that the
// user uid owns and no other user can reach.
func checkPrivate(path string, info fs.FileInfo, uid int) error {
	if !info.IsDir() {
		return fmt.Errorf("%s is not a folder", path)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != uid {
		return fmt.Errorf("%s belongs to another user", path)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("%s has mode %#o: other users can reach it", path, perm)
	}
	return nil
}

// peerUser returns the user of the process at the other end of the Unix
// socket conn.
func peerUser(conn net.Conn) (int, error) {
	sc, ok := conn.(syscall.Conn)
	if !ok {
		return -1, fmt.Errorf("%T is not a socket", conn)
	}
	raw, err := sc.SyscallConn()
	if err != nil {
		return -1, err
	}
	var uid int
	var uidErr error
	if err := raw.Control(func(fd uintptr) { uid, uidErr = peerUID(int(fd)) }); err != nil {
		return -1, err
	}
	return uid, uidErr
}

//go:build !unix && !js

package mygo

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

// singleInstanceSocket returns a short per-user socket path, in the user's
// own temporary folder on Windows, which other users cannot reach; Unix
// socket paths are limited to about 100 bytes. Older MyGos used the same
// path: old is "".
func singleInstanceSocket(name string) (path, old string, err error) {
	file := "mygo-" + strconv.Itoa(os.Getuid()) + "-" + singleInstanceHash(name) + ".sock"
	for _, dir := range []string{os.Getenv("XDG_RUNTIME_DIR"), os.TempDir(), "/tmp"} {
		if dir != "" && len(filepath.Join(dir, file)) < 100 {
			return filepath.Join(dir, file), "", nil
		}
	}
	return filepath.Join("/tmp", file), "", nil
}

// peerUser would return the user of the process at the other end of
// conn: only the user reaches the socket.
func peerUser(net.Conn) (int, error) { return -1, errors.ErrUnsupported }

//go:build darwin && !ios

package darwin

import (
	"errors"
	"sync"

	"github.com/ebitengine/purego"
)

// getpeereid is libSystem's getpeereid(3), which the syscall package
// leaves out, found with the first use.
var (
	getpeereidOnce sync.Once
	getpeereid     func(fd int32, uid, gid *uint32) int32
)

// PeerUID returns the user of the process at the other end of the Unix
// socket fd.
func PeerUID(fd int) (int, error) {
	getpeereidOnce.Do(func() {
		if p, err := purego.Dlsym(purego.RTLD_DEFAULT, "getpeereid"); err == nil {
			purego.RegisterFunc(&getpeereid, p)
		}
	})
	if getpeereid == nil {
		return -1, errors.New("getpeereid is missing")
	}
	var uid, gid uint32
	if getpeereid(int32(fd), &uid, &gid) != 0 {
		return -1, errors.New("getpeereid failed")
	}
	return int(uid), nil
}

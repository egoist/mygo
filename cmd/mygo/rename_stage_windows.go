//go:build windows

package main

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// renamePackageStage retries only the final commit of an already built package.
// Windows can report ACCESS_DENIED for an open child of a directory. The same
// code can mean a permanent ACL denial, so retries are limited and preserve the
// last os.Rename error. The old destination is still managed by replacePath.
func renamePackageStage(from, to string) error {
	return retryPackageStageRename(func() error { return os.Rename(from, to) }, time.Sleep)
}

// Ten attempts schedule at most 900 ms of waiting, in addition to syscall and
// scheduler time. No earlier replacePath phase or rollback is retried here.
func retryPackageStageRename(rename func() error, pause func(time.Duration)) error {
	for attempt := 1; ; attempt++ {
		err := rename()
		if err == nil || attempt == 10 ||
			!errors.Is(err, syscall.Errno(5)) && !errors.Is(err, syscall.Errno(32)) {
			return err
		}
		pause(100 * time.Millisecond)
	}
}

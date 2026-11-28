//go:build !(darwin && !ios) && !(linux && !android && (amd64 || arm64)) && !(windows && (amd64 || arm64))

package mygo

import "github.com/egoist/mygo/internal/platform"

func supportsSessionProxy() bool { return false }
func openProxyStore(platform.AppController, platform.DataStoreOptions) (platform.DataStore, error) {
	return nil, platform.ErrUnsupported
}

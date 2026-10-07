package unsupported

import "github.com/egoist/mygo/internal/platform"

func (*Backend) NewTextChecker(string) (platform.TextChecker, error) {
	return nil, platform.ErrUnsupported
}

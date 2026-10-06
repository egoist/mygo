package fake

import "github.com/egoist/mygo/internal/platform"

// TextCheckerFactory supplies deterministic checking sessions for core tests.
// The default fake has no text service and reports ErrUnsupported.
func (b *Backend) NewTextChecker(language string) (platform.TextChecker, error) {
	if b.TextCheckerFactory != nil {
		return b.TextCheckerFactory(language)
	}
	return nil, platform.ErrUnsupported
}

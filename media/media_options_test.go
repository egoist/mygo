//go:build darwin || (windows && (amd64 || arm64)) || (linux && (amd64 || arm64))

package media

import (
	"context"
	"errors"
	"testing"
)

// TestASourceWithoutPicturesHasNone checks that WithVideo(false) means the
// same thing on every platform: a source that is not asked for pictures has
// none to give, rather than decoding pictures for nobody — which on Linux
// would hold the file back for the sound as well.
func TestASourceWithoutPicturesHasNone(t *testing.T) {
	s, err := openSource(clipPath, options{noVideo: true})
	if err != nil {
		t.Fatalf("openSource(%q): %v", clipPath, err)
	}
	defer s.Close()
	if _, err := s.NextFrame(context.Background()); !errors.Is(err, ErrEnd) {
		t.Errorf("a source with its pictures turned off gave %v, want the end of the source", err)
	}
	if s.(soundSource).Sound() == nil {
		t.Error("turning the pictures off gave up the sound too")
	}
}

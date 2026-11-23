package mobile

import "github.com/egoist/mygo/internal/platform"

// MediaControls is installed by the backgroundaudio plugin.
var MediaControls func(platform.AppHandler, platform.PluginBackend) platform.NowPlaying

func (b *Backend) NowPlaying() platform.NowPlaying {
	if MediaControls != nil {
		return MediaControls(b.h, b)
	}
	return b.Backend.NowPlaying()
}

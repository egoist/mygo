// Package backgroundaudio brings system media controls and background playback
// to NowPlaying on phones. Importing it opts the app into the native hosts.
package backgroundaudio

import (
	"encoding/json"

	"github.com/egoist/mygo"
)

// Event reports an interruption, focus change or change of output.
// Kind is interrupted, resume, focusLoss, focusTransient, duck, focusGain,
// outputChanged or outputRemoved. ResumeAllowed is iOS's resume hint.
type Event struct {
	Kind          string
	ResumeAllowed bool
}

// OnEvent calls fn on the main thread. The app owns its playback policy.
func OnEvent(fn func(Event)) func() {
	return mygo.Native("backgroundaudio").On("lifecycle", func(data []byte) {
		var e Event
		if json.Unmarshal(data, &e) == nil {
			fn(e)
		}
	})
}

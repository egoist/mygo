//go:build ios || android

package backgroundaudio

import (
	"encoding/json"
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/mobile"
	"github.com/egoist/mygo/internal/platform"
)

type controls struct{ host platform.PluginBackend }

func init() {
	p := mygo.Native("backgroundaudio")
	var listening bool
	mobile.MediaControls = func(h platform.AppHandler, host platform.PluginBackend) platform.NowPlaying {
		if !listening {
			listening = true
			p.On("command", func(data []byte) {
				var c struct {
					Command      platform.MediaCommand
					Milliseconds int64
				}
				if json.Unmarshal(data, &c) == nil {
					h.MediaCommand(c.Command, time.Duration(c.Milliseconds)*time.Millisecond)
				}
			})
		}
		return controls{host}
	}
}

func (c controls) Set(info *platform.NowPlayingInfo) {
	method := "set"
	if info == nil {
		method = "clear"
	}
	args := []byte("{}")
	if info != nil {
		var err error
		args, err = json.Marshal(info)
		if err != nil {
			log.Printf("background audio: %v", err)
			return
		}
	}
	c.host.PluginCall("backgroundaudio", method, args, func(_ []byte, err error) {
		if err != nil {
			log.Printf("background audio: %v", err)
		}
	})
}

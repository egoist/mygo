// Background-audio plays a looping local tone and accepts lock-screen commands.
package main

import (
	_ "embed"
	"log"
	"math"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/plugins/backgroundaudio"
	"github.com/egoist/mygo/ui"
)

//go:embed cover.png
var cover []byte

func main() {
	const rate = 24000
	samples := make([]float32, rate*8)
	for i := range samples {
		samples[i] = float32(0.08 * math.Sin(2*math.Pi*220*float64(i)/rate))
	}
	player := audio.NewSound(rate, 1, samples).NewPlayer().SetLoop(true)
	defer player.Close()
	defer mygo.NowPlaying.Control(player)()
	backgroundaudio.OnEvent(func(e backgroundaudio.Event) {
		log.Printf("audio: %+v", e)
		switch e.Kind {
		case "interrupted", "focusLoss", "focusTransient", "outputRemoved":
			player.Pause()
		case "duck":
			player.SetVolume(0.2)
		case "focusGain":
			player.SetVolume(1)
		default:
			return
		}
		mygo.NowPlaying.Sync()
	})
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{Title: "Background Audio", Content: ui.View(func(c *ui.Context) {
			ui.Column(c).Fill().Center().Children(func() {
				if ui.Button(c, "Play tone").Clicked() {
					player.SetVolume(1)
					player.Play()
					mygo.NowPlaying.Set(mygo.NowPlayingInfo{Title: "Local tone", Artist: "MyGo", Artwork: cover})
				}
				if ui.Button(c, "Stop and clear").Clicked() {
					player.Stop()
					mygo.NowPlaying.Clear()
				}
			})
		})})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

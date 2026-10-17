// Video-player plays a file in native UI: MyGo reads the pictures and the
// sound itself — the platform decodes, MyGo owns the clock — and draws them
// into a window of its own, with no web page and no video element anywhere.
//
//	go run ./examples/video-player clip.mp4
//
// Space plays and pauses, the left and right arrows seek, and F switches
// between showing the whole picture and filling the window with it.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/media"
	"github.com/egoist/mygo/media/mediaui"
	"github.com/egoist/mygo/ui"
)

// seekStep is how far the arrow keys move through the file.
const seekStep = 5 * time.Second

type videoPlayer struct {
	path  string
	win   *mygo.Window
	med   *media.Player
	video *mediaui.Video
	fit   ui.Fit
}

// view builds the interface from the player's state, on the main thread,
// whenever the window needs a frame.
func (a *videoPlayer) view(c *ui.Context) {
	if a.video == nil {
		// The window can ask for its first frame before the video
		// exists: the window is made, then the video that draws into it.
		return
	}
	t := c.Theme()
	if c.Shortcut(0, ui.KeySpace) {
		if a.med.Playing() {
			a.med.Pause()
		} else {
			a.med.Play()
		}
	}
	if c.Shortcut(0, ui.KeyLeft) {
		a.seek(-seekStep)
	}
	if c.Shortcut(0, ui.KeyRight) {
		a.seek(seekStep)
	}
	if c.Shortcut(0, ui.KeyF) {
		if a.fit == ui.Contain {
			a.fit = ui.Cover
		} else {
			a.fit = ui.Contain
		}
		a.video.SetFit(a.fit)
	}
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Grow(1).MinHeight(0).Padding(8).Children(func() {
			a.video.Element(c).Fill()
		})
		a.status(c, t)
	})
}

// status is the bar under the picture: where playback has reached, and what
// happened when it stopped.
func (a *videoPlayer) status(c *ui.Context, t *ui.Theme) {
	ui.Row(c).Padding(8, 12).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, clock(a.med.Position())).FontSize(12)
		ui.Text(c, clock(a.med.Duration())).FontSize(12).TextColor(t.TextMuted)
		switch err := a.video.Err(); {
		case err == nil:
			if !a.med.Playing() {
				ui.Text(c, "paused").FontSize(12).TextColor(t.TextMuted)
			}
		case err == media.ErrEnd:
			ui.Text(c, "the end").FontSize(12).TextColor(t.TextMuted)
		default:
			ui.Text(c, err.Error()).FontSize(12).TextColor(t.Danger)
		}
		ui.Text(c, "Space play/pause · ← → seek · F fit").FontSize(12).TextColor(t.TextMuted)
	})
}

// seek moves through the file, clamped to it.
func (a *videoPlayer) seek(d time.Duration) {
	at := a.med.Position() + d
	if at < 0 {
		at = 0
	}
	if d := a.med.Duration(); d > 0 && at > d {
		at = d
	}
	a.med.Seek(at)
}

// clock writes a length of media as minutes and seconds.
func clock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d / time.Second)
	if h := s / 3600; h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: %s [-audio=false] file\n", os.Args[0])
		flag.PrintDefaults()
	}
	audio := flag.Bool("audio", true, "play the file's sound")
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	path := flag.Arg(0)
	med, err := media.Open(path, media.WithAudio(*audio))
	if err != nil {
		log.Fatal(err)
	}
	a := &videoPlayer{path: path, med: med, fit: ui.Contain}
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:  fmt.Sprintf("%s — %s", clock(med.Duration()), path),
			Width:  960,
			Height: 600,
			Content: ui.View(func(c *ui.Context) {
				a.view(c)
			}),
		})
		a.video = mediaui.NewVideo(a.win, med)
		a.video.Start()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

package video

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"time"
)

// View shows a video surface with playback, seek, volume, mute and speed
// controls. Space toggles playback, and left/right seek five seconds. libmpv
// draws subtitles and letterboxing into the surface; MyGo clips it normally.
func View(c *ui.Context, p *Player) *ui.Element {
	p.mu.Lock()
	if p.state.Phase != Closed {
		p.invalidate = c.Invalidate
	}
	s := p.state
	p.mu.Unlock()
	root := ui.Column(c).Gap(8)
	root.Children(func() {
		e := ui.Box(c).Grow(1).MinHeight(80).Focusable().Clip().Label("Video").Background(ui.RGB(0, 0, 0))
		b := e.Bounds()
		if b.W > 0 && b.H > 0 {
			p.resize(int(b.W*2), int(b.H*2))
		}
		e.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputKeyDown {
				return false
			}
			s := p.State()
			switch ev.Key {
			case ui.KeySpace:
				if s.Phase == Playing {
					_ = p.Pause()
				} else {
					_ = p.Play()
				}
			case ui.KeyLeft:
				_ = p.Seek(max(0, s.Position-5*time.Second))
			case ui.KeyRight:
				_ = p.Seek(s.Position + 5*time.Second)
			default:
				return false
			}
			return true
		})
		e.Draw(func(paint *ui.Painter, r ui.Rect) {
			p.mu.Lock()
			bitmap, state := p.bitmap, p.state
			p.mu.Unlock()
			if bitmap != nil {
				paint.Image(bitmap, r, ui.FillBox)
			}
			if state.Err != nil {
				paint.Text(r.X+12, r.Y+12, state.Err.Error(), 13, ui.RGB(255, 180, 180))
			} else if bitmap == nil {
				paint.Text(r.X+12, r.Y+12, string(state.Phase), 13, ui.RGB(220, 220, 220))
			}
		})
		ui.Row(c).Gap(8).Children(func() {
			disabled := s.Phase == Closed || s.Phase == Idle || s.Phase == Failed
			label := "Play"
			if s.Phase == Playing {
				label = "Pause"
			}
			if ui.Button(c, label).Disabled(disabled).Clicked() {
				if s.Phase == Playing {
					_ = p.Pause()
				} else {
					_ = p.Play()
				}
			}
			if ui.Button(c, "Stop").Disabled(s.Phase == Closed || s.Phase == Idle).Clicked() {
				_ = p.Stop()
			}
			ui.Text(c, fmt.Sprintf("%s / %s", clock(s.Position), clock(s.Duration)))
			position := s.Position.Seconds()
			if ui.Slider(c, &position, 0, max(1, s.Duration.Seconds())).Label("Video position").Grow(1).Disabled(!s.Seekable || s.Phase == Closed).Changed() {
				_ = p.Seek(time.Duration(position * 1e9))
			}
			mute := "Mute"
			if s.Muted {
				mute = "Unmute"
			}
			if ui.Button(c, mute).Disabled(s.Phase == Closed).Clicked() {
				_ = p.SetMuted(!s.Muted)
			}
			volume := s.Volume
			if ui.Slider(c, &volume, 0, 100).Label("Video volume").Width(90).Disabled(s.Phase == Closed).Changed() {
				_ = p.SetVolume(volume)
			}
			if ui.Button(c, fmt.Sprintf("%.2g×", s.Rate)).Label("Playback speed").Disabled(s.Phase == Closed).Clicked() {
				rates := []float64{0.5, 1, 1.5, 2}
				next := 0.5
				for _, rate := range rates {
					if rate > s.Rate {
						next = rate
						break
					}
				}
				_ = p.SetRate(next)
			}
		})
		ui.Text(c, string(s.Phase)).Label("Playback state").FontSize(12)
	})
	return root
}
func clock(d time.Duration) string {
	sec := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", sec/60, sec%60)
}

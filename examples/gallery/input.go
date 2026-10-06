package main

import (
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"
)

type pointerDemo struct {
	contacts              map[uint64]ui.InputEvent
	scale, rotation, x, y float32
	drawing               bool
	last                  string
}

func (g *gallery) inputPage(c *ui.Context) {
	d := &g.pointer
	if d.contacts == nil {
		d.contacts = make(map[uint64]ui.InputEvent)
		d.scale = 1
	}
	t := c.Theme()
	ui.Text(c, "Pinch, rotate, or pan on the pad. A mouse drag also pans. Touch outside the pad scrolls the page.").TextColor(t.TextMuted)
	if ui.Checkbox(c, &d.drawing, "Capture contacts for drawing").Changed() {
		clear(d.contacts)
	}
	ui.Text(c, "Drawing captures each contact and prevents gestures and touch scrolling. Pen pressure changes the marker size; tilt and device availability appear below.").TextColor(t.TextMuted)
	pad := ui.Box(c).Key("pointer-pad").Height(320).WidthPercent(100).Radius(12).Background(t.Surface).Border(1, t.Border).Clip()
	pad.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputPointerDown:
			if d.drawing {
				pad.CapturePointer(ev.Pointer.ID)
			}
			d.contacts[ev.Pointer.ID] = ev
		case ui.InputPointerMove:
			if prev, ok := d.contacts[ev.Pointer.ID]; ok {
				if ev.Pointer.Device == ui.PointerMouse && !d.drawing {
					d.x += ev.X - prev.X
					d.y += ev.Y - prev.Y
				}
				d.contacts[ev.Pointer.ID] = ev
			}
		case ui.InputPointerUp, ui.InputPointerCancel:
			delete(d.contacts, ev.Pointer.ID)
		default:
			return false
		}
		devices := []string{"mouse", "touch", "pen", "touchpad"}
		d.last = fmt.Sprintf("%s #%d · contact %t · pressure %.2f (available %t) · tilt %.0f / %.0f° (available %t)", devices[ev.Pointer.Device], ev.Pointer.ID, ev.Pointer.Contact, ev.Pointer.Pressure, ev.Pointer.HasPressure, ev.Pointer.TiltX, ev.Pointer.TiltY, ev.Pointer.HasTilt)
		return true
	})
	pad.Gestures(ui.GesturePan|ui.GesturePinch|ui.GestureRotation, func(ev ui.GestureEvent) bool {
		if d.drawing {
			return false
		}
		if ev.Phase == ui.GestureUpdate {
			d.x += ev.DX
			d.y += ev.DY
			d.scale = max(.1, min(4, d.scale*ev.Scale))
			d.rotation += ev.Rotation
		}
		d.last = fmt.Sprintf("gesture phase %d · contacts %d · delta %.1f / %.1f · scale ×%.3f · rotation %.1f°", ev.Phase, ev.Contacts, ev.DX, ev.DY, ev.Scale, ev.Rotation*180/math.Pi)
		return true
	})
	pad.Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2+d.x, r.Y+r.H/2+d.y
		var path ui.Path
		for i, v := range [][2]float32{{-65, -40}, {65, -40}, {65, 40}, {-65, 40}, {-65, -40}} {
			sin, cos := float32(math.Sin(float64(d.rotation))), float32(math.Cos(float64(d.rotation)))
			x, y := cx+(v[0]*cos-v[1]*sin)*d.scale, cy+(v[0]*sin+v[1]*cos)*d.scale
			if i == 0 {
				path.MoveTo(x, y)
			} else {
				path.LineTo(x, y)
			}
		}
		p.StrokePath(&path, 3, t.Accent)
		for _, ev := range d.contacts {
			x, y := r.X+ev.X, r.Y+ev.Y
			if ev.Pointer.Device == ui.PointerTouchpad {
				x = r.X + ev.Pointer.NormalizedX*r.W
				y = r.Y + ev.Pointer.NormalizedY*r.H
			}
			radius := float32(7)
			if ev.Pointer.HasPressure {
				radius += 15 * ev.Pointer.Pressure
			}
			var dot ui.Path
			dot.Circle(x, y, radius)
			p.FillPath(&dot, t.Accent)
		}
	})
	ui.Text(c, fmt.Sprintf("Zoom %.0f%% · rotation %.0f° · pan %.0f / %.0f", d.scale*100, d.rotation*180/math.Pi, d.x, d.y))
	ui.Text(c, d.last).TextColor(t.TextMuted)
	if ui.Button(c, "Reset pad").Clicked() {
		d.scale, d.rotation, d.x, d.y = 1, 0, 0, 0
		clear(d.contacts)
	}
	ui.Text(c, "macOS: indirect trackpad contacts plus native gestures and tablet data. Linux: touch, tablet axes and trackpad gestures depend on GDK, compositor and hardware. Windows: touch/pen contacts; precision touchpads keep wheel scrolling.").TextColor(t.TextMuted)
}

package main

import "testing"

// TestPressDIPs holds the Windows press transform still: a press is reported
// in the DIPs of the frame mygo placed the control's whole window at, which a
// border puts outside the control's client area, at the DPI the window has
// now. None of it needs a Windows runtime: the arithmetic under test is pure,
// so this runs on any host, as the sibling coords.go and coords_window.go
// tests do.
func TestPressDIPs(t *testing.T) {
	tests := []struct {
		name         string
		client       point
		clientOrigin point
		frameOrigin  point
		scale        float64
		want         point
		ok           bool
	}{
		{
			name: "one device pixel of border at 100%",
			// The client area begins one pixel right and down of the frame,
			// as it does for an EDIT with WS_BORDER.
			client:       point{X: 5, Y: 7},
			clientOrigin: point{X: 101, Y: 202},
			frameOrigin:  point{X: 100, Y: 200},
			scale:        1,
			want:         point{X: 6, Y: 9},
			ok:           true,
		},
		{
			name:         "no border: the client area is the frame",
			client:       point{X: 5, Y: 7},
			clientOrigin: point{X: 100, Y: 200},
			frameOrigin:  point{X: 100, Y: 200},
			scale:        1,
			want:         point{X: 5, Y: 7},
			ok:           true,
		},
		{
			name:         "150%: a border of one device pixel is not one DIP",
			client:       point{X: 42, Y: 56},
			clientOrigin: point{X: 103, Y: 104},
			frameOrigin:  point{X: 100, Y: 100},
			scale:        1.5,
			want:         point{X: 30, Y: 40},
			ok:           true,
		},
		{
			name:         "200%: two device pixels of border are one DIP",
			client:       point{X: 30, Y: 60},
			clientOrigin: point{X: 202, Y: 204},
			frameOrigin:  point{X: 200, Y: 200},
			scale:        2,
			want:         point{X: 16, Y: 32},
			ok:           true,
		},
		{
			name: "125% with the frame on a display left of the primary one",
			// Screen pixels here are negative: the frame's origin is not the
			// origin of anything, so the arithmetic never assumes it is.
			client:       point{X: 8, Y: 8},
			clientOrigin: point{X: -1918, Y: -1078},
			frameOrigin:  point{X: -1920, Y: -1080},
			scale:        1.25,
			want:         point{X: 8, Y: 8},
			ok:           true,
		},
		{
			name:         "a scale that cannot describe a DIP reports nothing",
			client:       point{X: 5, Y: 7},
			clientOrigin: point{X: 101, Y: 202},
			frameOrigin:  point{X: 100, Y: 200},
			scale:        0,
			want:         point{},
			ok:           false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := pressDIPs(tt.client, tt.clientOrigin, tt.frameOrigin, tt.scale)
			if ok != tt.ok {
				t.Fatalf("pressDIPs reported ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("pressDIPs = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestPressDIPsKeepsThePointInTheFrame is the regression the transform was
// wrong for: with a border, the frame's point and the client area's point are
// not the same point, and the difference is the border the window measured,
// not a width this example guessed.
func TestPressDIPsKeepsThePointInTheFrame(t *testing.T) {
	frameOrigin := point{X: 100, Y: 100}
	client := point{X: 20, Y: 30}
	onePixel := point{X: 101, Y: 101}
	threePixels := point{X: 103, Y: 103}
	for _, scale := range []float64{1, 1.25, 1.5, 2} {
		at, ok := pressDIPs(client, onePixel, frameOrigin, scale)
		if !ok {
			t.Fatalf("scale %v: pressDIPs reported nothing", scale)
		}
		// The point of the client area alone would be short of the frame by
		// the border, which is what the old arithmetic reported.
		clientOnly := point{X: client.X / scale, Y: client.Y / scale}
		if at.X <= clientOnly.X || at.Y <= clientOnly.Y {
			t.Errorf("scale %v: pressDIPs = %+v, not inside the frame of the client point %+v", scale, at, clientOnly)
		}
		want := point{X: (1 + client.X) / scale, Y: (1 + client.Y) / scale}
		if at != want {
			t.Errorf("scale %v: pressDIPs = %+v, want %+v", scale, at, want)
		}
		// A wider border moves the point further still: the width is the
		// window's answer, measured at the event.
		wider, ok := pressDIPs(client, threePixels, frameOrigin, scale)
		if !ok {
			t.Fatalf("scale %v: pressDIPs reported nothing for a wider border", scale)
		}
		if wider != (point{X: (3 + client.X) / scale, Y: (3 + client.Y) / scale}) {
			t.Errorf("scale %v: a wider border gave %+v, want the frame's own point", scale, wider)
		}
		if wider == at {
			t.Errorf("scale %v: two border widths gave the same point %+v", scale, at)
		}
	}
}

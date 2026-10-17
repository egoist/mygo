package main

import "testing"

// Tests of coords.go's arithmetic, which run on any host.

// PointerDown asks for DIPs from the top-left of the view MyGo hosts, whatever
// the platform's own coordinates are: the bounds origin (a scroll view's scroll
// offset) comes off, and a view that is not flipped counts Y upward from the
// bottom of its bounds.
func TestLocalDIPs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		p       point
		b       bounds
		flipped bool
		want    point
	}{
		{"flipped", point{X: 12, Y: 34}, bounds{Width: 320, Height: 160}, true, point{X: 12, Y: 34}},
		{"unflipped", point{X: 12, Y: 34}, bounds{Width: 320, Height: 160}, false, point{X: 12, Y: 126}},
		{"unflipped top left", point{X: 0, Y: 160}, bounds{Width: 320, Height: 160}, false, point{X: 0, Y: 0}},
		{"flipped, scrolled", point{X: 12, Y: 234}, bounds{Y: 200, Width: 320, Height: 160}, true, point{X: 12, Y: 34}},
		{"unflipped, scrolled", point{X: 14, Y: 190}, bounds{X: 4, Y: 50, Width: 320, Height: 160}, false, point{X: 10, Y: 20}},
		{"unflipped, scrolled to its top left", point{X: 4, Y: 210}, bounds{X: 4, Y: 50, Width: 320, Height: 160}, false, point{X: 0, Y: 0}},
	} {
		if got := localDIPs(tc.p, tc.b, tc.flipped); got != tc.want {
			t.Errorf("%s: localDIPs(%+v, %+v, %v) = %+v, want %+v", tc.name, tc.p, tc.b, tc.flipped, got, tc.want)
		}
	}
}

// The point a press reports is where it is in the part of the view the user
// sees, so scrolling the content must not move it: without this, a press
// drifts by the scroll offset once the document is scrolled, which is exactly
// what converting into a scrolled view's content coordinates does.
func TestLocalDIPsIgnoresScrolling(t *testing.T) {
	for _, flipped := range []bool{true, false} {
		// The visible part's top-left in the view's own coordinates, and a
		// point 20 DIPs below it.
		inView := func(scroll, down float64) point {
			y := scroll + down
			if !flipped {
				y = scroll + 160 - down
			}
			return point{X: 3, Y: y}
		}
		for _, scroll := range []float64{0, 40, 200, 1000} {
			b := bounds{X: 2, Y: scroll, Width: 320, Height: 160}
			for _, down := range []float64{0, 20, 159} {
				if got, want := localDIPs(inView(scroll, down), b, flipped), (point{X: 1, Y: down}); got != want {
					t.Errorf("flipped=%v scroll=%v down=%v: got %+v, want %+v", flipped, scroll, down, got, want)
				}
			}
		}
	}
}

// A GTK press can arrive in the coordinates of a window that is not the view
// MyGo hosts: the windowless GtkEntry's private text area is offset inside
// the entry by its border and padding, and a windowless view is offset inside
// the window it shares with its host. Both origins have to come off, in
// logical units, whatever GDK's scale factor is.
func TestEventToViewDIPs(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		p, eventOrigin, viewOrigin point
		want                       point
	}{
		{"the event window is the view's own", point{X: 50, Y: 30}, point{}, point{}, point{X: 50, Y: 30}},
		{"offset event window (the entry's text area)", point{X: 5, Y: 7}, point{X: 24, Y: 18}, point{}, point{X: 29, Y: 25}},
		{"windowless view offset in its window", point{X: 5, Y: 7}, point{}, point{X: 10, Y: 5}, point{X: -5, Y: 2}},
		{"offset event window and windowless view", point{X: 5, Y: 7}, point{X: 24, Y: 18}, point{X: 10, Y: 5}, point{X: 19, Y: 20}},
		{"fractions stay: the units are logical", point{X: 5.5, Y: 7.25}, point{X: 24.5, Y: 18.5}, point{X: 10, Y: 5}, point{X: 20, Y: 20.75}},
	} {
		if got := eventToViewDIPs(tc.p, tc.eventOrigin, tc.viewOrigin); got != tc.want {
			t.Errorf("%s: eventToViewDIPs(%+v, %+v, %+v) = %+v, want %+v", tc.name, tc.p, tc.eventOrigin, tc.viewOrigin, got, tc.want)
		}
	}
}

// The raw event point is what the example used to forward, and it is the
// view-local point only when both origins are zero. With the entry's text
// area inset by 24x18 and the view 10x5 into the window it shares, the raw
// point misses by 14x13 DIPs: far more than the DIP the native-view tests
// allow. This test is the regression: it fails if either origin stops being
// subtracted.
func TestEventToViewDIPsIsNotTheRawEventPoint(t *testing.T) {
	const tolerance = 1 // DIPs, the rounding allowance the native-view tests use
	p, eventOrigin, viewOrigin := point{X: 5, Y: 7}, point{X: 24, Y: 18}, point{X: 10, Y: 5}
	got := eventToViewDIPs(p, eventOrigin, viewOrigin)
	if got == p {
		t.Fatalf("eventToViewDIPs(%+v, %+v, %+v) = the raw event point %+v", p, eventOrigin, viewOrigin, got)
	}
	if dx, dy := got.X-p.X, got.Y-p.Y; dx <= tolerance && dy <= tolerance {
		t.Errorf("eventToViewDIPs(%+v, %+v, %+v) = %+v, only (%v, %v) DIPs from the raw event point: the text area's inset and the view's offset are not coming off",
			p, eventOrigin, viewOrigin, got, dx, dy)
	}
}

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

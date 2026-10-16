package main

import "testing"

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

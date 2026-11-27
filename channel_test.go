//go:build !js

package mygo

import (
	"fmt"
	"strings"
	"testing"
)

// cutter streams a value, closes its channel, and tries once more.
type cutter struct{}

// Cut sends n, closes the channel, and sends again, which fails.
func (cutter) Cut(n int, ch *Channel[int]) error {
	if err := ch.Send(n); err != nil {
		return err
	}
	ch.Close()
	return ch.Send(n)
}

// TestChannelClose checks the channel a bound method closes itself: the
// page takes the value and the end, and the send after the close fails
// the call with it.
func TestChannelClose(t *testing.T) {
	bindForTest(t, "Cut", cutter{})
	_, fw := testWindow(t, WindowOptions{})
	m := call(t, fw, 1, "Cut.Cut", 7, 8)
	var got []string
	for _, p := range pageMessages(t, fw) {
		switch {
		case p["t"] == "chan" && p["end"] == true:
			got = append(got, "end")
		case p["t"] == "chan":
			got = append(got, fmt.Sprint(p["p"]))
		}
	}
	if strings.Join(got, ",") != "7,end" {
		t.Errorf("the page received %v", got)
	}
	if m["ok"] != false || !strings.Contains(fmt.Sprint(m["e"]), "channel closed") {
		t.Errorf("the call = %v (%v), want the closed channel's error", m["ok"], m["e"])
	}
}

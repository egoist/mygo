package media

import (
	"bytes"
	"testing"
	"time"

	"github.com/Eyevinn/mp4ff/mp4"
)

func TestMP4CompositionTimes(t *testing.T) {
	cfg, packets := clipPackets(t)
	for _, tc := range []struct {
		name     string
		pts, dts time.Duration
	}{
		{"reordered", 2 * time.Second, time.Second},
		{"zero decode time", time.Second, 0},
		{"negative offset", time.Second, 2 * time.Second},
		{"no reordering", time.Second, time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			m := newMP4Muxer(&b)
			track, err := m.AddTrack(cfg)
			if err != nil {
				t.Fatal(err)
			}
			p := packets[0]
			p.Track, p.PTS, p.DTS = track, tc.pts, tc.dts
			if err := m.WritePacket(p); err != nil {
				t.Fatal(err)
			}
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
			f, err := mp4.DecodeFile(bytes.NewReader(b.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			traf := f.Segments[0].Fragments[0].Moof.Traf
			if got, want := traf.Tfdt.BaseMediaDecodeTime(), uint64(tc.dts/time.Second)*90000; got != want {
				t.Errorf("decode time = %d, want %d", got, want)
			}
			if got, want := traf.Trun.Samples[0].CompositionTimeOffset, int32((tc.pts-tc.dts)/time.Second)*90000; got != want {
				t.Errorf("composition offset = %d, want %d", got, want)
			}
		})
	}
}

func TestTicksOfLongDuration(t *testing.T) {
	for _, tc := range []struct {
		d     time.Duration
		scale uint32
		want  int64
	}{
		{30 * time.Hour, 90000, 9720000000},
		{-30 * time.Hour, 90000, -9720000000},
		{30*time.Hour + time.Millisecond, 90000, 9720000090},
		{time.Second / 3, 90000, 29999},
		{-time.Second / 3, 90000, -29999},
	} {
		if got := ticksOf(tc.d, tc.scale); got != tc.want {
			t.Errorf("ticksOf(%v, %d) = %d, want %d", tc.d, tc.scale, got, tc.want)
		}
	}
}

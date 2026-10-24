//go:build linux && (amd64 || arm64)

package media

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/egoist/mygo/media/internal/linux"
)

func TestLinuxFileURIPreservesURLs(t *testing.T) {
	for _, target := range []string{"http://127.0.0.1:8123/movie.mp4?part=1", "https://example.org/a%20b.mp4", "file:///tmp/a%20b.mp4"} {
		got, err := linux.FileURI(target)
		if err != nil {
			t.Fatal(err)
		}
		if got != target {
			t.Errorf("FileURI(%q) = %q", target, got)
		}
	}
}

func TestLinuxHTTPSourceDecodes(t *testing.T) {
	server := httptest.NewServer(http.FileServer(http.Dir("testdata")))
	defer server.Close()
	source, err := openSource(server.URL+"/clip.mp4", options{noSound: true})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	frame, err := source.NextFrame(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer frame.Release()
	if frame.Width <= 0 || frame.Height <= 0 {
		t.Fatal("HTTP source returned an empty frame")
	}
}

func TestLinuxEncoderPreservesInputTimeline(t *testing.T) {
	for _, start := range []time.Duration{0, 3 * time.Second} {
		t.Run(start.String(), func(t *testing.T) {
			encoder, err := newH264Encoder(160, 120, 0, 30)
			if err != nil {
				t.Fatal(err)
			}
			defer encoder.close()
			source := newPatternSource(3)
			want := []time.Duration{start, start + 100*time.Millisecond, start + 300*time.Millisecond}
			for _, pts := range want {
				f, err := source.NextFrame(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				f.PTS = pts
				err = encoder.encode(f)
				f.Release()
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := encoder.close(); err != nil {
				t.Fatal(err)
			}
			samples := encoder.take()
			if len(samples) != len(want) {
				t.Fatalf("got %d samples, want %d", len(samples), len(want))
			}
			for i, sample := range samples {
				if sample.packet.PTS != want[i] || sample.packet.DTS != want[i] {
					t.Errorf("sample %d: PTS=%v DTS=%v, want %v", i, sample.packet.PTS, sample.packet.DTS, want[i])
				}
			}
		})
	}
}

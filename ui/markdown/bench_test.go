package markdown

import (
	"os"
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"
)

func llm(b *testing.B) string {
	data, err := os.ReadFile("parser/testdata/llm.md")
	if err != nil {
		b.Fatal(err)
	}
	return string(data)
}

// BenchmarkViewSteady draws frames of the 50 KB answer shown whole, in a
// window of 800 by 1000, as nothing changes.
func BenchmarkViewSteady(b *testing.B) {
	src := llm(b)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Scroll(c).Fill().Children(func() { View(c, src, nil) })
	}, 800, 1000)
	b.ReportAllocs()
	for b.Loop() {
		tt.Frame()
	}
}

// BenchmarkViewStream draws the frames of the 50 KB answer streaming in,
// 16 bytes a frame, as a chat follows a reply's end; it reports the time
// of a frame with its token.
func BenchmarkViewStream(b *testing.B) {
	src := llm(b)
	o := &Options{Reveal: &Reveal{Unit: AtOnce}}
	n := 0
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Scroll(c).Fill().Children(func() { Stream(c, src[:n], o) })
	}, 800, 1000)
	b.ReportAllocs()
	for b.Loop() {
		n += 16
		if n > len(src) {
			n = 16
		}
		tt.Frame()
	}
}

// BenchmarkPartsSteady draws frames of the 50 KB answer in parts, as the
// rows of a List in a window of 800 by 1000, as nothing changes: the
// list builds the parts in view.
func BenchmarkPartsSteady(b *testing.B) {
	src := llm(b)
	var p Parts
	var list ui.ListState
	list.Key = func(i int) any { return p.Key(i) }
	tt := ui.NewTester(func(c *ui.Context) {
		n := p.Update(c, src, nil, false)
		ui.List(c, &list, n, func(i int) { p.View(c, i) }).Fill()
	}, 800, 1000)
	tt.Frame()
	b.ReportAllocs()
	for b.Loop() {
		tt.Frame()
	}
}

// BenchmarkPartsStream draws the frames of the 50 KB answer streaming in
// parts, 16 bytes a frame, as a List following its end.
func BenchmarkPartsStream(b *testing.B) {
	src := llm(b)
	o := &Options{Reveal: &Reveal{Unit: AtOnce}}
	var p Parts
	list := ui.ListState{FollowEnd: true}
	list.Key = func(i int) any { return p.Key(i) }
	n := 0
	tt := ui.NewTester(func(c *ui.Context) {
		parts := p.Update(c, src[:n], o, true)
		ui.List(c, &list, parts, func(i int) { p.View(c, i) }).Fill()
	}, 800, 1000)
	b.ReportAllocs()
	for b.Loop() {
		n += 16
		if n > len(src) {
			n = 16
		}
		tt.Frame()
	}
}

// TestLongReplyRetainedHeap logs the Go heap a long reply keeps, shown
// whole and in parts in a List, in a window of 800 by 900: what the
// views of a chat keep of each reply in view. It warms fonts and
// rendering with a view it then drops, as TestValueFrameRetainedHeap.
func TestLongReplyRetainedHeap(t *testing.T) {
	src := longReply(t)
	views := []struct {
		name string
		view func() func(*ui.Context)
	}{
		{"whole", func() func(*ui.Context) {
			return func(c *ui.Context) {
				ui.Scroll(c).Fill().Children(func() { View(c, src, nil) })
			}
		}},
		{"in parts", func() func(*ui.Context) {
			var p Parts
			var list ui.ListState
			list.Key = func(i int) any { return p.Key(i) }
			return func(c *ui.Context) {
				n := p.Update(c, src, nil, false)
				ui.List(c, &list, n, func(i int) { p.View(c, i) }).Fill()
			}
		}},
	}
	for _, v := range views {
		warm := ui.NewTester(v.view(), 800, 900)
		for range 10 {
			warm.Frame()
		}
		runtime.KeepAlive(warm)
		warm = nil
		runtime.GC()
		var before, after, frames runtime.MemStats
		runtime.ReadMemStats(&before)
		tt := ui.NewTester(v.view(), 800, 900)
		for range 10 {
			tt.Frame()
		}
		runtime.GC()
		runtime.ReadMemStats(&after)
		for range 100 {
			tt.Frame()
		}
		runtime.ReadMemStats(&frames)
		t.Logf("%d bytes %s: retained Go heap %d bytes; a steady frame allocates %d times, %d bytes",
			len(src), v.name, int64(after.HeapAlloc)-int64(before.HeapAlloc),
			(frames.Mallocs-after.Mallocs)/100, (frames.TotalAlloc-after.TotalAlloc)/100)
		runtime.KeepAlive(tt)
	}
}

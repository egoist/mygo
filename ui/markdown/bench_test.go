package markdown

import (
	"fmt"
	"os"
	"runtime"
	"strings"
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

// BenchmarkPartsHistory draws frames of a chat of replies of about 5 KB,
// each in parts, as the rows of one List following its end in a window
// of 800 by 1000, as nothing changes: every reply's Update runs every
// frame, and the rows are keyed as the docs key them.
func BenchmarkPartsHistory(b *testing.B) {
	src := llm(b)
	var replies []string
	for from := 0; from < len(src); {
		to := min(from+5<<10, len(src))
		if k := strings.LastIndex(src[from:to], "\n\n"); to < len(src) && k > 0 {
			to = from + k + 2
		}
		replies = append(replies, src[from:to])
		from = to
	}
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("%d replies", n), func(b *testing.B) {
			type row struct {
				m    int
				part int
			}
			parts := make([]Parts, n)
			var rows []row
			list := ui.ListState{FollowEnd: true}
			list.Key = func(i int) any { return [2]any{rows[i].m, parts[rows[i].m].Key(rows[i].part)} }
			tt := ui.NewTester(func(c *ui.Context) {
				rows = rows[:0]
				for m := range parts {
					for k := range parts[m].Update(c, replies[m%len(replies)], nil, false) {
						rows = append(rows, row{m, k})
					}
				}
				ui.List(c, &list, len(rows), func(i int) { parts[rows[i].m].View(c, rows[i].part) }).Fill()
			}, 800, 1000)
			tt.Frame()
			b.ReportAllocs()
			for b.Loop() {
				tt.Frame()
			}
		})
	}
}

// BenchmarkLongReplyRetainedHeap reports the Go heap a long reply keeps
// (retained-B), shown whole and in parts in a List, in a window of 800 by
// 900: what the views of a chat keep of each reply in view. It warms
// fonts and rendering with a view it then drops, as
// TestValueFrameRetainedHeap, and then draws steady frames.
func BenchmarkLongReplyRetainedHeap(b *testing.B) {
	src := longReply(b)
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
		b.Run(v.name, func(b *testing.B) {
			warm := ui.NewTester(v.view(), 800, 900)
			for range 10 {
				warm.Frame()
			}
			runtime.KeepAlive(warm)
			warm = nil
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			tt := ui.NewTester(v.view(), 800, 900)
			for range 10 {
				tt.Frame()
			}
			runtime.GC()
			runtime.ReadMemStats(&after)
			b.ReportAllocs()
			for b.Loop() {
				tt.Frame()
			}
			// After the loop, whose start clears metrics reported.
			b.ReportMetric(float64(int64(after.HeapAlloc)-int64(before.HeapAlloc)), "retained-B")
			runtime.KeepAlive(tt)
		})
	}
}

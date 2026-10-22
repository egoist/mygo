package text

import (
	"strings"
	"sync"
	"testing"
)

// oversized is a paragraph whose layout is estimated above maxLayoutBytes
// but within maxLiveLayoutBytes (#628).
func oversized(t testing.TB, s *System) Params {
	p := Params{Text: strings.Repeat("mixed 中文。 ", 10000), Width: 700}
	s.mu.Lock()
	n := layoutBytes(s.layout(p))
	s.mu.Unlock()
	if n <= maxLayoutBytes || n > maxLiveLayoutBytes {
		t.Fatalf("fixture is %d bytes, want in (%d, %d]", n, maxLayoutBytes, maxLiveLayoutBytes)
	}
	return p
}

// An unchanged paragraph above the single-layout limit is not shaped again
// on every frame while frames use it, and labels drawn with it stay cached.
func TestOversizedLayoutReusedWhileLive(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	p := oversized(t, s)
	label := Params{Text: "Grace Hopper"}
	first := s.Layout(p)
	s.Layout(label)
	s.EndFrame()
	made := s.LayoutsMade()
	for range 3 * liveLayoutFrames {
		if s.Layout(p) != first {
			t.Fatal("unchanged oversized paragraph was laid out again")
		}
		s.Layout(label)
		s.EndFrame()
	}
	if n := s.LayoutsMade() - made; n != 0 {
		t.Fatalf("%d layouts made across frames that changed nothing", n)
	}
	// Once frames stop using it, EndFrame gives its memory back.
	for range liveLayoutFrames + 1 {
		s.Layout(label)
		s.EndFrame()
	}
	s.mu.Lock()
	_, kept := s.layouts[p]
	over := s.layoutBytes > maxLayoutBytes
	s.mu.Unlock()
	if kept || over {
		t.Fatalf("unused oversized layout kept: %v, bytes over budget: %v", kept, over)
	}
}

// Above the live budget, a single layout is still not cached.
func TestLayoutAboveLiveBudgetNotCached(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	p := Params{Text: strings.Repeat("mixed 中文。 ", 100000), Width: 700}
	s.Layout(p)
	s.mu.Lock()
	_, kept := s.layouts[p]
	s.mu.Unlock()
	if kept {
		t.Fatal("layout above the live budget was cached")
	}
}

// Layout and EndFrame may be called from any goroutine (go test -race).
func TestOversizedLayoutConcurrent(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	p := oversized(t, s)
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			for range 20 {
				s.Layout(p)
				s.Layout(Params{Text: strings.Repeat("x", i+1)})
				s.EndFrame()
			}
		})
	}
	wg.Wait()
}

// BenchmarkLayoutOversized draws a frame of an unchanged paragraph above
// the single-layout limit (#628).
func BenchmarkLayoutOversized(b *testing.B) {
	s := newSystem()
	s.eng = &stubEngine{}
	p := oversized(b, s)
	s.Layout(p)
	s.EndFrame()
	made := s.LayoutsMade()
	b.ReportAllocs()
	for b.Loop() {
		s.Layout(p)
		s.EndFrame()
	}
	b.ReportMetric(float64(s.LayoutsMade()-made)/float64(b.N), "layouts/op")
}

// A paragraph close to the live budget is not cached when room for it
// would evict layouts that frames are using: the labels drawn with it stay
// cached, as before #628, and the paragraph lays out uncached.
func TestNearLiveBudgetLayoutKeepsLiveLabels(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	var labels []Params
	var labelBytes int
	for i := range 4 {
		p := Params{Text: strings.Repeat("Grace Hopper ", 100) + strings.Repeat("!", i)}
		labels = append(labels, p)
		s.mu.Lock()
		labelBytes += layoutBytes(s.layout(p))
		s.mu.Unlock()
	}
	// The largest paragraph within the live budget on its own, found by
	// counts of a repeated phrase.
	big := func(k int) (Params, int) {
		p := Params{Text: strings.Repeat("mixed 中文。 ", k), Width: 700}
		s.mu.Lock()
		defer s.mu.Unlock()
		return p, layoutBytes(s.layout(p))
	}
	lo, hi := 1, 1<<17
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if _, n := big(mid); n <= maxLiveLayoutBytes {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	p, n := big(lo)
	if n <= maxLayoutBytes || n+labelBytes <= maxLiveLayoutBytes {
		t.Fatalf("fixture %d bytes with labels %d does not need room the labels take", n, labelBytes)
	}
	kept := make([]*Layout, len(labels))
	for i, l := range labels {
		kept[i] = s.Layout(l)
	}
	s.EndFrame()
	made := s.LayoutsMade()
	for range 3 {
		for i, l := range labels {
			if s.Layout(l) != kept[i] {
				t.Fatal("a paragraph near the live budget evicted a label frames draw")
			}
		}
		s.Layout(p)
		s.EndFrame()
	}
	// Only the uncached paragraph is laid out again, once a frame.
	if got := s.LayoutsMade() - made; got != 3 {
		t.Fatalf("%d layouts made in 3 frames, want 3 (the paragraph only)", got)
	}
}

// Once a paragraph near the live budget is cached next to the labels that
// frames draw, an ordinary cache miss that takes the cache over the live
// budget evicts that paragraph first, not the labels.
func TestOrdinaryMissEvictsOversizedBeforeLiveLabels(t *testing.T) {
	s := newSystem()
	s.eng = &stubEngine{}
	size := func(p Params) int {
		s.mu.Lock()
		defer s.mu.Unlock()
		return layoutBytes(s.layout(p))
	}
	var labels []Params
	var labelBytes int
	for i := range 4 {
		p := Params{Text: strings.Repeat("Grace Hopper ", 100) + strings.Repeat("!", i)}
		labels = append(labels, p)
		labelBytes += size(p)
	}
	miss := Params{Text: strings.Repeat("Ada Lovelace ", 2000)}
	missBytes := size(miss)
	// The largest paragraph that fits next to the labels.
	big := func(k int) Params { return Params{Text: strings.Repeat("mixed 中文。 ", k), Width: 700} }
	lo, hi := 1, 1<<17
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if size(big(mid))+labelBytes <= maxLiveLayoutBytes {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	p := big(lo)
	n := size(p)
	if n <= maxLayoutBytes || n+labelBytes+missBytes <= maxLiveLayoutBytes || missBytes > maxLayoutBytes {
		t.Fatalf("fixture: paragraph %d, labels %d, miss %d bytes do not go over the live budget", n, labelBytes, missBytes)
	}
	kept := make([]*Layout, len(labels))
	for i, l := range labels {
		kept[i] = s.Layout(l)
	}
	s.Layout(p)
	s.mu.Lock()
	_, cached := s.layouts[p]
	s.mu.Unlock()
	if !cached {
		t.Fatal("fixture: the paragraph was not cached next to the labels")
	}
	s.Layout(miss)
	s.mu.Lock()
	for i, l := range labels {
		if c, ok := s.layouts[l]; !ok || &c.layout != kept[i] {
			s.mu.Unlock()
			t.Fatalf("label %d was evicted by an ordinary miss while an oversized paragraph stayed", i)
		}
	}
	_, still := s.layouts[p]
	over := s.layoutBytes > maxLiveLayoutBytes
	s.mu.Unlock()
	if still || over {
		t.Fatalf("oversized paragraph kept: %v, bytes over the live budget: %v", still, over)
	}
}

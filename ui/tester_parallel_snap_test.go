package ui

import (
	"sync"
	"testing"
)

// TestSnapLinksAcrossTesters runs Testers snapping their scroll on
// different goroutines, and fails under go test -race while ScrollSnap
// wrote the package's snap function, which every scrolling container's
// layout reads.
func TestSnapLinksAcrossTesters(t *testing.T) {
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			<-start
			for range 8 {
				NewTester(func(c *Context) {
					Scroll(c).ScrollSnap(SnapMandatory).Children(func() { Text(c, "card") })
				}, 120, 80).Frame()
			}
		}()
	}
	close(start)
	wg.Wait()
}

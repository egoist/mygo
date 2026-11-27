package ui

import (
	"sync"
	"testing"
)

// TestMaximizeButtonLinksAcrossTesters runs Testers marking a maximize
// button on different goroutines, and fails under go test -race while
// MaximizeButton wrote the package's function that places it, which every
// window's frame reads.
func TestMaximizeButtonLinksAcrossTesters(t *testing.T) {
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			<-start
			for range 8 {
				NewTester(func(c *Context) {
					Text(c, "maximize").MaximizeButton()
				}, 120, 80).Frame()
			}
		}()
	}
	close(start)
	wg.Wait()
}

package ui

import (
	"sync"
	"testing"
)

// TestMorphLinksAcrossTesters runs Testers morphing their text on
// different goroutines, and fails under go test -race while linking a
// morph wrote the package's morph functions, which every Painter's text
// reads at once.
func TestMorphLinksAcrossTesters(t *testing.T) {
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			<-start
			for range 8 {
				NewTester(func(c *Context) {
					Text(c, "morphing text").ContentTransition(ContentTransition{Morph: true})
				}, 120, 80).Frame()
			}
		}()
	}
	close(start)
	wg.Wait()
}

package ui

import (
	"sync"
	"testing"
)

// TestGroupLinksAcrossTesters runs a view fading an element while another
// Tester paints, and fails under go test -race while linking the groups
// wrote the package's group functions, which every Painter's element reads
// as it paints.
func TestGroupLinksAcrossTesters(t *testing.T) {
	reader := NewTester(func(c *Context) { Text(c, "concurrent paint") }, 120, 80)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for range 1000 {
			reader.Frame()
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for range 8 {
			NewTester(func(c *Context) {
				Text(c, "faded text").Opacity(0.5)
			}, 120, 80).Frame()
		}
	}()
	close(start)
	wg.Wait()
}

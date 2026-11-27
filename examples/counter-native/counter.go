// Counter-native is a counter in native UI: a window MyGo draws itself from
// Go, with no web page. The arrow keys count too.
//
//	go run ./examples/counter-native
package main

// counter is the state the window shows.
type counter struct{ n int }

// Increment counts up, Decrement down and Reset back to none: the
// handlers the markup names.
func (s *counter) Increment() { s.n++ }
func (s *counter) Decrement() { s.n-- }
func (s *counter) Reset()     { s.n = 0 }

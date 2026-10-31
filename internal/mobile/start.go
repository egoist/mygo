package mobile

import (
	"bufio"
	"os"
	"runtime"
	_ "unsafe" // go:linkname
)

// mainMain is the app's main function, which the build modes of phones
// (c-archive, c-shared) compile but don't run: start runs it.
//
//go:linkname mainMain main.main
func mainMain()

// start runs the app's main function on a thread of its own, Go's main
// thread, once the host is there.
func start() {
	redirectOutput()
	go func() {
		runtime.LockOSThread()
		mainMain()
	}()
}

// drain runs the calls Go made of the host, on the UI thread.
func drain() {
	if b := current.Load(); b != nil && b.t != nil {
		b.t.drain()
	}
}

// redirectOutput sends what the app writes to its standard output and
// error to the system's log, line by line, where phones show nothing of
// them.
func redirectOutput() {
	for _, f := range []struct {
		file **os.File
		err  bool
	}{{&os.Stdout, false}, {&os.Stderr, true}} {
		r, w, err := os.Pipe()
		if err != nil {
			continue
		}
		if dupOutput(w, *f.file) != nil {
			r.Close()
			w.Close()
			continue
		}
		w.Close() // the standard descriptor writes to the pipe now
		isErr := f.err
		go func() {
			s := bufio.NewScanner(r)
			s.Buffer(make([]byte, 4096), 1<<20)
			for s.Scan() {
				hostLog(isErr, s.Bytes())
			}
		}()
	}
}

//go:build !js

package e2e

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo"
)

type e2eSourceService struct{}

var bindE2eSource sync.Once

func (e2eSourceService) Collect(ctx context.Context, src *mygo.Source[string]) (string, error) {
	var out []string
	for v, err := range src.Values() {
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		out = append(out, v)
	}
	return strings.Join(out, ","), nil
}

// TestSourceStream checks values the page sends through a Source reach the
// method in order, its result arrives after the page closes the source,
// and send rejects once the method returned.
func TestSourceStream(t *testing.T) {
	bindE2eSource.Do(func() { mygo.BindAs("e2eSource", e2eSourceService{}) })
	w := newWindow(t, mygo.WindowOptions{Title: "Source", Width: 300, Height: 200})
	if err := w.Page().LoadURL("app://localhost/"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, `window.run`)
	got := mustEval(t, w, `(async () => {
		const src = window.mygo.source();
		const done = window.mygo.call("e2eSource.Collect", src);
		await src.send("a");
		await src.send("b");
		src.close();
		return await done;
	})()`)
	if got != "a,b" {
		t.Errorf("Collect = %v, want a,b", got)
	}
	got = mustEval(t, w, `(async () => {
		const src = window.mygo.source();
		const done = window.mygo.call("e2eSource.Collect", src);
		src.close();
		await done;
		try {
			await src.send("late");
			return "sent";
		} catch {
			return "rejected";
		}
	})()`)
	if got != "rejected" {
		t.Errorf("send after the method returned = %v, want rejected", got)
	}
}

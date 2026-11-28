package linux

import (
	"net/http/httptest"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func TestCloseSchemeRequestsReleasesUpload(t *testing.T) {
	if err := platform.SetSchemeBodyLimits(platform.SchemeBodyLimits{MaxInFlightRequestBodyBytes: 1}); err != nil {
		t.Fatal(err)
	}
	defer platform.SetSchemeBodyLimits(platform.SchemeBodyLimits{})
	var view schemeRequests
	ctx, finish := view.start()
	defer finish()
	req := httptest.NewRequestWithContext(ctx, "POST", "app://localhost/", nil)
	var held, next platform.SchemeBodyReservation
	if status := held.Reserve(1); status != 0 {
		t.Fatal(status)
	}
	done := make(chan struct{})
	go func() {
		<-req.Context().Done()
		held.Release()
		close(done)
	}()
	defer next.Release()
	if status := next.Reserve(1); status != 503 {
		t.Fatal(status)
	}
	view.close()
	if ctx.Err() == nil {
		t.Fatal("webview close left scheme request active")
	}
	<-done
	if status := next.Reserve(1); status != 0 {
		t.Fatal(status)
	}
}

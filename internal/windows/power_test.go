//go:build windows && (amd64 || arm64)

package windows

import "testing"

// throttledTest asks Windows to throttle the test process, and skips the test
// where it can't: the system's power policy, or a Windows too old for power
// throttling, may say no.
func throttled(t *testing.T) {
	t.Helper()
	if !has(procGetProcessInformation) || !has(procSetProcessInformation) {
		t.Skip("no power throttling on this Windows")
	}
	if _, ok := throttling(); !ok {
		t.Skip("the process's power throttling can't be read here")
	}
	if !setThrottling(true) {
		t.Skip("the process can't be throttled here")
	}
	if on, ok := throttling(); !ok || !on {
		setThrottling(false)
		t.Skip("the process can't be throttled here")
	}
}

func TestKeepRunningTurnsThrottlingOff(t *testing.T) {
	throttled(t)
	p := power{&Backend{}}
	release := p.KeepRunning("Serving the phones")
	if on, ok := throttling(); !ok || on {
		t.Fatal("the process is still throttled while it keeps running")
	}
	release()
	if on, ok := throttling(); !ok || !on {
		t.Fatal("the throttling the process had was not put back")
	}
	setThrottling(false)
}

func TestKeepRunningNests(t *testing.T) {
	throttled(t)
	p := power{&Backend{}}
	first := p.KeepRunning("Serving the phones")
	second := p.KeepRunning("Exporting the video")
	first()
	if on, _ := throttling(); on {
		t.Fatal("the process is throttled while another KeepRunning holds")
	}
	second()
	if on, _ := throttling(); !on {
		t.Fatal("the throttling the process had was not put back")
	}
	setThrottling(false)
}

func TestKeepRunningLeavesAFastProcessFast(t *testing.T) {
	throttled(t)
	setThrottling(false)
	p := power{&Backend{}}
	p.KeepRunning("Serving the phones")()
	if on, _ := throttling(); on {
		t.Fatal("a process that was not throttled was throttled by a release")
	}
}

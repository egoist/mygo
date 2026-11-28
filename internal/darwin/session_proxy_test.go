//go:build darwin && !ios

package darwin

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// WebKit requires the process main thread. Run the store check in a fresh
// test process during package initialization, which Go runs on that thread.
func init() {
	if os.Getenv("MYGO_TEST_WEBSITE_DATA_STORE_PROXY") != "1" {
		return
	}
	runtime.LockOSThread()
	load()
	store, err := NewProxyDataStore(appController{}, platform.DataStoreOptions{Ephemeral: true, Proxy: &platform.SessionProxyConfig{HTTPProxy: "http://127.0.0.1:12345", Username: "test", Password: "test"}})
	if errors.Is(err, platform.ErrUnsupported) {
		os.Exit(77)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if n := sendInt(send(store.(*dataStore).store, "proxyConfigurations"), "count"); n != 1 {
		fmt.Fprintln(os.Stderr, "proxy configuration count", n)
		os.Exit(1)
	}
	store.Close()
	os.Exit(0)
}

func TestWebsiteDataStoreProxy(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "MYGO_TEST_WEBSITE_DATA_STORE_PROXY=1")
	out, err := cmd.CombinedOutput()
	if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 77 {
		t.Skip("proxy configurations unavailable")
	}
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}

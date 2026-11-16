//go:build !js

package mygo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var singleInstance struct {
	sync.Mutex
	listener net.Listener
	path     string
	socket   os.FileInfo // identifies our socket file at path
	lock     *os.File    // the OS lock, held until the process exits
	locked   bool
	handlers listeners[func([]string, string)]
}

type secondInstanceMessage struct {
	Args       []string `json:"args"`
	WorkingDir string   `json:"workingDir"`
	// ActivationToken lets the first instance focus a window for this
	// launch on Wayland, which only grants the focus with the launcher's
	// token.
	ActivationToken string `json:"activationToken,omitempty"`
}

// RequestSingleInstanceLock makes sure a single instance of the app runs.
// It returns true in the first instance. In any later instance it returns
// false after forwarding its command line arguments and working directory to
// the first one (see OnSecondInstance); that instance should then exit:
//
//	if !mygo.App.RequestSingleInstanceLock() {
//		return
//	}
//
// The lock is identified by the application name and, in a packaged app,
// its identifier. It is a lock file next to the socket that hands
// arguments over, granted by the system to one process at a time and
// released only when that process dies: an instance quitting keeps it past
// OnQuit, where only its listener closes, so another launch waits for the
// old process to be gone instead of running beside its shutdown, and two
// launches at once cannot both win.
func (a *Application) RequestSingleInstanceLock() bool {
	if os.Getenv("MYGO_GENERATE") != "" {
		return true // Run will only write the TypeScript client.
	}
	singleInstance.Lock()
	defer singleInstance.Unlock()
	if singleInstance.locked {
		return true
	}
	path := singleInstanceSocket(singleInstanceKey(a.Name()))
	wd, _ := os.Getwd()
	// Read before GTK starts in this process: it takes the token from the
	// environment.
	token := os.Getenv("XDG_ACTIVATION_TOKEN")
	if token == "" {
		token = os.Getenv("DESKTOP_STARTUP_ID")
	}
	msg, _ := json.Marshal(secondInstanceMessage{Args: os.Args[1:], WorkingDir: wd, ActivationToken: token})
	// The lock file is what makes sure only one instance runs; the socket
	// only hands arguments over. The system grants the lock to one process
	// and releases it when that process dies, so a crash leaves nothing
	// behind to clean up (#1906).
	lock, err := os.OpenFile(singleInstanceLockPath(path), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		// Locking is not possible here (e.g. read-only temp dir): behave
		// as if this were the only instance.
		return true
	}
	start, saidWait := time.Now(), false
	for attempt := 0; ; attempt++ {
		if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
			// Another instance owns the lock: hand over our arguments.
			// On Windows the first instance, in the background, could not
			// bring its windows to the front from OnSecondInstance: this
			// launch, which the user just started, passes its right to
			// the foreground on before it exits.
			backend().App().AllowForeground()
			_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
			_, _ = conn.Write(msg)
			if uc, ok := conn.(*net.UnixConn); ok {
				_ = uc.CloseWrite()
			}
			_, _ = io.Copy(io.Discard, conn) // wait until it was read
			conn.Close()
			lock.Close()
			if launchedByDev() {
				// mygo dev would only see the app exit.
				log.Printf("mygo: %s is already running: handed the command line over to it (RequestSingleInstanceLock)", a.Name())
			}
			return false
		}
		// Nobody answers the socket. Only the holder of the lock file may
		// take it over, which settles two launches at the same moment: one
		// wins the lock, and the other hands its arguments over once it
		// listens.
		if err := lockFile(lock); err == nil {
			// We hold the lock: a socket at path is a predecessor's, gone
			// with it (a crash leaves it behind; its release removed it).
			_ = os.Remove(path)
			ln, err := net.Listen("unix", path)
			if err == nil {
				// The socket file is removed on release, and only if it is
				// still ours: an instance that found it stale may have
				// replaced it.
				ln.(*net.UnixListener).SetUnlinkOnClose(false)
				singleInstance.socket, _ = os.Stat(path)
				singleInstance.listener, singleInstance.path, singleInstance.lock = ln, path, lock
				singleInstance.locked = true
				go acceptSecondInstances(ln)
				a.OnQuit(releaseSingleInstanceLock)
				return true
			}
			// Windows reports WSAEADDRINUSE. The lock is ours, so the
			// socket is bound by an instance of an older mygo, which
			// takes no lock file: keep trying, as when another instance
			// holds the lock.
			if !errors.Is(err, syscall.EADDRINUSE) && !errors.Is(err, syscall.Errno(10048)) {
				// Locking is not possible here: behave as if this were
				// the only instance.
				lock.Close()
				return true
			}
		} else if !lockHeld(err) {
			// Locking is not possible here: behave as if this were the
			// only instance.
			lock.Close()
			return true
		}
		// The lock is another instance's, which does not answer: it is
		// starting up, or exiting and done answering. Wait: the dial above
		// hands over to the former, and the system frees the lock with the
		// process for the latter. The wait has no end, for the holder's
		// end is the only thing that frees the lock, and running beside
		// it is what the lock is there to prevent (#1906).
		if !saidWait && time.Now().After(start.Add(singleInstanceWait)) {
			log.Printf("mygo: waiting for the other instance of %s to end (it holds the single instance lock)", a.Name())
			saidWait = true
		}
		pause := time.Duration(attempt+1) * 20 * time.Millisecond
		if pause > 100*time.Millisecond {
			pause = 100 * time.Millisecond
		}
		time.Sleep(pause)
	}
}

// OnSecondInstance is called in the first instance when another instance
// was started and called RequestSingleInstanceLock. Apps usually focus
// their main window here.
func (a *Application) OnSecondInstance(fn func(args []string, workingDir string)) (off func()) {
	return singleInstance.handlers.add(fn, false)
}

func acceptSecondInstances(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
			data, err := io.ReadAll(io.LimitReader(conn, 1<<20))
			if err != nil {
				return
			}
			var msg secondInstanceMessage
			if json.Unmarshal(data, &msg) != nil {
				return
			}
			postMain(func() {
				backend().App().WithActivationToken(msg.ActivationToken, func() {
					for _, fn := range singleInstance.handlers.snapshot() {
						fn(msg.Args, msg.WorkingDir)
					}
					deliverArgs(msg.Args, msg.WorkingDir)
				})
			})
		}()
	}
}

// singleInstanceWait is how long a launch waits for the instance that
// holds the lock file — starting up, or exiting and done answering its
// socket — before saying so. The wait itself has no end: only the
// holder's end frees the lock, and running beside it is what the lock is
// there to prevent.
var singleInstanceWait = 10 * time.Second

// releaseSingleInstanceLock ends what OnQuit releases of the single
// instance lock: the listener that takes handovers. The lock file goes
// only with the process, which keeps later instances out of it until it
// is really done with what App.Run left (#1906).
func releaseSingleInstanceLock() {
	singleInstance.Lock()
	defer singleInstance.Unlock()
	if singleInstance.listener != nil {
		singleInstance.listener.Close()
		if fi, err := os.Stat(singleInstance.path); err == nil && singleInstance.socket != nil && os.SameFile(fi, singleInstance.socket) {
			_ = os.Remove(singleInstance.path)
		}
		singleInstance.listener, singleInstance.socket = nil, nil
	}
}

// singleInstanceKey identifies the lock of the app named name. The
// identifier keeps the development app of mygo dev ("<identifier>.dev")
// apart from the installed app when both call SetName with the same name.
func singleInstanceKey(name string) string {
	if info, ok := packageInfo(); ok && info.Identifier != "" {
		return info.Identifier + "\n" + name
	}
	return name
}

// singleInstanceSocket returns a short per-user socket path; Unix socket
// paths are limited to about 100 bytes.
func singleInstanceSocket(name string) string {
	sum := sha256.Sum256([]byte(name))
	file := "mygo-" + strconv.Itoa(os.Getuid()) + "-" + hex.EncodeToString(sum[:6]) + ".sock"
	for _, dir := range []string{os.Getenv("XDG_RUNTIME_DIR"), os.TempDir(), "/tmp"} {
		if dir != "" && len(filepath.Join(dir, file)) < 100 {
			return filepath.Join(dir, file)
		}
	}
	return filepath.Join("/tmp", file)
}

// singleInstanceLockPath returns the lock file next to the socket at
// path, which the system grants to one process at a time and releases
// when that process dies.
func singleInstanceLockPath(path string) string {
	return strings.TrimSuffix(path, ".sock") + ".lock"
}

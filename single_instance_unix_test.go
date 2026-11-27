//go:build unix

package mygo

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"github.com/egoist/mygo/internal/wait"
)

// ownFolder is the name of the user's folder of sockets and locks in the
// temporary folder.
var ownFolder = "mygo-" + strconv.Itoa(os.Getuid())

// TestSingleInstanceInPrivateFolder checks that the socket and the lock
// are in a folder of the user's that only the user can reach, made for
// them in XDG_RUNTIME_DIR or else in the temporary folder, and that the
// lock works there.
func TestSingleInstanceInPrivateFolder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		env    func(dir string) []string
		folder string
	}{
		{"runtime", func(dir string) []string { return []string{"XDG_RUNTIME_DIR=" + dir} }, "mygo"},
		{"temp", func(dir string) []string { return []string{"XDG_RUNTIME_DIR=", "TMPDIR=" + dir} }, ownFolder},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := shortTempDir(t)
			first := startForwarding(t, "MyGoTest-"+t.Name(), tc.env(dir))
			expectSingleInstanceFiles(t, filepath.Join(dir, tc.folder))
			first.exit(t)
		})
	}
}

// startForwarding starts an instance of the app named name that takes the
// lock and one that hands its command line to it and exits, and returns
// the first, still running.
func startForwarding(t *testing.T, name string, env []string) *testInstance {
	t.Helper()
	first := startInstance(t, name, "hold", env...)
	first.expect(t, "locked")
	second := startInstance(t, name, "", env...)
	second.expect(t, "forwarded")
	second.exit(t)
	return first
}

// expectSingleInstanceFiles checks that folder, of mode 0700, holds a
// socket and a lock.
func expectSingleInstanceFiles(t *testing.T, folder string) {
	t.Helper()
	info, err := os.Lstat(folder)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("%s: %v, %v; want a folder of mode 0700", folder, info, err)
	}
	for _, pattern := range []string{"*.sock", "*.lock"} {
		if found, _ := filepath.Glob(filepath.Join(folder, pattern)); len(found) != 1 {
			t.Errorf("%s in %s: %q, want one", pattern, folder, found)
		}
	}
}

// TestSingleInstanceSkipsSharedFolder checks that a folder other users
// can reach, or a link in its place, is refused for the next one: an
// XDG_RUNTIME_DIR open to others, as the specification forbids, or a
// "mygo" in it that is shared, as one root made under sudo -E. When the
// one in the temporary folder is refused too, /tmp's is next
// (TestSingleInstanceFallsBackToTmp).
func TestSingleInstanceSkipsSharedFolder(t *testing.T) {
	for _, tc := range []struct {
		name string
		make func(t *testing.T, runtime string)
	}{
		{"shared runtime folder", func(t *testing.T, runtime string) {
			if err := os.Chmod(runtime, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"world writable", func(t *testing.T, runtime string) { mkdirMode(t, filepath.Join(runtime, "mygo"), 0o777) }},
		{"group readable", func(t *testing.T, runtime string) { mkdirMode(t, filepath.Join(runtime, "mygo"), 0o750) }},
		{"link", func(t *testing.T, runtime string) {
			target := filepath.Join(runtime, "target")
			mkdirMode(t, target, 0o700)
			if err := os.Symlink(target, filepath.Join(runtime, "mygo")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime, temp := shortTempDir(t), shortTempDir(t)
			tc.make(t, runtime)
			env := []string{"XDG_RUNTIME_DIR=" + runtime, "TMPDIR=" + temp}
			name := "MyGoTest-" + t.Name()
			first := startForwarding(t, name, env)
			expectSingleInstanceFiles(t, filepath.Join(temp, ownFolder))
			if found, _ := filepath.Glob(filepath.Join(runtime, "*", "*.sock")); len(found) != 0 {
				t.Errorf("sockets in the refused folder: %q", found)
			}
			// Only a shared XDG_RUNTIME_DIR is refused for the older
			// MyGo's files too.
			if found, _ := filepath.Glob(filepath.Join(runtime, ownFolder+"-*")); tc.name == "shared runtime folder" && len(found) != 0 {
				t.Errorf("an older MyGo's files in the refused folder: %q", found)
			}
			first.exit(t)
		})
	}
}

// TestSingleInstanceFallsBackToTmp checks that where the folders in
// XDG_RUNTIME_DIR and the temporary folder are both refused, the lock
// and its socket are in /tmp's, as /tmp is sticky. The test removes what
// it made there.
func TestSingleInstanceFallsBackToTmp(t *testing.T) {
	if info, err := os.Stat("/tmp"); err != nil || info.Sys().(*syscall.Stat_t).Uid != 0 {
		t.Skip("/tmp is not root's here, as in a sandbox of its own user namespace")
	}
	runtime, temp := shortTempDir(t), shortTempDir(t)
	if err := os.Chmod(runtime, 0o755); err != nil {
		t.Fatal(err)
	}
	mkdirMode(t, filepath.Join(temp, ownFolder), 0o777)
	name := "MyGoTest-" + t.Name()
	tmp := filepath.Join("/tmp", ownFolder)
	socket := filepath.Join(tmp, singleInstanceHash(singleInstanceKey(name))+".sock")
	if _, err := os.Lstat(tmp); errors.Is(err, fs.ErrNotExist) {
		t.Cleanup(func() { os.Remove(tmp) }) // if empty
	}
	t.Cleanup(func() {
		os.Remove(socket)
		os.Remove(singleInstanceLockPath(socket))
	})
	first := startForwarding(t, name, []string{"XDG_RUNTIME_DIR=" + runtime, "TMPDIR=" + temp})
	if _, err := os.Stat(singleInstanceLockPath(socket)); err != nil {
		t.Errorf("no lock in /tmp's folder: %v", err)
	}
	first.exit(t)
}

func mkdirMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// TestSingleInstanceFolderOfAnotherUser checks that a folder of another
// user, XDG_RUNTIME_DIR as root's under sudo -E or one made in the
// socket's place, is refused. The test cannot make one: it asks for
// folders of the user after its own.
func TestSingleInstanceFolderOfAnotherUser(t *testing.T) {
	runtime := shortTempDir(t) // of mode 0700, which t.TempDir is not
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	if got := privateRuntimeDir(os.Getuid()); got != runtime {
		t.Fatalf("the user's own runtime folder %q was refused: %q", runtime, got)
	}
	if got := privateRuntimeDir(os.Getuid() + 1); got != "" {
		t.Fatalf("a runtime folder of another user was taken: %q", got)
	}
	folder := filepath.Join(runtime, "mygo")
	if err := privateFolder(folder, os.Getuid()); err != nil {
		t.Fatal(err)
	}
	if err := privateFolder(folder, os.Getuid()+1); err == nil {
		t.Fatal("a folder of another user was taken")
	}
	// Others could rename a folder in a parent they can write to, but for
	// a sticky one such as /tmp.
	shared := filepath.Join(runtime, "shared")
	mkdirMode(t, shared, 0o777)
	if err := privateFolder(filepath.Join(shared, "mygo"), os.Getuid()); err == nil {
		t.Fatal("a folder in a parent others can write to was taken")
	}
	if err := os.Chmod(shared, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	if err := privateFolder(filepath.Join(shared, "mygo"), os.Getuid()); err != nil {
		t.Fatalf("a folder in a sticky parent was refused: %v", err)
	}
}

// olderSocket returns the socket path at which an older MyGo listened for
// the app named name, in folder: XDG_RUNTIME_DIR, else the temporary
// folder.
func olderSocket(folder, name string) string {
	return filepath.Join(folder, ownFolder+"-"+singleInstanceHash(singleInstanceKey(name))+".sock")
}

// TestSingleInstanceWaitsForOlderVersion checks that a launch of a new
// version, as an update's Relaunch, waits for an instance of an older
// MyGo that holds the lock where it kept it, in XDG_RUNTIME_DIR or else
// in the temporary folder, as on macOS, and runs once it exits.
func TestSingleInstanceWaitsForOlderVersion(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  func(dir string) []string
	}{
		{"runtime", func(dir string) []string { return []string{"XDG_RUNTIME_DIR=" + dir} }},
		{"temp", func(dir string) []string { return []string{"XDG_RUNTIME_DIR=", "TMPDIR=" + dir} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := shortTempDir(t)
			name := "MyGoTest-" + t.Name()
			old, err := openSingleInstanceLock(singleInstanceLockPath(olderSocket(dir, name)))
			if err != nil || old == nil {
				t.Fatalf("lock: %v, %v", old, err)
			}
			next := startInstance(t, name, "hold", append(tc.env(dir), "MYGO_TEST_INSTANCE_WAIT=1")...)
			next.expect(t, "waiting")
			old.Close()
			next.expect(t, "locked")
			if !lockHeld(t, singleInstanceLockPath(olderSocket(dir, name))) {
				t.Error("the new version does not hold the older lock: an older version would run beside it")
			}
			next.exit(t)
		})
	}
}

// TestSingleInstanceTakesOlderVersionLaunch checks that a launch of an
// older MyGo, as after a downgrade, hands its command line to a running
// instance of a new version, as an older MyGo sends it, instead of
// waiting for the new one to exit.
func TestSingleInstanceTakesOlderVersionLaunch(t *testing.T) {
	runtime := shortTempDir(t)
	name := "MyGoTest-" + t.Name()
	first := startInstance(t, name, "hold", "XDG_RUNTIME_DIR="+runtime)
	first.expect(t, "locked")
	conn, err := net.Dial("unix", olderSocket(runtime, name))
	if err != nil {
		t.Fatalf("an older MyGo's launch finds no instance: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(`{"args":["--old"],"workingDir":"/work"}`)); err != nil {
		t.Fatal(err)
	}
	_ = conn.(*net.UnixConn).CloseWrite()
	var ack [1]byte
	if _, err := conn.Read(ack[:]); err != nil {
		t.Fatalf("the command line was not taken: %v", err)
	}
	first.exit(t)
}

// TestSingleInstanceForwardsToOlderVersion checks that a launch of a new
// version hands its command line to a running instance of an older MyGo.
func TestSingleInstanceForwardsToOlderVersion(t *testing.T) {
	runtime := shortTempDir(t)
	name := "MyGoTest-" + t.Name()
	ln, err := net.Listen("unix", olderSocket(runtime, name))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	read := make(chan secondInstanceMessage, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		if msg, err := readSecondInstance(conn); err == nil {
			read <- msg
		}
	}()
	next := startInstance(t, name, "", "XDG_RUNTIME_DIR="+runtime)
	next.expect(t, "forwarded")
	next.exit(t)
	if msg := wait.For(t, "the message to be read", read); msg.WorkingDir == "" {
		t.Errorf("handed over %+v", msg)
	}
}

// TestSingleInstancePeerOfAnotherUser checks that the first instance
// takes command lines only from processes of its own user. The test
// cannot connect as another user: it asks for the user after its own.
func TestSingleInstancePeerOfAnotherUser(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "s.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	client, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	conn, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	peer, err := peerUser(conn)
	if errors.Is(err, errors.ErrUnsupported) {
		t.Skip("this system does not tell the user of a socket's peer")
	}
	if err != nil || peer != os.Getuid() {
		t.Fatalf("peer user %d, %v; want %d", peer, err, os.Getuid())
	}
	if err := peerRefused(conn, os.Getuid()); err != nil {
		t.Fatalf("a process of the same user was refused: %v", err)
	}
	if peerRefused(conn, os.Getuid()+1) == nil {
		t.Fatal("a process of another user was taken")
	}
	client.Close()
	conn.Close()
	if err := peerRefused(conn, os.Getuid()); err == nil {
		t.Fatal("a process whose user is unknown was taken")
	}
}

// TestSingleInstanceSockets checks that the instance listens on each of
// its sockets, though one fails, and that its quit removes the socket
// files that are still its own, not one another process put in place of
// one.
func TestSingleInstanceSockets(t *testing.T) {
	dir := shortTempDir(t)
	own, replaced := filepath.Join(dir, "own.sock"), filepath.Join(dir, "replaced.sock")
	failing := filepath.Join(dir, "missing", "failing.sock")
	singleInstance.Lock()
	paths, listening := singleInstance.paths, singleInstance.listening
	singleInstance.paths, singleInstance.listening = []string{failing, own, replaced}, nil
	listenSecondInstances()
	got := len(singleInstance.listening)
	singleInstance.Unlock()
	defer func() {
		singleInstance.Lock()
		singleInstance.paths, singleInstance.listening = paths, listening
		singleInstance.Unlock()
	}()
	if got != 2 {
		t.Fatalf("listening on %d sockets, want 2", got)
	}
	for _, path := range []string{own, replaced} {
		conn, err := net.Dial("unix", path)
		if err != nil {
			t.Fatalf("%s takes no command lines: %v", path, err)
		}
		conn.Close()
	}
	// Another process, an older MyGo that takes no lock, replaces one.
	if err := os.Remove(replaced); err != nil {
		t.Fatal(err)
	}
	other, err := net.Listen("unix", replaced)
	if err != nil {
		t.Fatal(err)
	}
	other.(*net.UnixListener).SetUnlinkOnClose(false)
	defer other.Close()
	stopSecondInstances()
	if _, err := os.Lstat(own); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the instance's own socket was left: %v", err)
	}
	if _, err := os.Lstat(replaced); err != nil {
		t.Errorf("the socket of another process was removed: %v", err)
	}
}

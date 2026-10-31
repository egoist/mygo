package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The development builds of phones: mygo dev -platform ios builds the app
// for the simulator, installs it on the one booted (or -device) and
// launches it, its output in the console; -platform android does the
// same on the device or emulator adb reaches. The app loads devUrl,
// which the simulator reaches as the Mac does and Android through adb
// reverse; without devUrl it embeds frontendDist, as the device cannot
// read the Mac's disk.

// mobileDevice is the device a development build runs on.
type mobileDevice struct {
	platform string // "ios" or "android"
	id       string // the simulator's UDID, or adb's serial
	name     string
}

// bootedSimulator finds the simulator to run on: id if set (a UDID or a
// name), else the booted one.
func bootedSimulator(id string) (*mobileDevice, error) {
	out, err := exec.Command("xcrun", "simctl", "list", "devices", "-j").Output()
	if err != nil {
		return nil, fmt.Errorf("no simulators (install Xcode): %v", err)
	}
	var list struct {
		Devices map[string][]struct {
			UDID, Name, State string
			IsAvailable       bool
		}
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, err
	}
	var booted *mobileDevice
	for runtime, devs := range list.Devices {
		if !strings.Contains(runtime, "iOS") {
			continue
		}
		for _, d := range devs {
			if !d.IsAvailable {
				continue
			}
			if id != "" && (d.UDID == id || d.Name == id) {
				if d.State != "Booted" {
					if err := exec.Command("xcrun", "simctl", "boot", d.UDID).Run(); err != nil {
						return nil, fmt.Errorf("could not boot %s: %v", d.Name, err)
					}
				}
				return &mobileDevice{"ios", d.UDID, d.Name}, nil
			}
			if id == "" && d.State == "Booted" && booted == nil {
				booted = &mobileDevice{"ios", d.UDID, d.Name}
			}
		}
	}
	if id != "" {
		return nil, fmt.Errorf("no simulator %q (xcrun simctl list devices lists them)", id)
	}
	if booted == nil {
		return nil, errors.New("no simulator is booted: open one (open -a Simulator), or pass -device with its name or UDID")
	}
	return booted, nil
}

// buildAndLaunchMobile is buildAndLaunch for phones.
func (s *devSession) buildAndLaunchMobile(ctx context.Context, t *task, running [32]byte) (p *devProcess, sum [32]byte, err error) {
	c, err := loadConfig(s.root)
	if err != nil {
		return nil, sum, err
	}
	dc := devConfig(c)
	dir := filepath.Join(s.root, ".mygo", "dev", s.device.platform)
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return nil, sum, err
	}
	if err := s.updateI18n(c); err != nil {
		return nil, sum, err
	}
	opts := buildOptions{debug: true, work: work}
	if opts.pkg, err = packageDir(c); err != nil {
		return nil, sum, err
	}
	if c.DevURL == "" {
		if opts.overlay, err = frontendFiles(c, opts.pkg, work); err != nil {
			return nil, sum, err
		}
	}
	stage, err := os.MkdirTemp(dir, ".staging-")
	if err != nil {
		return nil, sum, err
	}
	defer os.RemoveAll(stage)

	var app string
	switch s.device.platform {
	case "ios":
		tc, err := newIOSToolchain(c, true)
		if err != nil {
			return nil, sum, err
		}
		if app, err = tc.buildApp(ctx, c, dc, t, opts, stage); err != nil {
			return nil, sum, err
		}
	case "android":
		if app, err = s.buildAndroidDev(ctx, c, dc, t, opts, stage); err != nil {
			return nil, sum, err
		}
	}
	h := sha256.New()
	if err := hashTree(h, app); err != nil {
		return nil, sum, err
	}
	copy(sum[:], h.Sum(nil))
	if sum == running {
		return nil, sum, errUnchanged
	}
	if c.Bindings != "" {
		t.set("generating the TypeScript client")
		if err := writeClient(ctx, c, false); err != nil {
			return nil, sum, err
		}
	}
	final := filepath.Join(dir, filepath.Base(app))
	if s.device.platform == "ios" {
		t.set("signing")
		if err := runQuiet("codesign", "--force", "--sign", "-", app); err != nil {
			return nil, sum, err
		}
	}
	if err := replacePath(app, final); err != nil {
		return nil, sum, err
	}
	t.set("installing on " + s.device.name)
	s.stopLive()
	if p, err = s.launchMobile(dc, final); err != nil {
		return nil, sum, err
	}
	return p, sum, nil
}

// hashTree hashes the files under root, by path and content.
func hashTree(h io.Writer, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if rel == filepath.Join("_CodeSignature", "CodeResources") {
			return nil
		}
		io.WriteString(h, rel+"\x00")
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(h, f)
		return err
	})
}

// launchMobile installs the app on the device and starts it, its output
// in the console. The process it returns ends when the app does.
func (s *devSession) launchMobile(dc *Config, app string) (*devProcess, error) {
	env := append(append([]string{}, s.env...), "MYGO_DEV=1")
	var cmd *exec.Cmd
	var stop func()
	switch s.device.platform {
	case "ios":
		if out, err := exec.Command("xcrun", "simctl", "install", s.device.id, app).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("simctl install: %v\n%s", err, strings.TrimSpace(string(out)))
		}
		cmd = exec.Command("xcrun", "simctl", "launch", "--console-pty", "--terminate-running-process", s.device.id, dc.Identifier)
		cmd.Env = os.Environ()
		for _, e := range env {
			// simctl passes the variables named so to the app.
			cmd.Env = append(cmd.Env, "SIMCTL_CHILD_"+e)
		}
		id := s.device.id
		stop = func() { exec.Command("xcrun", "simctl", "terminate", id, dc.Identifier).Run() }
	case "android":
		var err error
		if cmd, stop, err = s.launchAndroid(dc, app, env); err != nil {
			return nil, err
		}
	}
	out, outDone, err := pipe(con.output("app", os.Stdout))
	if err != nil {
		return nil, err
	}
	r, w, err := os.Pipe()
	if err != nil {
		out.Close()
		return nil, err
	}
	go func() {
		// The lines of the app's, without the system's noise about it.
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 4096), 1<<20)
		for sc.Scan() {
			if l := sc.Text(); !mobileNoise(l, dc.Identifier) {
				io.WriteString(out, l+"\n")
			}
		}
		r.Close()
		out.Close()
	}()
	cmd.Stdout, cmd.Stderr = w, w
	setProcessGroup(cmd)
	err = cmd.Start()
	w.Close()
	if err != nil {
		return nil, err
	}
	p := &devProcess{exe: app, cmd: cmd, done: make(chan struct{}), onStop: stop}
	go func() {
		p.err = cmd.Wait()
		<-outDone
		close(p.done)
	}()
	return p, nil
}

// mobileNoise reports whether a line of a development build's output is
// the system's rather than the app's: the simulator's warnings about its
// own frameworks, simctl's line of the app's process ID.
func mobileNoise(line, id string) bool {
	if strings.Contains(line, "is implemented in both") && strings.Contains(line, ".simruntime") {
		return true
	}
	if rest, ok := strings.CutPrefix(line, id+": "); ok && strings.Trim(rest, "0123456789") == "" {
		return true
	}
	return false
}

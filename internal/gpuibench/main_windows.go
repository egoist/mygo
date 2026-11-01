// Command gpuibench compares MyGo's native UI with GPUI, Zed's framework, on
// the machine it runs on, in real windows on the real GPU and display. It
// needs a desktop session, and shows windows on top of the others for as long
// as it runs. See README.md for what it measures, how, and what it found.
//
//	go run ./internal/gpuibench [-runs 6] [-modes frames,latency,startup,idle,size] [-out results.json]
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"log"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// scene is a stress scene of both apps, and what it asks of them.
type scene struct {
	name, app string
	count     int
	w, h      int
}

var scenes = []scene{
	{"boxes", "boxes", 600, 900, 640},
	{"labels", "labels", 0, 900, 640},
	{"translated_column", "translated_column", 0, 900, 640},
	{"scrolled_column", "scrolled_column", 0, 900, 640},
	{"list", "list", 0, 900, 640},
	{"dashboard", "dashboard", 0, 900, 640},
	{"paragraphs", "paragraphs", 24, 900, 640},
	{"boxes×4", "boxes", 2400, 900, 640},
	{"boxes×16", "boxes", 9600, 900, 640},
	{"boxes×64", "boxes", 38400, 900, 640},
	{"dashboard large", "dashboard", 0, 1800, 1000},
}

// framework is a build of a side of the comparison.
type framework struct{ name, exe string }

var (
	runs      = flag.Int("runs", 6, "runs of each measurement, alternating frameworks; each result is their median")
	frames    = flag.Int("frames", 600, "frames measured in a run of a stress scene")
	warmup    = flag.Int("warmup", 120, "frames before measuring")
	samples   = flag.Int("samples", 100, "input events measured in a run")
	modes     = flag.String("modes", "frames,latency,startup,idle,size", "what to measure: frames, screen, latency, startup, idle, size, shots")
	only      = flag.String("only", "", "only the stress scenes with these names, separated by commas")
	out       = flag.String("out", "", "write the raw results to this JSON file")
	shots     = flag.String("shots", "", "directory the shots mode writes PNGs to")
	gpuiDir   = flag.String("gpui", filepath.Join("internal", "gpuibench", "gpui"), "GPUI's Cargo project")
	buildDir  = flag.String("bin", "", "directory for MyGo's builds (default: a temporary one)")
	cycleRate float64
)

func main() {
	log.SetFlags(0)
	flag.Parse()
	// Pixels of the screen rather than DIPs, whatever its scale.
	const perMonitorAwareV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2, -4
	setDpiAwarenessContext.Call(perMonitorAwareV2)
	cycleRate = calibrateCycles()

	mygoExe, mygoHello, gpuiExe, gpuiHello, err := build()
	if err != nil {
		log.Fatal(err)
	}
	sides := []framework{{"MyGo", mygoExe}, {"GPUI", gpuiExe}}
	results := map[string]any{"cycles_per_second": cycleRate}
	for _, mode := range strings.Split(*modes, ",") {
		var r any
		switch mode {
		case "frames":
			r, err = measureFrames(sides, false)
		case "screen":
			r, err = measureFrames(sides, true)
		case "latency":
			r, err = measureLatency(sides)
		case "startup":
			r, err = measureStartup(sides)
		case "idle":
			r, err = measureIdle(sides)
		case "size":
			r, err = measureSize(mygoExe, mygoHello, gpuiExe, gpuiHello)
		case "shots":
			err = takeShots(sides)
		default:
			err = fmt.Errorf("no mode %q", mode)
		}
		if err != nil {
			log.Fatalf("%s: %v", mode, err)
		}
		results[mode] = r
		if *out != "" { // kept as it goes: a run takes half an hour
			data, _ := json.MarshalIndent(results, "", " ")
			if err := os.WriteFile(*out, data, 0o644); err != nil {
				log.Fatal(err)
			}
		}
	}
}

// build makes release builds of both sides, as the benchmarks of bench.ts and
// Zed's releases build theirs.
func build() (mygoExe, mygoHello, gpuiExe, gpuiHello string, err error) {
	dir := *buildDir
	if dir == "" {
		if dir, err = os.MkdirTemp("", "gpuibench"); err != nil {
			return
		}
	}
	mygoExe = filepath.Join(dir, "gpuibench-mygo.exe")
	mygoHello = filepath.Join(dir, "gpuibench-mygo-hello.exe")
	ld := "-s -w -X github.com/egoist/mygo.production=1"
	for _, b := range []struct{ out, pkg, ld string }{
		{mygoExe, "./internal/gpuibench/app", ld},
		// mygo build's flags for an app without a console.
		{mygoHello, "./internal/gpuibench/hello", ld + " -H=windowsgui"},
	} {
		cmd := exec.Command("go", "build", "-trimpath", "-tags", "mygo_noinspector", "-ldflags", b.ld, "-o", b.out, b.pkg)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err = cmd.Run(); err != nil {
			return
		}
	}
	cargo := exec.Command("cargo", "build", "--release", "--message-format=short")
	cargo.Dir = *gpuiDir
	cargo.Stdout, cargo.Stderr = os.Stdout, os.Stderr
	if err = cargo.Run(); err != nil {
		return
	}
	target := os.Getenv("CARGO_TARGET_DIR")
	if target == "" {
		target = filepath.Join(*gpuiDir, "target")
	}
	gpuiExe = filepath.Join(target, "release", "gpuibench-gpui.exe")
	gpuiHello = filepath.Join(target, "release", "gpuibench-gpui-hello.exe")
	return
}

// app is a running side of the comparison.
type app struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines *bufio.Scanner
}

// startApp starts a build drawing a scene.
func startApp(exe string, args ...string) (*app, error) {
	cmd := exec.Command(exe, args...)
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	lines := bufio.NewScanner(stdout)
	lines.Buffer(nil, 1<<22)
	return &app{cmd, stdin, lines}, nil
}

func (a *app) stop() {
	a.cmd.Process.Kill()
	a.cmd.Wait()
}

// waitLine returns the app's next line, or fails once it has exited.
func (a *app) waitLine() (string, error) {
	if a.lines.Scan() {
		return a.lines.Text(), nil
	}
	return "", fmt.Errorf("the app ended: %v", a.lines.Err())
}

// waitReady waits for the app's warmup to end and for its window, which it
// keeps on top.
func (a *app) waitReady() (window, error) {
	line, err := a.waitLine()
	if err != nil {
		return window{}, err
	}
	if line != "ready" {
		return window{}, fmt.Errorf("the app said %q", line)
	}
	win, ok := findWindow(a.cmd.Process.Pid)
	if !ok {
		return window{}, errors.New("no window")
	}
	keepOnTop(win.hwnd)
	return win, nil
}

// sceneArgs are the flags of both apps for a scene.
func sceneArgs(s scene, warmup, frames int) []string {
	return []string{"-scene", s.app, "-count", strconv.Itoa(s.count), "-w", strconv.Itoa(s.w), "-h", strconv.Itoa(s.h),
		"-warmup", strconv.Itoa(warmup), "-frames", strconv.Itoa(frames), "-x", "40", "-y", "40"}
}

// checkSize fails unless the window's content is the size asked for at its
// scale, on both sides, as frames of different sizes do not compare.
func checkSize(win window, w, h int) error {
	if abs(win.w-win.client(float64(w))) > 2 || abs(win.h-win.client(float64(h))) > 2 {
		return fmt.Errorf("a window of %d×%d at %d dpi, asked for %d×%d DIPs", win.w, win.h, win.dpi, w, h)
	}
	return nil
}

func abs(v int) int { return max(v, -v) }

// frameRun is what a run of a stress scene measured.
type frameRun struct {
	FPS, P50, P99, Missed float64
	MainMS, ProcessMS     float64
	GPUMS                 float64
	GPUBusy               float64
	OtherCores            float64
	PrivateMB, PeakMB     float64
	ScreenFPS             float64
	Width, Height, DPI    int
}

type measured struct {
	Frames        int       `json:"frames"`
	ThreadCycles  uint64    `json:"thread_cycles"`
	ProcessCycles uint64    `json:"process_cycles"`
	Intervals     []float64 `json:"intervals_ms"`
}

// measureFrames runs each scene on each side, alternating sides from run to
// run. With screen, it also counts the frames that reach the display, which
// costs a duplication of the desktop: those runs answer whether the frames
// the apps count show, not how much they cost.
func measureFrames(sides []framework, screen bool) (map[string]map[string][]frameRun, error) {
	all := map[string]map[string][]frameRun{}
	var refresh float64
	{
		d, err := newDuplicator()
		if err != nil {
			return nil, err
		}
		refresh = d.hz
		d.close()
	}
	fmt.Printf("display: %.3f Hz, %.2f GHz cycle counter\n", refresh, cycleRate/1e9)
	picked := scenes
	if *only != "" {
		names := strings.Split(*only, ",")
		picked = slices.DeleteFunc(slices.Clone(scenes), func(s scene) bool { return !slices.Contains(names, s.name) })
	}
	for run := range *runs {
		for _, s := range picked {
			if all[s.name] == nil {
				all[s.name] = map[string][]frameRun{}
			}
			for i := range sides {
				side := sides[(i+run)%len(sides)]
				r, err := frameOnce(side, s, refresh, screen)
				if err != nil {
					return nil, fmt.Errorf("%s %s: %w", side.name, s.name, err)
				}
				all[s.name][side.name] = append(all[s.name][side.name], r)
				fmt.Printf("run %d %-18s %-5s %6.1f fps  main %6.2f ms  process %6.2f ms  gpu %6.2f ms  others %.2f cores\n",
					run+1, s.name, side.name, r.FPS, r.MainMS, r.ProcessMS, r.GPUMS, r.OtherCores)
			}
		}
	}
	printFrames(all, picked, sides, screen)
	return all, nil
}

func frameOnce(side framework, s scene, refresh float64, screen bool) (frameRun, error) {
	var r frameRun
	a, err := startApp(side.exe, sceneArgs(s, *warmup, *frames)...)
	if err != nil {
		return r, err
	}
	defer a.stop()
	win, err := a.waitReady()
	if err != nil {
		return r, err
	}
	if err := checkSize(win, s.w, s.h); err != nil {
		return r, err
	}
	r.Width, r.Height, r.DPI = win.w, win.h, win.dpi
	pid := a.cmd.Process.Pid
	gpu, err := newGPUTime(pid)
	if err != nil {
		return r, err
	}
	defer gpu.close()
	gpu0, err := gpu.seconds()
	if err != nil {
		return r, err
	}
	busy0 := systemBusy()
	t0 := qpc()
	if _, err := a.stdin.Write([]byte("go\n")); err != nil {
		return r, err
	}
	lines := make(chan string, 1)
	failure := make(chan error, 1)
	go func() {
		line, err := a.waitLine()
		if err != nil {
			failure <- err
			return
		}
		lines <- line
	}()
	var line string
	if screen {
		line, r.ScreenFPS, err = countScreenFrames(win, lines, failure)
		if err != nil {
			return r, err
		}
	} else {
		select {
		case line = <-lines:
		case err := <-failure:
			return r, err
		}
	}
	seconds := float64(qpc()-t0) / qpcHz
	gpu1, err := gpu.seconds()
	if err != nil {
		return r, err
	}
	busy1 := systemBusy()
	mem, err := memoryOf(pid)
	if err != nil {
		return r, err
	}
	var m measured
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return r, fmt.Errorf("%w: %.80s", err, line)
	}
	var total float64
	missed := 0
	for _, v := range m.Intervals {
		total += v
		if v > 1500/refresh {
			missed++
		}
	}
	sorted := slices.Sorted(slices.Values(m.Intervals))
	n := float64(m.Frames)
	r.FPS = n / (total / 1e3)
	r.P50 = sorted[len(sorted)/2]
	r.P99 = sorted[len(sorted)*99/100]
	r.Missed = float64(missed) / n
	r.MainMS = float64(m.ThreadCycles) / cycleRate * 1e3 / n
	r.ProcessMS = float64(m.ProcessCycles) / cycleRate * 1e3 / n
	r.GPUMS = (gpu1 - gpu0) * 1e3 / n
	r.GPUBusy = (gpu1 - gpu0) / (total / 1e3)
	r.OtherCores = (busy1-busy0)/seconds - float64(m.ProcessCycles)/cycleRate/seconds
	r.PrivateMB = float64(mem.PrivateWorkingSetSize) / (1 << 20)
	r.PeakMB = float64(mem.PeakWorkingSetSize) / (1 << 20)
	return r, nil
}

// countScreenFrames counts the images of the window's content that differ
// from the one before, until the app's result comes.
func countScreenFrames(win window, lines <-chan string, failure <-chan error) (string, float64, error) {
	d, err := newDuplicator()
	if err != nil {
		return "", 0, err
	}
	defer d.close()
	var last uint64
	count := 0
	start := qpc()
	for {
		select {
		case line := <-lines:
			return line, float64(count) / (float64(qpc()-start) / qpcHz), nil
		case err := <-failure:
			return "", 0, err
		default:
		}
		err := d.next(50*time.Millisecond, func(info *frameInfo, img uintptr) error {
			if info.LastPresentTime == 0 {
				return nil
			}
			return d.read(img, win.x, win.y, win.w, win.h, func(pix []byte, pitch int) {
				var sum uint64 = 14695981039346656037
				for y := 0; y < win.h; y += 3 {
					row := pix[y*pitch : y*pitch+win.w*4]
					for x := 0; x < len(row); x += 12 {
						sum = (sum ^ uint64(row[x]) ^ uint64(row[x+1])<<8 ^ uint64(row[x+2])<<16) * 1099511628211
					}
				}
				if sum != last {
					count++
					last = sum
				}
			})
		})
		if err != nil && !errors.Is(err, errNoFrame) {
			return "", 0, err
		}
	}
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := slices.Sorted(slices.Values(v))
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

func medianOf[T any](runs []T, get func(T) float64) float64 {
	v := make([]float64, len(runs))
	for i, r := range runs {
		v[i] = get(r)
	}
	return median(v)
}

func printFrames(all map[string]map[string][]frameRun, picked []scene, sides []framework, screen bool) {
	fmt.Println("\n| scene | framework | fps | p50 ms | p99 ms | missed | main CPU ms/frame | process CPU ms/frame | GPU ms/frame | private MB | screen fps |")
	fmt.Println("|---|---|---|---|---|---|---|---|---|---|---|")
	for _, s := range picked {
		for _, side := range sides {
			rs := all[s.name][side.name]
			at := func(get func(frameRun) float64) float64 { return medianOf(rs, get) }
			fmt.Printf("| %s | %s | %.1f | %.2f | %.2f | %.1f%% | %.2f | %.2f | %.2f | %.0f | %.1f |\n", s.name, side.name,
				at(func(r frameRun) float64 { return r.FPS }), at(func(r frameRun) float64 { return r.P50 }),
				at(func(r frameRun) float64 { return r.P99 }), at(func(r frameRun) float64 { return r.Missed * 100 }),
				at(func(r frameRun) float64 { return r.MainMS }), at(func(r frameRun) float64 { return r.ProcessMS }),
				at(func(r frameRun) float64 { return r.GPUMS }), at(func(r frameRun) float64 { return r.PrivateMB }),
				at(func(r frameRun) float64 { return r.ScreenFPS }))
		}
	}
}

// latencyScenes are windows that change color when the pointer crosses their
// middle, alone or with boxes moving.
var latencyScenes = []string{"input", "input-busy"}

// measureLatency posts pointer moves to a window and times them to the image
// that shows the window's new color reaching the display.
func measureLatency(sides []framework) (map[string]map[string][][]float64, error) {
	all := map[string]map[string][][]float64{}
	for run := range *runs {
		for _, name := range latencyScenes {
			if all[name] == nil {
				all[name] = map[string][][]float64{}
			}
			for i := range sides {
				side := sides[(i+run)%len(sides)]
				v, err := latencyOnce(side, name)
				if err != nil {
					return nil, fmt.Errorf("%s %s: %w", side.name, name, err)
				}
				all[name][side.name] = append(all[name][side.name], v)
				fmt.Printf("run %d %-11s %-5s %s\n", run+1, name, side.name, describeLatency(v))
			}
		}
	}
	fmt.Println("\n| scene | framework | mean ms | p50 ms | p95 ms | p99 ms |")
	fmt.Println("|---|---|---|---|---|---|")
	for _, name := range latencyScenes {
		for _, side := range sides {
			var joined []float64
			for _, v := range all[name][side.name] {
				joined = append(joined, v...)
			}
			fmt.Printf("| %s | %s | %.1f | %.1f | %.1f | %.1f |\n", name, side.name, mean(joined), percentile(joined, 50), percentile(joined, 95), percentile(joined, 99))
		}
	}
	return all, nil
}

func mean(v []float64) float64 {
	var sum float64
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}

func percentile(v []float64, p int) float64 {
	s := slices.Sorted(slices.Values(v))
	return s[min(len(s)*p/100, len(s)-1)]
}

func describeLatency(v []float64) string {
	return fmt.Sprintf("mean %5.1f  p50 %5.1f  p95 %5.1f  p99 %5.1f ms", mean(v), percentile(v, 50), percentile(v, 95), percentile(v, 99))
}

func latencyOnce(side framework, name string) ([]float64, error) {
	warm := 1
	if name == "input-busy" {
		warm = *warmup
	}
	a, err := startApp(side.exe, sceneArgs(scene{name, name, 300, 900, 640}, warm, 1<<30)...)
	if err != nil {
		return nil, err
	}
	defer a.stop()
	win, err := a.waitReady()
	if err != nil {
		return nil, err
	}
	if err := checkSize(win, 900, 640); err != nil {
		return nil, err
	}
	// Let the first frames settle.
	time.Sleep(500 * time.Millisecond)
	d, err := newDuplicator()
	if err != nil {
		return nil, err
	}
	defer d.close()
	// Left of the boxes, and below them, where the pointer moves over the
	// background itself.
	sx, sy := win.point(20, 320)
	move := func(right bool) (int64, error) {
		x, y := win.point(100, 636)
		want := byte(0)
		if right {
			x, y = win.point(800, 636)
			want = 255
		}
		deadline := time.Now().Add(5 * time.Second)
		t0 := qpc()
		postMouseMove(x, y)
		for time.Now().Before(deadline) {
			var seen int64
			err := d.next(5*time.Second, func(info *frameInfo, img uintptr) error {
				if info.LastPresentTime == 0 {
					return nil
				}
				return d.read(img, sx, sy, 1, 1, func(pix []byte, _ int) {
					if pix[0] == want && pix[1] == want && pix[2] == want {
						seen = info.LastPresentTime
					}
				})
			})
			if err != nil {
				return 0, fmt.Errorf("waiting for the window to turn %d: %w", want, err)
			}
			if seen != 0 {
				return seen - t0, nil
			}
		}
		return 0, fmt.Errorf("the window never turned %d", want)
	}
	right := true
	var v []float64
	for i := -2; i < *samples; i++ {
		// At any time within a refresh, and long enough for the window to be
		// idle when alone; a frame's work apart when busy.
		pause := 120 + rand.Float64()*130
		if name == "input-busy" {
			pause = 40 + rand.Float64()*60
		}
		time.Sleep(time.Duration(pause * float64(time.Millisecond)))
		ticks, err := move(right)
		if err != nil {
			return nil, err
		}
		right = !right
		if i >= 0 {
			v = append(v, float64(ticks)/qpcHz*1e3)
		}
	}
	return v, nil
}

// measureStartup times a process's start to the image of its first frame
// reaching the display.
func measureStartup(sides []framework) (map[string][]float64, error) {
	all := map[string][]float64{}
	d, err := newDuplicator()
	if err != nil {
		return nil, err
	}
	defer d.close()
	for run := range *runs * 2 {
		for i := range sides {
			side := sides[(i+run)%len(sides)]
			ms, err := startupOnce(d, side)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", side.name, err)
			}
			// The first run of a build reads it from disk.
			if run > 0 {
				all[side.name] = append(all[side.name], ms)
			}
			fmt.Printf("run %d startup %-5s %.1f ms\n", run+1, side.name, ms)
		}
	}
	fmt.Println("\n| framework | median ms | fastest ms | slowest ms |")
	fmt.Println("|---|---|---|---|")
	for _, side := range sides {
		v := all[side.name]
		fmt.Printf("| %s | %.1f | %.1f | %.1f |\n", side.name, median(v), slices.Min(v), slices.Max(v))
	}
	return all, nil
}

func startupOnce(d *duplicator, side framework) (float64, error) {
	// Nothing new on the display before the process starts counts.
	for d.next(0, func(*frameInfo, uintptr) error { return nil }) == nil {
	}
	t0 := qpc()
	a, err := startApp(side.exe, sceneArgs(scene{"idle", "idle", 0, 900, 640}, 1, 1)...)
	if err != nil {
		return 0, err
	}
	defer a.stop()
	go func() { // keep its output from filling the pipe
		for a.lines.Scan() {
		}
	}()
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		var shown int64
		err := d.next(50*time.Millisecond, func(info *frameInfo, img uintptr) error {
			win, ok := findWindow(a.cmd.Process.Pid)
			if !ok || win.w == 0 {
				return nil
			}
			x, y := win.point(20, 20)
			if x < 0 || y < 0 || x >= d.screenW || y >= d.screenH {
				return nil
			}
			return d.read(img, x, y, 1, 1, func(pix []byte, _ int) {
				if near(pix[2], 0x7b) && near(pix[1], 0x2f) && near(pix[0], 0xf7) {
					shown = info.LastPresentTime
				}
			})
		})
		if err != nil && !errors.Is(err, errNoFrame) {
			return 0, err
		}
		if shown != 0 {
			return float64(shown-t0) / qpcHz * 1e3, nil
		}
	}
	return 0, errors.New("its window's color never showed")
}

func near(a, b byte) bool { return abs(int(a)-int(b)) <= 3 }

// idleRun is what a window showing a frame and waiting costs.
type idleRun struct {
	PrivateMB, WorkingSetMB, CommitMB float64
	CPUShare, GPUShare                float64
}

// measureIdle reads memory six seconds after the app starts, as
// internal/idlemem does, and what it uses over the next twenty.
func measureIdle(sides []framework) (map[string][]idleRun, error) {
	all := map[string][]idleRun{}
	for run := range *runs {
		for i := range sides {
			side := sides[(i+run)%len(sides)]
			r, err := idleOnce(side)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", side.name, err)
			}
			all[side.name] = append(all[side.name], r)
			fmt.Printf("run %d idle %-5s private %.1f MB  working set %.1f MB  commit %.1f MB  cpu %.3f%%  gpu %.3f%%\n",
				run+1, side.name, r.PrivateMB, r.WorkingSetMB, r.CommitMB, r.CPUShare*100, r.GPUShare*100)
		}
	}
	fmt.Println("\n| framework | private working set MB | working set MB | committed MB | CPU % of a core | GPU % |")
	fmt.Println("|---|---|---|---|---|---|")
	for _, side := range sides {
		rs := all[side.name]
		fmt.Printf("| %s | %.1f | %.1f | %.1f | %.3f | %.3f |\n", side.name,
			medianOf(rs, func(r idleRun) float64 { return r.PrivateMB }), medianOf(rs, func(r idleRun) float64 { return r.WorkingSetMB }),
			medianOf(rs, func(r idleRun) float64 { return r.CommitMB }), medianOf(rs, func(r idleRun) float64 { return r.CPUShare * 100 }),
			medianOf(rs, func(r idleRun) float64 { return r.GPUShare * 100 }))
	}
	return all, nil
}

func idleOnce(side framework) (idleRun, error) {
	var r idleRun
	a, err := startApp(side.exe, sceneArgs(scene{"idle", "idle", 0, 900, 640}, 1, 1)...)
	if err != nil {
		return r, err
	}
	defer a.stop()
	win, err := a.waitReady()
	if err != nil {
		return r, err
	}
	if err := checkSize(win, 900, 640); err != nil {
		return r, err
	}
	time.Sleep(6 * time.Second)
	pid := a.cmd.Process.Pid
	mem, err := memoryOf(pid)
	if err != nil {
		return r, err
	}
	r.PrivateMB = float64(mem.PrivateWorkingSetSize) / (1 << 20)
	r.WorkingSetMB = float64(mem.WorkingSetSize) / (1 << 20)
	r.CommitMB = float64(mem.PrivateUsage) / (1 << 20)
	gpu, err := newGPUTime(pid)
	if err != nil {
		return r, err
	}
	defer gpu.close()
	gpu0, err := gpu.seconds()
	if err != nil {
		return r, err
	}
	c0, err := processCycles(pid)
	if err != nil {
		return r, err
	}
	const seconds = 20
	time.Sleep(seconds * time.Second)
	c1, err := processCycles(pid)
	if err != nil {
		return r, err
	}
	gpu1, err := gpu.seconds()
	if err != nil {
		return r, err
	}
	r.CPUShare = float64(c1-c0) / cycleRate / seconds
	r.GPUShare = (gpu1 - gpu0) / seconds
	return r, nil
}

// measureSize returns the sizes of the release builds.
func measureSize(mygoExe, mygoHello, gpuiExe, gpuiHello string) (map[string]int64, error) {
	sizes := map[string]int64{}
	fmt.Println("\n| build | MB |")
	fmt.Println("|---|---|")
	for _, f := range []struct{ name, path string }{
		{"MyGo hello", mygoHello}, {"GPUI hello", gpuiHello},
		{"MyGo scenes", mygoExe}, {"GPUI scenes", gpuiExe},
	} {
		st, err := os.Stat(f.path)
		if err != nil {
			return nil, err
		}
		sizes[f.name] = st.Size()
		fmt.Printf("| %s | %.1f |\n", f.name, float64(st.Size())/(1<<20))
	}
	return sizes, nil
}

// takeShots writes a PNG of each scene on each side, to compare them by eye.
func takeShots(sides []framework) error {
	if *shots == "" {
		return errors.New("-shots names no directory")
	}
	if err := os.MkdirAll(*shots, 0o755); err != nil {
		return err
	}
	type shot struct {
		s     scene
		warm  int
		start time.Duration
	}
	list := []shot{{scene{"idle", "idle", 0, 900, 640}, 1, 300 * time.Millisecond}, {scene{"input-busy", "input-busy", 300, 900, 640}, 30, 0}}
	for _, s := range scenes {
		list = append(list, shot{s, 30, 0})
	}
	for _, sh := range list {
		for _, side := range sides {
			if err := shotOnce(side, sh.s, sh.warm, sh.start); err != nil {
				return fmt.Errorf("%s %s: %w", side.name, sh.s.name, err)
			}
		}
	}
	return nil
}

func shotOnce(side framework, s scene, warm int, settle time.Duration) error {
	a, err := startApp(side.exe, sceneArgs(s, warm, 1<<30)...)
	if err != nil {
		return err
	}
	defer a.stop()
	win, err := a.waitReady()
	if err != nil {
		return err
	}
	fmt.Printf("%-18s %-5s content %d×%d at %d dpi\n", s.name, side.name, win.w, win.h, win.dpi)
	if err := checkSize(win, s.w, s.h); err != nil {
		return err
	}
	time.Sleep(settle + 300*time.Millisecond)
	d, err := newDuplicator()
	if err != nil {
		return err
	}
	defer d.close()
	for {
		done := false
		err := d.next(2*time.Second, func(info *frameInfo, img uintptr) error {
			// The first image a duplication gives may be empty.
			if info.LastPresentTime == 0 {
				return nil
			}
			return d.read(img, win.x, win.y, win.w, win.h, func(pix []byte, pitch int) {
				im := image.NewNRGBA(image.Rect(0, 0, win.w, win.h))
				for y := range win.h {
					row := pix[y*pitch:]
					for x := range win.w {
						o := im.PixOffset(x, y)
						im.Pix[o], im.Pix[o+1], im.Pix[o+2], im.Pix[o+3] = row[x*4+2], row[x*4+1], row[x*4], 255
					}
				}
				f, err := os.Create(filepath.Join(*shots, side.name+"-"+strings.ReplaceAll(s.name, " ", "_")+".png"))
				if err != nil {
					log.Fatal(err)
				}
				defer f.Close()
				if err := png.Encode(f, im); err != nil {
					log.Fatal(err)
				}
				done = true
			})
		})
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

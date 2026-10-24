// Command recorder records a screen or a camera into an MP4 file while it
// happens, then plays the recording back in the same window: MyGo reads the
// pictures through the platform's own capture stack, encodes them with the
// machine's own encoder, and writes the file with its own muxer.
//
//	go run ./examples/recorder
//	go run ./examples/recorder -seconds 5s -out screen.mp4
//	go run ./examples/recorder -source camera
//	go run ./examples/recorder -source camera -id "ASUS FHD webcam"
//	go run ./examples/recorder -list
//
// Close the window to stop recording and quit.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/media"
	"github.com/egoist/mygo/media/mediaui"
	"github.com/egoist/mygo/ui"
)

// askTimeout bounds how long the example waits for the person to answer the
// system's question about the screen or the camera.
const askTimeout = time.Minute

// A recorder records one source to one file, and plays the file back when the
// recording is done.
type recorder struct {
	name  string        // what is being recorded, as a person reads it
	out   string        // where the recording goes
	limit time.Duration // how long to record, or 0 until finished

	ctx      context.Context
	stop     context.CancelFunc
	finished chan struct{} // closed when the recording and playback are done

	mu    sync.Mutex
	win   *mygo.Window
	done  bool
	err   error
	video *mediaui.Video
}

// view builds the window on the main thread: the recording being made, the
// file it made, or why neither happened.
func (r *recorder) view(c *ui.Context) {
	t := c.Theme()
	video, done, err := r.state()
	ui.Column(c).Fill().Padding(12).Gap(10).Children(func() {
		if video != nil {
			ui.Row(c).Grow(1).MinHeight(0).Children(func() {
				video.Element(c).Fill()
			})
		}
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			switch {
			case err != nil:
				ui.Text(c, err.Error()).FontSize(12).TextColor(t.Danger)
			case done:
				ui.Text(c, fmt.Sprintf("saved %s", r.out)).FontSize(12)
			default:
				ui.Text(c, r.recordingLine()).FontSize(12)
				if ui.PrimaryButton(c, "finish").Clicked() {
					r.finish()
				}
			}
		})
	})
}

// recordingLine says what is being recorded, and for how long when the
// recording has a limit.
func (r *recorder) recordingLine() string {
	if r.limit > 0 {
		return fmt.Sprintf("recording %s to %s for %s", r.name, r.out, r.limit)
	}
	return fmt.Sprintf("recording %s to %s", r.name, r.out)
}

// state is what the window draws, taken under the lock because the recording
// happens on another goroutine.
func (r *recorder) state() (*mediaui.Video, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.video, r.done, r.err
}

// finish stops recording early, closing the file where it is.
func (r *recorder) finish() {
	if r.stop != nil {
		r.stop()
	}
}

// window returns the window the recording shows in, if it has been made yet.
func (r *recorder) window() *mygo.Window {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.win
}

// refresh asks the window to build itself again from what the recorder now is.
func (r *recorder) refresh() {
	if win := r.window(); win != nil {
		win.Update(func() {})
	}
}

// run records the source to the file, then plays the file back in the window.
func (r *recorder) run(src media.Source, opts ...media.RecordOption) {
	defer close(r.finished)
	err := media.Record(r.ctx, r.out, src, opts...)
	if err == nil {
		err = r.playBack()
	}
	r.mu.Lock()
	r.err = err
	r.done = true
	r.mu.Unlock()
	r.refresh()
}

// playBack opens the file just recorded and shows it, the way a recording app
// plays what it saved.
func (r *recorder) playBack() error {
	player, err := media.Open(r.out)
	if err != nil {
		return err
	}
	win := r.window()
	if win == nil {
		player.Close()
		return errors.New("the recording has no window to play in")
	}
	var video *mediaui.Video
	win.Update(func() {
		video = mediaui.NewVideo(win, player)
		r.mu.Lock()
		r.video = video
		r.mu.Unlock()
		video.Start()
	})
	return nil
}

// wait waits for the recording and its playback to finish.
func (r *recorder) wait() { <-r.finished }

// close stops the playback and the file, after the window is gone.
func (r *recorder) close() {
	r.mu.Lock()
	video := r.video
	r.mu.Unlock()
	if video != nil {
		video.Stop()
	}
}

// chooseScreen picks the screen to record: the one the id names, by its id or
// by its name, or the first the machine offers when no id is given.
func chooseScreen(screens []media.ScreenInfo, id string) (media.ScreenInfo, error) {
	if len(screens) == 0 {
		return media.ScreenInfo{}, media.ErrNoScreen
	}
	if id == "" {
		return screens[0], nil
	}
	for _, screen := range screens {
		if screen.ID == id || screen.Name == id {
			return screen, nil
		}
	}
	return media.ScreenInfo{}, fmt.Errorf("media: no screen with id %q", id)
}

// chooseCamera picks the camera to record: the one the id names, by its id or
// by its name, or the first the machine offers when no id is given.
func chooseCamera(cameras []media.CameraInfo, id string) (media.CameraInfo, error) {
	if len(cameras) == 0 {
		return media.CameraInfo{}, media.ErrNoCamera
	}
	if id == "" {
		return cameras[0], nil
	}
	for _, camera := range cameras {
		if camera.ID == id || camera.Name == id {
			return camera, nil
		}
	}
	return media.CameraInfo{}, fmt.Errorf("media: no camera with id %q", id)
}

// allowScreen asks for the screen where the system has not answered yet, and
// says where to turn it on where it has said no.
func allowScreen(ctx context.Context) error {
	switch status := media.ScreenPermission(); status {
	case media.PermissionGranted, media.PermissionNotRequired:
		return nil
	case media.PermissionNotDetermined:
		status, err := media.RequestScreenPermission(ctx)
		if err != nil {
			return fmt.Errorf("asking for the screen: %w", err)
		}
		if status == media.PermissionGranted || status == media.PermissionNotRequired {
			return nil
		}
		return fmt.Errorf("media: this app may not capture the screen: %s", status)
	default:
		return fmt.Errorf("media: this app may not capture the screen: %s: turn it on in the system's privacy settings, then run again", status)
	}
}

// allowCamera asks for the camera where the system has not answered yet, and
// says where to turn it on where it has said no.
func allowCamera(ctx context.Context) error {
	switch status := media.CameraPermission(); status {
	case media.PermissionGranted, media.PermissionNotRequired:
		return nil
	case media.PermissionNotDetermined:
		status, err := media.RequestCameraPermission(ctx)
		if err != nil {
			return fmt.Errorf("asking for the camera: %w", err)
		}
		if status == media.PermissionGranted || status == media.PermissionNotRequired {
			return nil
		}
		return fmt.Errorf("media: this app may not use the camera: %s", status)
	default:
		return fmt.Errorf("media: this app may not use the camera: %s: turn it on in the system's privacy settings, then run again", status)
	}
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: %s [-source screen|camera] [-id name] [-out file.mp4] [-seconds 5] [-list]\n", os.Args[0])
		flag.PrintDefaults()
	}
	source := flag.String("source", "screen", `what to record: "screen" or "camera"`)
	id := flag.String("id", "", "the id or name of the screen or camera, the first one by default")
	out := flag.String("out", "recording.mp4", "the file to record into")
	seconds := flag.Duration("seconds", 0, "how long to record; 0 records until finished")
	list := flag.Bool("list", false, "list the screens and cameras this machine offers and exit")
	flag.Parse()

	if *list {
		screens, err := media.Screens()
		if err != nil {
			log.Fatal(err)
		}
		cameras, err := media.Cameras()
		if err != nil {
			log.Fatal(err)
		}
		if len(screens) == 0 {
			fmt.Println("this machine offers no screen")
		}
		for _, screen := range screens {
			fmt.Printf("screen\t%s\t%s\n", screen.ID, screen.Name)
		}
		if len(cameras) == 0 {
			fmt.Println("this machine offers no camera")
		}
		for _, camera := range cameras {
			fmt.Printf("camera\t%s\t%s\n", camera.ID, camera.Name)
		}
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if *seconds > 0 {
		ctx, cancel = context.WithTimeout(ctx, *seconds)
		defer cancel()
	}
	askCtx, askCancel := context.WithTimeout(context.Background(), askTimeout)
	defer askCancel()

	var (
		name string
		src  media.Source
	)
	switch *source {
	case "screen":
		if err := allowScreen(askCtx); err != nil {
			log.Fatal(err)
		}
		screens, err := media.Screens()
		if err != nil {
			log.Fatal(err)
		}
		chosen, err := chooseScreen(screens, *id)
		if err != nil {
			log.Fatal(err)
		}
		screen, err := media.OpenScreen(chosen.ID)
		if err != nil {
			log.Fatal(err)
		}
		name, src = chosen.Name, screen
	case "camera":
		if err := allowCamera(askCtx); err != nil {
			log.Fatal(err)
		}
		cameras, err := media.Cameras()
		if err != nil {
			log.Fatal(err)
		}
		chosen, err := chooseCamera(cameras, *id)
		if err != nil {
			log.Fatal(err)
		}
		camera, err := media.OpenCamera(chosen.ID)
		if err != nil {
			log.Fatal(err)
		}
		name, src = chosen.Name, camera
	default:
		log.Fatalf(`-source takes "screen" or "camera", not %q`, *source)
	}

	r := &recorder{name: name, out: *out, limit: *seconds, ctx: ctx, stop: cancel, finished: make(chan struct{})}
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:   fmt.Sprintf("%s — recorder", name),
			Width:   960,
			Height:  600,
			Content: ui.View(r.view),
		})
		r.mu.Lock()
		r.win = win
		r.mu.Unlock()
		go r.run(src)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	r.finish()
	r.wait()
	r.close()
	src.Close()
}

// Command camera shows what a camera sees in native UI: MyGo reads the
// pictures itself, through the platform's own capture stack, and draws them
// in a window it draws, with no web page and no video element anywhere.
//
//	go run ./examples/camera
//	go run ./examples/camera -camera <id or name>
//	go run ./examples/camera -list
//
// Close the window to stop. The camera is asked for once, when the system has
// not answered yet whether this app may use it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/media"
	"github.com/egoist/mygo/media/mediaui"
	"github.com/egoist/mygo/ui"
)

// askTimeout bounds how long the example waits for the person to answer the
// system's question about the camera.
const askTimeout = time.Minute

// A viewer shows what a camera sees.
type viewer struct {
	name  string
	video *mediaui.Video
}

// view builds the window from the viewer's state, on the main thread,
// whenever the window needs a frame.
func (v *viewer) view(c *ui.Context) {
	if v.video == nil {
		// The window can ask for its first frame before the video
		// exists: the window is made, then the video that draws into it.
		return
	}
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Grow(1).MinHeight(0).Padding(8).Children(func() {
			v.video.Element(c).Fill()
		})
		ui.Row(c).Padding(8, 12).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, v.name).FontSize(12)
			switch err := v.video.Err(); {
			case err == nil:
				ui.Text(c, "live").FontSize(12).TextColor(t.TextMuted)
			case errors.Is(err, media.ErrEnd):
				ui.Text(c, "the camera stopped").FontSize(12).TextColor(t.TextMuted)
			default:
				ui.Text(c, err.Error()).FontSize(12).TextColor(t.Danger)
			}
			ui.Text(c, "close the window to stop").FontSize(12).TextColor(t.TextMuted)
		})
	})
}

// chooseCamera picks the camera to open: the one the id names, by its id or by
// its name, or the first the machine offers when no id is given.
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
		fmt.Fprintf(flag.CommandLine.Output(), "usage: %s [-camera id] [-list]\n", os.Args[0])
		flag.PrintDefaults()
	}
	id := flag.String("camera", "", "the id or name of the camera to open, the first one by default")
	list := flag.Bool("list", false, "list the cameras this machine offers and exit")
	flag.Parse()

	cameras, err := media.Cameras()
	if err != nil {
		log.Fatal(err)
	}
	if *list {
		if len(cameras) == 0 {
			fmt.Println("this machine offers no camera")
		}
		for _, camera := range cameras {
			fmt.Printf("%s\t%s\n", camera.ID, camera.Name)
		}
		return
	}
	chosen, err := chooseCamera(cameras, *id)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), askTimeout)
	defer cancel()
	if err := allowCamera(ctx); err != nil {
		log.Fatal(err)
	}
	camera, err := media.OpenCamera(chosen.ID)
	if err != nil {
		log.Fatal(err)
	}
	v := &viewer{name: chosen.Name}
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:   fmt.Sprintf("%s — camera", chosen.Name),
			Width:   960,
			Height:  600,
			Content: ui.View(v.view),
		})
		v.video = mediaui.NewVideo(win, camera)
		v.video.Start()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	if v.video != nil {
		v.video.Stop()
	}
	camera.Close()
}

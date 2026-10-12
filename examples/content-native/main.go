// Content-native demonstrates the optional image, PDF and video components.
//
// go run ./examples/content-native -pdfium /path/libpdfium.dylib -video movie.mp4
package main

import (
	"context"
	_ "embed"
	"flag"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/imageview"
	"github.com/egoist/mygo/plugins/pdf"
	"github.com/egoist/mygo/plugins/video"
	"github.com/egoist/mygo/ui"
	"image"
	"image/color"
	"log"
	"os"
)

//go:embed sample.pdf
var samplePDF []byte

type example struct {
	tab, query       string
	image            *imageview.Viewer
	doc              *pdf.Document
	pdf              *pdf.Viewer
	video            *video.Player
	pdfErr, videoErr error
	cancelSearch     context.CancelFunc
}

func (s *example) view(c *ui.Context) {
	ui.Column(c).Fill().Padding(12).Gap(10).Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			for _, tab := range []string{"Image", "PDF", "Video"} {
				if ui.Button(c, tab).Clicked() {
					s.tab = tab
				}
			}
		})
		switch s.tab {
		case "Image":
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c, "Fit").Clicked() {
					s.image.SetFit(imageview.FitContain)
				}
				if ui.Button(c, "Actual size").Clicked() {
					s.image.SetFit(imageview.FitActual)
				}
				if ui.Button(c, "Rotate").Clicked() {
					s.image.Rotate(1)
				}
				ui.Text(c, "Drag to pan; Ctrl/Command + wheel to zoom")
			})
			imageview.View(c, s.image).Grow(1)
		case "PDF":
			if s.pdfErr != nil {
				ui.Text(c, s.pdfErr.Error())
				return
			}
			if s.doc.Capabilities().Search {
				ui.Row(c).Gap(8).Children(func() {
					ui.TextInput(c, &s.query).Placeholder("Find in PDF").Grow(1)
					if ui.Button(c, "Find").Clicked() {
						if s.cancelSearch != nil {
							s.cancelSearch()
						}
						ctx, cancel := context.WithCancel(context.Background())
						s.cancelSearch = cancel
						query := s.query
						go func() { _, _ = s.pdf.Find(ctx, query, false) }()
					}
					if ui.Button(c, "Previous match").Clicked() {
						s.pdf.NextMatch(-1)
					}
					if ui.Button(c, "Next match").Clicked() {
						s.pdf.NextMatch(1)
					}
				})
			}
			pdf.View(c, s.pdf).Grow(1)
		case "Video":
			if s.videoErr != nil {
				ui.Text(c, s.videoErr.Error())
				return
			}
			video.View(c, s.video).Grow(1)
		}
	})
}
func (s *example) close() {
	if s.cancelSearch != nil {
		s.cancelSearch()
	}
	s.image.Close()
	if s.pdf != nil {
		s.pdf.Close()
	}
	if s.video != nil {
		s.video.Close()
	}
}
func main() {
	imagePath := flag.String("image", "", "image file; generated pattern by default")
	pdfPath := flag.String("pdf", "", "PDF file; bundled two-page sample by default")
	videoPath := flag.String("video", "", "local video path or direct media URL")
	pdfium := flag.String("pdfium", os.Getenv("MYGO_PDFIUM_LIBRARY"), "PDFium native library path")
	mpv := flag.String("mpv", os.Getenv("MYGO_MPV_LIBRARY"), "libmpv native library path")
	flag.Parse()
	s := &example{tab: "Image", image: imageview.New()}
	if *imagePath != "" {
		if err := s.image.Open(*imagePath); err != nil {
			log.Print(err)
		}
	} else {
		m := image.NewRGBA(image.Rect(0, 0, 800, 600))
		for y := 0; y < 600; y++ {
			for x := 0; x < 800; x++ {
				m.SetRGBA(x, y, color.RGBA{uint8(x * 255 / 800), uint8(y * 255 / 600), uint8((x/40+y/40)%2*150 + 50), 255})
			}
		}
		_ = s.image.SetImage(m)
	}
	if *pdfPath != "" {
		s.doc, s.pdfErr = pdf.Open(*pdfPath, pdf.Options{Library: *pdfium})
	} else {
		s.doc, s.pdfErr = pdf.Load(samplePDF, pdf.Options{Library: *pdfium})
	}
	if s.pdfErr == nil {
		s.pdf, s.pdfErr = pdf.NewViewer(s.doc)
	}
	s.video, s.videoErr = video.New(video.Options{Library: *mpv})
	if s.videoErr == nil && *videoPath != "" {
		s.videoErr = s.video.Load(*videoPath)
	}
	mygo.App.WhenReady(func() {
		w := mygo.NewWindow(mygo.WindowOptions{Title: "Native content", Width: 960, Height: 720, Content: ui.View(s.view)})
		w.OnClosed(s.close)
	})
	if err := mygo.App.Run(); err != nil {
		log.Print(err)
	}
	s.close()
	if s.pdf != nil {
		<-s.pdf.Done()
	}
	if s.doc != nil {
		_ = s.doc.Close()
	}
	if s.video != nil {
		<-s.video.Done()
	}
}

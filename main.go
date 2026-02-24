// Package main creates a simple video player using the gstreamer library wrapped by Simon Dassow.

package main

import (
	"errors"
	"image/color"
	"log"
	"os"
	"path/filepath"

	"codeberg.org/sdassow/fyne-gstreamer"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	width  = 1280
	height = 720
)

// player holds all the data
// necessary for playing video.
type player struct {
	pix    *gstreamer.Video
	paused bool
}

// Starts reading the media file.
func (p *player) open(fname string) error {
	path := storage.NewFileURI(fname)

	// Open the media file.
	media, err := gstreamer.NewVideo(path)
	if err != nil {
		return err
	}
	p.pix = media

	if yes, err := storage.Exists(path); !yes || err != nil {
		if err != nil {
			return err
		}

		return errors.New("file does not exist")
	}

	return nil
}

func main() {
	if len(os.Args) == 1 {
		log.Println("Please specify a video file to play (e.g. myVid.mp4)")
		return
	}

	a := app.New()
	p := &player{}
	path, _ := filepath.Abs(os.Args[1])
	name := filepath.Base(path)
	err := p.open(path)

	w := a.NewWindow("Video Player")
	w.Resize(fyne.NewSize(width/2, height/2))
	w.SetPadded(false)
	w.SetOnClosed(func() {
		_ = p.pix.Stop()
	})

	autoPlay := true
	if err != nil {
		dialog.ShowError(err, w)
		autoPlay = false
	}

	p.paused = !autoPlay
	var play *widget.Button
	play = widget.NewButtonWithIcon("", theme.MediaPauseIcon(), func() {
		p.paused = !p.paused
		if p.paused {
			_ = p.pix.Pause()
			play.SetIcon(theme.MediaPlayIcon())
		} else {
			err = p.pix.Play()
			if err != nil {
				dialog.ShowError(err, w)
			} else {
				play.SetIcon(theme.MediaPauseIcon())
			}
		}
	})
	play.Importance = widget.LowImportance
	if p.paused {
		play.SetIcon(theme.MediaPlayIcon())
	}

	bg := canvas.NewRectangle(color.NRGBA{R: 0x42, G: 0x42, B: 0x42, A: 0xaa})
	bg.CornerRadius = theme.InputRadiusSize() * 1.5
	buttons := container.NewCenter(container.NewStack(
		bg,
		container.NewPadded(container.NewHBox(
			widget.NewLabel(name),
			play,
		))))
	space := canvas.NewRectangle(color.Transparent)
	space.SetMinSize(fyne.NewSize(48, 48))
	controls := container.NewBorder(nil, container.NewVBox(buttons, space), nil, nil)

	if autoPlay {
		// Start decoding streams.
		err = p.pix.Play()

		if err != nil {
			dialog.ShowError(err, w)
		}
	}

	w.SetContent(container.NewStack(p.pix, controls))
	w.ShowAndRun()
}

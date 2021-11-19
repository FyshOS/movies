// Package main creates a simple video player using the reisen library for accessing the data.
// It is derived from the excellent example at https://github.com/zergon321/reisen/blob/master/examples/player/main.go

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
	"github.com/zergon321/reisen"
)

const (
	width                             = 1280
	height                            = 720
	frameBufferSize                   = 1024
	sampleRate                        = 44100
	channelCount                      = 2
	bitDepth                          = 8
	sampleBufferSize                  = 32 * channelCount * bitDepth * 1024
	SpeakerSampleRate beep.SampleRate = 44100
)

// readVideoAndAudio reads video and audio frames
// from the opened media and sends the decoded
// data to che channels to be played.
func (p *player) readVideoAndAudio(media *reisen.Media) (<-chan *image.RGBA, <-chan [2]float64, chan error, error) {
	frameBuffer := make(chan *image.RGBA, frameBufferSize)
	sampleBuffer := make(chan [2]float64, sampleBufferSize)
	errs := make(chan error)

	err := media.OpenDecode()

	if err != nil {
		return nil, nil, nil, err
	}

	videoStream := media.VideoStreams()[0]
	err = videoStream.Open()

	if err != nil {
		return nil, nil, nil, err
	}

	audioStream := media.AudioStreams()[0]
	err = audioStream.Open()

	if err != nil {
		return nil, nil, nil, err
	}

	go func() {
		for {
			packet, gotPacket, err := media.ReadPacket()

			if err != nil {
				go func(err error) {
					errs <- err
				}(err)
			}

			if !gotPacket {
				break
			}

			switch packet.Type() {
			case reisen.StreamVideo:
				s := media.Streams()[packet.StreamIndex()].(*reisen.VideoStream)
				videoFrame, gotFrame, err := s.ReadVideoFrame()

				if err != nil {
					go func(err error) {
						errs <- err
					}(err)
				}

				if !gotFrame {
					break
				}

				if videoFrame == nil {
					continue
				}

				frameBuffer <- videoFrame.Image()

			case reisen.StreamAudio:
				s := media.Streams()[packet.StreamIndex()].(*reisen.AudioStream)
				audioFrame, gotFrame, err := s.ReadAudioFrame()

				if err != nil {
					go func(err error) {
						errs <- err
					}(err)
				}

				if !gotFrame {
					break
				}

				if audioFrame == nil {
					continue
				}

				// Turn the raw byte data into
				// audio samples of type [2]float64.
				reader := bytes.NewReader(audioFrame.Data())

				// See the README.md file for
				// detailed scheme of the sample structure.
				for reader.Len() > 0 {
					sample := [2]float64{0, 0}
					var result float64
					err = binary.Read(reader, binary.LittleEndian, &result)

					if err != nil {
						go func(err error) {
							errs <- err
						}(err)
					}

					sample[0] = result

					err = binary.Read(reader, binary.LittleEndian, &result)

					if err != nil {
						go func(err error) {
							errs <- err
						}(err)
					}

					sample[1] = result
					sampleBuffer <- sample
				}
			}
		}

		videoStream.Close()
		audioStream.Close()
		media.CloseDecode()
		close(frameBuffer)
		close(sampleBuffer)
		close(errs)
	}()

	return frameBuffer, sampleBuffer, errs, nil
}

// streamSamples creates a new custom streamer for
// playing audio samples provided by the source channel.
//
// See https://github.com/faiface/beep/wiki/Making-own-streamers
// for reference.
func (p *player) streamSamples(sampleSource <-chan [2]float64) beep.Streamer {
	return beep.StreamerFunc(func(samples [][2]float64) (n int, ok bool) {
		numRead := 0

		for i := 0; i < len(samples); i++ {
			if p.paused {
				time.Sleep(time.Millisecond * 8)
				i--
				continue
			}
			sample, ok := <-sampleSource

			if !ok {
				numRead = i + 1
				break
			}

			samples[i] = sample
			numRead++
		}

		if numRead < len(samples) {
			return numRead, false
		}

		return numRead, true
	})
}

// player holds all the data
// necessary for playing video.
type player struct {
	pix         *image.NRGBA
	ticker      <-chan time.Time
	errs        <-chan error
	frameBuffer <-chan *image.RGBA
	last        time.Time
	deltaTime   float64
	paused      bool
}

// Starts reading samples and frames
// of the media file.
func (p *player) open(fname string) error {
	// Initialize the audio speaker.
	err := speaker.Init(sampleRate,
		SpeakerSampleRate.N(time.Second/10))

	if err != nil {
		return err
	}

	// Sprite for drawing video frames.
	p.pix = image.NewNRGBA(image.Rect(0, 0, width, height))

	if err != nil {
		return err
	}

	// Open the media file.
	media, err := reisen.NewMedia(fname)

	if err != nil {
		return err
	}

	// Get the FPS for playing
	// video frames.
	videoFPS, _ := media.Streams()[0].FrameRate()
	if videoFPS == 0 {
		videoFPS = 60
	}

	if err != nil {
		return err
	}

	// SPF for frame ticker.
	spf := 1.0 / float64(videoFPS)
	frameDuration, err := time.
		ParseDuration(fmt.Sprintf("%fs", spf))

	if err != nil {
		return err
	}

	// Start decoding streams.
	var sampleSource <-chan [2]float64
	p.frameBuffer, sampleSource,
		p.errs, err = p.readVideoAndAudio(media)

	if err != nil {
		return err
	}

	// Start playing audio samples.
	speaker.Play(p.streamSamples(sampleSource))

	p.ticker = time.Tick(frameDuration)

	// Setup metrics.
	p.last = time.Now()
	return nil
}

func (p *player) update(screen *canvas.Image) error {
	// Compute dt.
	p.deltaTime = time.Since(p.last).Seconds()
	p.last = time.Now()

	// Check for incoming errors.
	select {
	case err, ok := <-p.errs:
		if ok {
			return err
		}

	default:
	}

	// Read video frames and draw them.
	select {
	case <-p.ticker:
		frame, ok := <-p.frameBuffer

		if ok {
			p.pix.Pix = frame.Pix
			screen.Refresh()
		}

	default:
	}

	return nil
}

func main() {
	if len(os.Args) == 1 {
		log.Println("Please specify a video file to play (.mp4, .mkv)")
		return
	}

	p := &player{}
	path, _ := filepath.Abs(os.Args[1])
	name := filepath.Base(path)
	err := p.open(path)

	a := app.New()
	w := a.NewWindow("Video Player")
	w.Resize(fyne.NewSize(width/2, height/2))
	w.SetPadded(false)

	if err != nil {
		dialog.ShowError(err, w)
	}

	var play *widget.Button
	play = widget.NewButtonWithIcon("", theme.MediaPauseIcon(), func() {
		p.paused = !p.paused
		if p.paused {
			play.SetIcon(theme.MediaPlayIcon())
		} else {
			play.SetIcon(theme.MediaPauseIcon())
		}
	})

	bg := canvas.NewRectangle(color.NRGBA{R: 0x42, G: 0x42, B: 0x42, A: 0x66})
	buttons := container.NewCenter(container.NewMax(bg,
		container.NewHBox(
			widget.NewLabel(name),
			play,
		)))
	space := canvas.NewRectangle(color.Transparent)
	space.SetMinSize(fyne.NewSize(48, 48))
	controls := container.NewBorder(nil, container.NewVBox(buttons, space), nil, nil)

	i := canvas.NewImageFromImage(p.pix)
	i.ScaleMode = canvas.ImageScaleFastest
	w.SetContent(container.NewMax(i, controls))

	go func() {
		for {
			if p.paused {
				time.Sleep(time.Millisecond * 8)
				continue
			}

			err = p.update(i)
			if err != nil {
				log.Println("Error playing:", err)
			}
		}
	}()
	w.ShowAndRun()
}

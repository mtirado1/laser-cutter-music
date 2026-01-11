package main

import (
	"fmt"
	"gitlab.com/gomidi/midi/v2/smf"
	"go.bug.st/serial"
	"log"
	"math"
	"os"
	"strings"
)

// Change these constants as they will vary across machines
// (mm / m) / Hz
const frequencyConstant = 4 / 1.337
// XY Movement range
const size = 100.0 // mm




type tone struct {
	key    uint8
	silent bool
}

func (t tone) frequency() float64 {
	if t.silent {
		return 0
	}
	return 440 / 32 * math.Pow(2, (float64(t.key)-9)/12)
}

type note struct {
	tone
	duration uint64
}

type motorNote struct {
	toneX    tone
	toneY    tone
	duration uint64
}

func generateGCode(music []motorNote, frequencyConstant float64, size float64, tickDuration float64) []string {
	var switchX float64 = 1
	var switchY float64 = 1
	var x float64 = 0
	var y float64 = 0

	commands := []string{
		"G17", "G40", "G54", // XY plane, compensation off, Coordinate system
		"G91", // Relative movement
		"G21", // Metric mode (mm)
		"M8",  // Flood coolant ON
		"M5",  // Spindle OFF
		"M3",  // Spindle ON
	}

	for _, note := range music {
		var fx float64 = 0
		var fy float64 = 0

		// duration in minutes
		duration := float64(note.duration) * tickDuration / 60000

		// Frequencies in Hz, 0 means no sound
		fx = note.toneX.frequency()
		fy = note.toneY.frequency()

		if fx == 0 && fy == 0 {
			// grbl, delay in seconds
			commands = append(commands, fmt.Sprintf("G4 P%v", duration*60))
			continue
		}

		vx := fx * frequencyConstant // mm / min
		vy := fy * frequencyConstant

		v := math.Sqrt(vx*vx + vy*vy)

		// Displacement
		dx := vx * duration
		dy := vy * duration

		xDimensionOverflow := (switchX == 1 && (x+dx*switchX > size)) || (switchX == -1 && (x+dx*switchX < 0))
		yDimensionOverflow := (switchY == 1 && (y+dy*switchY > size)) || (switchY == -1 && (y+dy*switchY < 0))

		if xDimensionOverflow {
			switchX *= -1
		}

		if yDimensionOverflow {
			switchY *= -1
		}

		commands = append(commands, fmt.Sprintf("G1 X%v Y%v S100 F%v", dx*switchX, dy*switchY, v))
		x += dx * switchX
		y += dy * switchY
	}

	// Laser OFF, Go to (0,0), End
	commands = append(commands, "M5", fmt.Sprintf("G0 X%v Y%v", -x, -y), "M2")
	return commands
}

func main() {

	// Read a file
	s := smf.New()

	// Parse MIDI
	if len(os.Args) < 2 {
		fmt.Println("Error: Provide a file name.")
		return
	}

	file, err := os.Open(os.Args[1])

	if err != nil {
		fmt.Printf("File ERROR: %s\n", err.Error())
		return
	}

	defer file.Close()

	s, err = smf.ReadFrom(file)

	if err != nil {
		fmt.Printf("ERROR: %s\n", err.Error())
		return
	}

	var ppq float64 = 96

	switch mt := s.TimeFormat.(type) {
	case smf.MetricTicks:
		ppq = float64(mt)
	}

	convertedTracks := [][]note{}

	var bpm float64
	for _, track := range s.Tracks {
		notes := []note{}

		var channel uint8
		var key, velocity uint8

		for _, ev := range track {
			msg := ev.Message

			if msg.Type() == smf.MetaEndOfTrackMsg {
				// ignore
				continue
			}

			switch {
			case msg.GetMetaTempo(&bpm): // Set the tempo
			case msg.GetNoteOn(&channel, &key, &velocity):
				if uint64(ev.Delta) > 0 {
					if velocity == 0 {
						notes = append(notes, note{
							duration: uint64(ev.Delta),
							tone:     tone{key: key},
						})
					} else {
						notes = append(notes, note{
							duration: uint64(ev.Delta),
							tone:     tone{silent: true},
						})
					}
				}
			case msg.GetNoteOff(&channel, &key, &velocity):
				if uint64(ev.Delta) > 0 {
					notes = append(notes, note{
						duration: uint64(ev.Delta),
						tone:     tone{key: key},
					})
				}
			}
		}

		if len(notes) > 0 {
			convertedTracks = append(convertedTracks, notes)
		}
	}

	tickDuration := 60000 / (bpm * ppq)

	if len(convertedTracks) < 2 {
		fmt.Println("Need at least two tracks with notes.")
		return
	}

	// Combine the two channels

	music := []motorNote{}
	ax := convertedTracks[0]
	ay := convertedTracks[1]
	ix := 0
	iy := 0

	for ix < len(ax) || iy < len(ay) {
		if ix == len(ax) {
			music = append(music, motorNote{
				toneX:    tone{key: 0, silent: true},
				toneY:    ay[iy].tone,
				duration: ay[iy].duration,
			})
			iy += 1
			continue
		}

		if iy == len(ay) {
			music = append(music, motorNote{
				toneX:    ax[ix].tone,
				toneY:    tone{key: 0, silent: true},
				duration: ax[ix].duration,
			})
			ix += 1
			continue
		}

		dx := ax[ix].duration
		dy := ay[iy].duration

		if dx <= dy {
			music = append(music, motorNote{
				toneX:    ax[ix].tone,
				toneY:    ay[iy].tone,
				duration: dx,
			})

			ay[iy].duration -= dx
			if ay[iy].duration == 0 {
				iy += 1
			}
			ix += 1
		} else {
			music = append(music, motorNote{
				toneX:    ax[ix].tone,
				toneY:    ay[iy].tone,
				duration: dy,
			})

			ax[ix].duration -= dy
			if ax[ix].duration == 0 {
				ix += 1
			}
			iy += 1
		}
	}

	// Converting music to G-Code



	commands := generateGCode(music, frequencyConstant, size, tickDuration)

	// Send through serial port

	mode := &serial.Mode{
		BaudRate: 115200,
	}

	device := ""
	if len(os.Args) >= 3 {
		device = os.Args[2]
	} else {
		fmt.Println(strings.Join(commands, "\n"))
		return
	}

	port, err := serial.Open(device, mode)
	if err != nil {
		log.Fatal(err)
	}

	for _, command := range commands {
		_, err := port.Write([]byte(command + "\n\r"))
		if err != nil {
			log.Fatal(err)
		}

		buff := make([]byte, 100)
		for {
			n, err := port.Read(buff)
			if err != nil {
				log.Fatal(err)
			}

			if n == 0 {
				break
			}

			if strings.Contains(string(buff[:n]), "\n") {
				break
			}
		}
	}
}

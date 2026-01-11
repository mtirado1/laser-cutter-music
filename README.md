## Description

Convert MIDI files to grbl G-Code.

This program converts a MIDI files into G-Code for 2-axis CNC machines.

Some caveats:

* MIDI file needs at least two tracks with notes, one for each axis.
* Only one note can be played at a time per axis.
* Spindle / laser will be **turned ON**.

## Usage

First update `main.go` to update the constants for your machine.

Create a file with G-Code.

```
go build
./laser-music file.mid > commands.gcode
```

Send to a serial device.

```
go build
./laser-music file.mid /dev/ttyUSB0
```

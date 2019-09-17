package ui

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// The states of the ANSI escape code parser.
const (
	ansiText = iota
	ansiEscape
	ansiSubstring
	ansiControlSequence
)

// ansi is a io.Writer which translates ANSI escape codes into tview color
// tags.
type ansi struct {
	io.Writer

	// Reusable buffers.
	buffer                        *bytes.Buffer // The entire output text of one Write().
	csiParameter, csiIntermediate *bytes.Buffer // Partial CSI strings.

	// The current state of the parser. One of the ansi constants.
	state int
}

// ANSIWriter returns an io.Writer which translates any ANSI escape codes
// written to it into tview color tags. Other escape codes don't have an effect
// and are simply removed. The translated text is written to the provided
// writer.
func ANSIWriter(writer io.Writer) io.Writer {
	return &ansi{
		Writer:          writer,
		buffer:          new(bytes.Buffer),
		csiParameter:    new(bytes.Buffer),
		csiIntermediate: new(bytes.Buffer),
		state:           ansiText,
	}
}

// Write parses the given text as a string of runes, translates ANSI escape
// codes to color tags and writes them to the output writer.
func (a *ansi) Write(text []byte) (int, error) {
	defer func() {
		a.buffer.Reset()
	}()

	for _, r := range string(text) {
		switch a.state {

		// We just entered an escape sequence.
		case ansiEscape:
			switch r {
			case '[': // Control Sequence Introducer.
				a.csiParameter.Reset()
				a.csiIntermediate.Reset()
				a.state = ansiControlSequence
			case 'c': // Reset.
				fmt.Fprint(a.buffer, "[-:-:-]")
				a.state = ansiText
			case 'P', ']', 'X', '^', '_': // Substrings and commands.
				a.state = ansiSubstring
			default: // Ignore.
				a.state = ansiText
			}

		// CSI Sequences.
		case ansiControlSequence:
			switch {
			case r >= 0x30 && r <= 0x3f: // Parameter bytes.
				if _, err := a.csiParameter.WriteRune(r); err != nil {
					return 0, err
				}
			case r >= 0x20 && r <= 0x2f: // Intermediate bytes.
				if _, err := a.csiIntermediate.WriteRune(r); err != nil {
					return 0, err
				}
			case r >= 0x40 && r <= 0x7e: // Final byte.
				switch r {
				case 'E': // Next line.
					count, _ := strconv.Atoi(a.csiParameter.String())
					if count == 0 {
						count = 1
					}
					fmt.Fprint(a.buffer, strings.Repeat("\n", count))
				case 'm': // Select Graphic Rendition.
					var (
						background, foreground, attributes           string
						bold, dim, italic, underline, blink, reverse *bool
					)
					codes := strings.Split(a.csiParameter.String(), ";")
					codesNum := len(codes)
					for index := 0; index < codesNum; index++ {
						code, err := strconv.Atoi(codes[index])
						if err != nil {
							continue
						}
						switch code {
						case 0:
							background = "-"
							foreground = "-"
							attributes = "-"
							bold = nil
							dim = nil
							italic = nil
							underline = nil
							blink = nil
							reverse = nil
						case 1:
							bold = boolTrue()
						case 2:
							dim = boolTrue()
						case 3:
							italic = boolTrue()
						case 4:
							underline = boolTrue()
						case 5:
							blink = boolTrue()
						case 7:
							reverse = boolTrue()
						case 21:
							bold = boolFalse()
						case 22:
							dim = boolFalse()
						case 23:
							italic = boolFalse()
						case 24:
							underline = boolFalse()
						case 25:
							blink = boolFalse()
						case 27:
							reverse = boolFalse()
						case 30, 31, 32, 33, 34, 35, 36, 37:
							isBright := false
							if bold != nil && *bold {
								isBright = true
							}
							foreground = lookupColor(code-30, isBright)
						case 40, 41, 42, 43, 44, 45, 46, 47:
							background = lookupColor(code-40, false)
						case 90, 91, 92, 93, 94, 95, 96, 97:
							bold = boolTrue()
							foreground = lookupColor(code-90, true)
						case 100, 101, 102, 103, 104, 105, 106, 107:
							bold = boolTrue()
							background = lookupColor(code-100, true)
						case 38, 48:
							var color string

							if codes[index+1] == "5" && codesNum > index+2 { // 8-bit colors.
								index += 2
								color = resolve8bitsColor(codes[index+2])
							} else if codes[index+1] == "2" && len(codes) > index+4 { // 24-bit colors.
								index += 4
								color = resolve24bitsColor(codes[index+2], codes[index+3], codes[index+4])
							}

							if len(color) > 0 {
								if code == 38 {
									foreground = color
								} else {
									background = color
								}
							}
						}
					}
					attributes2 := makeAttr(bold, dim, italic, underline, blink, reverse)
					if len(attributes2) > 0 {
						if attributes == "-" {
							attributes = "BDULR" + attributes2
						} else {
							attributes = attributes2
						}
					}
					if len(foreground) > 0 || len(background) > 0 || len(attributes) > 0 {
						fmt.Fprintf(a.buffer, "[%s:%s:%s]", foreground, background, attributes)
					}
				}
				a.state = ansiText
			default: // Undefined byte.
				a.state = ansiText // Abort CSI.
			}

			// We just entered a substring/command sequence.
		case ansiSubstring:
			if r == 27 { // Most likely the end of the substring.
				a.state = ansiEscape
			} // Ignore all other characters.

			// "ansiText" and all others.
		default:
			if r == 27 {
				// This is the start of an escape sequence.
				a.state = ansiEscape
			} else {
				// Just a regular rune. Send to buffer.
				if _, err := a.buffer.WriteRune(r); err != nil {
					return 0, err
				}
			}
		}
	}

	// Write buffer to target writer.
	n, err := a.buffer.WriteTo(a.Writer)
	if err != nil {
		return int(n), err
	}
	return len(text), nil
}

func lookupColor(colorNumber int, isBright bool) string {
	if colorNumber < 0 || colorNumber > 7 {
		return "black"
	}
	if isBright {
		colorNumber += 8
	}
	return [...]string{
		"black",
		"red",
		"green",
		"yellow",
		"blue",
		"darkmagenta",
		"darkcyan",
		"white",
		"#7f7f7f",
		"#ff0000",
		"#00ff00",
		"#ffff00",
		"#5c5cff",
		"#ff00ff",
		"#00ffff",
		"#ffffff",
	}[colorNumber]
}

func resolve8bitsColor(code string) string {
	colorNumber, _ := strconv.Atoi(code)
	color := ""
	if colorNumber <= 7 {
		color = lookupColor(colorNumber, false)
	} else if colorNumber <= 15 {
		color = lookupColor(colorNumber, true)
	} else if colorNumber <= 231 {
		red := (colorNumber - 16) / 36
		green := ((colorNumber - 16) / 6) % 6
		blue := (colorNumber - 16) % 6
		color = fmt.Sprintf("#%02x%02x%02x", 255*red/5, 255*green/5, 255*blue/5)
	} else if colorNumber <= 255 {
		grey := 255 * (colorNumber - 232) / 23
		color = fmt.Sprintf("#%02x%02x%02x", grey, grey, grey)
	}
	return color
}

func resolve24bitsColor(red, green, blue string) string {
	r, _ := strconv.Atoi(red)
	g, _ := strconv.Atoi(green)
	b, _ := strconv.Atoi(blue)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// TranslateANSI replaces ANSI escape sequences found in the provided string
// with tview's color tags and returns the resulting string.
func TranslateANSI(text string) string {
	var buffer bytes.Buffer
	writer := ANSIWriter(&buffer)
	writer.Write([]byte(text))
	return buffer.String()
}

func boolTrue() *bool  { b := true; return &b }
func boolFalse() *bool { b := false; return &b }

func makeAttr(bold, dim, italic, underline, blink, reverse *bool) string {
	attr := ""
	if bold != nil {
		if *bold {
			attr += "b"
		} else {
			attr += "B"
		}
	}
	if dim != nil {
		if *dim {
			attr += "d"
		} else {
			attr += "D"
		}
	}
	if underline != nil {
		if *underline {
			attr += "u"
		} else {
			attr += "U"
		}
	}
	if blink != nil {
		if *blink {
			attr += "l"
		} else {
			attr += "L"
		}
	}
	if reverse != nil {
		if *reverse {
			attr += "r"
		} else {
			attr += "R"
		}
	}

	return attr
}
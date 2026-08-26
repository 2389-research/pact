// ABOUTME: Renders the deterministic PACT globe and wax seal used by the CLI welcome.
// ABOUTME: Keeps geography, shading, color, and terminal sizing independent of animation.
package main

import (
	"fmt"
	"math"
	"strings"
)

const (
	sealTargetGlobeWidth  = 40
	sealMinimumGlobeWidth = 12
	sealWidthAllowance    = 6
	sealLineAllowance     = 9
)

func sealGlobeWidth(terminalWidth, terminalHeight int) (int, bool) {
	width := min(sealTargetGlobeWidth, terminalWidth-sealWidthAllowance, 2*(terminalHeight-sealLineAllowance))
	if width%2 != 0 {
		width--
	}
	if width < sealMinimumGlobeWidth {
		return 0, false
	}
	return width, true
}

func renderSealFrame(globeWidth int, spin float64, color bool) []string {
	lines := globe(globeWidth, spin, color)
	return append(lines, plaque(globeWidth+sealWidthAllowance, color)...)
}

func sealFrameText(globeWidth int, spin float64, color bool) string { //nolint:unparam // Width remains part of the renderer contract for scaled frames.
	return strings.Join(renderSealFrame(globeWidth, spin, color), "\n") + "\n"
}

// ---------------------------------------------------------------- geography

type box struct{ lonMin, lonMax, latMin, latMax float64 }

// A deliberately coarse Earth. Boxes, not coastlines -- at 40 columns the
// terminal can't tell the difference and this keeps the binary dependency-free.
var landmass = []box{
	// North America
	{-168, -141, 55, 71}, // Alaska
	{-141, -60, 50, 70},  // Canada
	{-120, -62, 68, 78},  // Arctic islands
	{-125, -67, 30, 50},  // United States
	{-100, -80, 25, 31},  // Gulf coast
	{-115, -86, 16, 32},  // Mexico
	{-93, -77, 8, 18},    // Central America
	{-55, -20, 60, 83},   // Greenland
	{-24, -14, 63, 67},   // Iceland
	// South America
	{-80, -50, -5, 12},
	{-78, -35, -18, -4},
	{-73, -40, -30, -17},
	{-73, -53, -40, -29},
	{-75, -64, -55, -39},
	// Europe
	{-10, 30, 37, 60},
	{5, 32, 55, 71},  // Scandinavia
	{-10, 2, 50, 59}, // Britain & Ireland
	{20, 50, 45, 65},
	// Africa
	{-17, 33, 15, 33},
	{-17, 30, 4, 16},
	{8, 42, -12, 5},
	{38, 51, 3, 12},
	{12, 40, -28, -11},
	{16, 33, -35, -27},
	{43, 50, -25, -12}, // Madagascar
	// Asia
	{30, 180, 50, 75},
	{60, 140, 42, 52},
	{26, 75, 30, 50},
	{35, 60, 13, 32},   // Arabia
	{68, 89, 8, 32},    // India
	{75, 122, 22, 45},  // China
	{95, 110, 5, 23},   // Indochina
	{130, 146, 31, 46}, // Japan
	// Oceania
	{95, 141, -10, 6},    // Indonesia
	{118, 127, 5, 19},    // Philippines
	{131, 151, -11, -1},  // New Guinea
	{113, 154, -39, -11}, // Australia
	{166, 179, -47, -34}, // New Zealand
	// Antarctica
	{-180, 180, -90, -63},
}

// Punched out after the fact, so a few inland seas survive the boxes.
var water = []box{
	{-95, -78, 51, 63}, // Hudson Bay
	{28, 43, 13, 29},   // Red Sea
	{48, 57, 24, 30},   // Persian Gulf
}

func isLand(lat, lon float64) bool {
	for _, b := range water {
		if lon >= b.lonMin && lon <= b.lonMax && lat >= b.latMin && lat <= b.latMax {
			return false
		}
	}
	for _, b := range landmass {
		if lon >= b.lonMin && lon <= b.lonMax && lat >= b.latMin && lat <= b.latMax {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- rendering

var (
	// Deliberately disjoint alphabets: any of -=+*#%@ is land, any of .:~ is
	// water. Brightness alone can't be trusted to tell them apart in mono.
	landRamp = []rune("+*##%%@@")
	seaRamp  = []rune(" ....::~")

	// light points up-left-toward-viewer, so the terminator sweeps as it spins.
	lx, ly, lz = norm3(-0.55, 0.42, 0.72)
)

func norm3(x, y, z float64) (float64, float64, float64) {
	m := math.Sqrt(x*x + y*y + z*z)
	return x / m, y / m, z / m
}

// shade quantizes brightness to a ramp index. Quantizing before coloring is
// what lets neighboring cells share an escape sequence.
func shade(v float64, ramp []rune) int {
	i := max(int(v*float64(len(ramp))), 0)
	if i >= len(ramp) {
		i = len(ramp) - 1
	}
	return i
}

func rgb(r, g, b float64) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", clamp255(r), clamp255(g), clamp255(b))
}

func clamp255(v float64) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return int(v)
}

// rimRune picks a line-art character matching the slope of the circle's edge.
func rimRune(dx, dy float64) rune {
	a := math.Atan2(dy, dx) * 180 / math.Pi // dy is screen-down
	if a < 0 {
		a += 360
	}
	switch {
	case a < 22.5 || a >= 337.5:
		return '|'
	case a < 67.5:
		return '/'
	case a < 112.5:
		return '-'
	case a < 157.5:
		return '\\'
	case a < 202.5:
		return '|'
	case a < 247.5:
		return '/'
	case a < 292.5:
		return '-'
	default:
		return '\\'
	}
}

// globe renders the seal medallion: spinning Earth plus wax rim.
func globe(w int, spin float64, color bool) []string {
	h := w / 2
	W, H := w+6, h+4
	cx, cy := float64(W-1)/2, float64(H-1)/2
	rx, ry := float64(w)/2, float64(h)/2
	// one-and-a-bit cells thick, whatever the size
	rimInner, rimOuter := 1.0, 1.0+1.7/rx

	lines := make([]string, 0, H)
	for y := range H {
		var sb strings.Builder
		cur := "" // last escape sequence written, so runs share one
		put := func(c string, r rune) {
			if c == "" {
				c = "\x1b[0m"
			}
			if color && c != cur {
				sb.WriteString(c)
				cur = c
			}
			sb.WriteRune(r)
		}
		for x := range W {
			dx := (float64(x) - cx) / rx
			dy := (float64(y) - cy) / ry
			r2 := dx*dx + dy*dy

			switch {
			case r2 > rimOuter*rimOuter:
				put("", ' ')

			case r2 >= rimInner*rimInner: // wax rim
				put(rgb(198, 64, 50), rimRune(dx, dy))

			default: // the sphere itself
				z := math.Sqrt(math.Max(0, 1-r2))
				px, py, pz := dx, -dy, z

				lat := math.Asin(py) * 180 / math.Pi
				lon := math.Atan2(px, pz)*180/math.Pi + spin
				lon = math.Mod(math.Mod(lon+180, 360)+360, 360) - 180

				lit := px*lx + py*ly + pz*lz
				if lit < 0 {
					lit = 0
				}
				lit = 0.20 + 0.80*lit // ambient, so the night side isn't a hole

				if isLand(lat, lon) {
					i := shade(lit, landRamp)
					t := float64(i+1) / float64(len(landRamp))
					put(rgb(25+70*t, 30+190*t, 25+95*t), landRamp[i])
				} else {
					i := shade(lit, seaRamp)
					t := float64(i+1) / float64(len(seaRamp))
					put(rgb(10+38*t, 18+96*t, 40+205*t), seaRamp[i])
				}
			}
		}
		line := strings.TrimRight(sb.String(), " ")
		if color && cur != "" && cur != "\x1b[0m" {
			line += "\x1b[0m"
		}
		lines = append(lines, line)
	}
	return lines
}

// ---------------------------------------------------------------- the plaque

func plaque(width int, color bool) []string {
	inner := min(20, width-2)
	pad := func(s string) string {
		if len(s) > inner {
			s = s[:inner]
		}
		left := (inner - len(s)) / 2
		return strings.Repeat(" ", left) + s + strings.Repeat(" ", inner-len(s)-left)
	}

	ribbon := strings.Repeat("~", (inner-1)/2) + "|" + strings.Repeat("~", inner-1-(inner-1)/2)
	raw := []string{
		ribbon,
		" " + strings.Repeat("_", inner) + " ",
		"|" + pad("P  A  C  T") + "|",
		"|" + pad("signed & sealed") + "|",
		"|" + strings.Repeat("_", inner) + "|",
	}

	out := make([]string, 0, len(raw))
	for i, s := range raw {
		indent := strings.Repeat(" ", max(0, (width-len(s))/2))
		if color {
			c := rgb(196, 62, 48)
			if i >= 2 {
				c = rgb(214, 198, 160)
			}
			s = c + s + "\x1b[0m"
		}
		out = append(out, indent+s)
	}
	return out
}

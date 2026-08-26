// ABOUTME: Emits the finite animated seal used only by PACT's bare interactive welcome.
// ABOUTME: Owns cursor-line replacement and returns every write failure without changing cursor modes.
package main

import (
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	sealAnimationFrames   = 16
	sealAnimationInterval = 60 * time.Millisecond
	sealAnimationStep     = 6.0
)

func renderBareWelcome(writer io.Writer, config runConfig, display presentation) error {
	if config.StdoutTerminal && config.AnimationFrames == 0 {
		return nil
	}
	globeWidth, ok := sealGlobeWidth(config.Width, config.Height)
	if !ok {
		return nil
	}
	color := colorEnabled(display, config, config.StdoutTerminal)
	if !config.StdoutTerminal || config.Environment["TERM"] == "dumb" || config.AnimationFrames <= 1 {
		_, err := io.WriteString(writer, sealFrameText(globeWidth, 0, color)+"\n")
		return err
	}
	if err := emitSealAnimation(writer, globeWidth, color, config.AnimationFrames, config.AnimationInterval); err != nil {
		return err
	}
	_, err := io.WriteString(writer, "\n")
	return err
}

func emitSealAnimation(writer io.Writer, globeWidth int, color bool, frames int, interval time.Duration) error {
	if frames < 1 {
		frames = 1
	}
	lineCount := globeWidth/2 + sealLineAllowance
	for frame := 0; frame < frames; frame++ {
		if frame > 0 {
			if interval > 0 {
				time.Sleep(interval)
			}
			if _, err := fmt.Fprintf(writer, "\x1b[%dA", lineCount); err != nil {
				return err
			}
		}
		lines := renderSealFrame(globeWidth, float64(frame)*sealAnimationStep, color)
		if frame == 0 {
			if _, err := io.WriteString(writer, strings.Join(lines, "\n")+"\n"); err != nil {
				return err
			}
			continue
		}
		for _, line := range lines {
			if _, err := fmt.Fprintf(writer, "\r\x1b[2K%s\n", line); err != nil {
				return err
			}
		}
	}
	return nil
}

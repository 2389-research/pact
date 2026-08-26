// ABOUTME: Starts the PACT command-line program and delegates argument handling to the app.
// ABOUTME: Keeps process exit behavior out of the testable command adapter.
package main

import (
	"os"

	"golang.org/x/term"
)

func main() {
	width := 80
	height := 30
	if detectedWidth, detectedHeight, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		if detectedWidth > 0 {
			width = detectedWidth
		}
		if detectedHeight > 0 {
			height = detectedHeight
		}
	}
	os.Exit(runWithConfig(os.Args[1:], runConfig{
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, WorkingDir: workingDirectory(),
		StdoutTerminal: term.IsTerminal(int(os.Stdout.Fd())), StderrTerminal: term.IsTerminal(int(os.Stderr.Fd())),
		Width: width, Height: height, AnimationFrames: sealAnimationFrames, AnimationInterval: sealAnimationInterval,
		Environment: environmentMap(os.Environ()),
	}))
}

func workingDirectory() string {
	workingDir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return workingDir
}

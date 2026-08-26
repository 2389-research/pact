// ABOUTME: Starts the PACT command-line program and delegates argument handling to the app.
// ABOUTME: Keeps process exit behavior out of the testable command adapter.
package main

import (
	"os"
	"time"

	"golang.org/x/term"
)

func main() {
	stdoutTerminal := term.IsTerminal(int(os.Stdout.Fd()))
	detectedWidth, detectedHeight, geometryError := term.GetSize(int(os.Stdout.Fd()))
	width, height, animationFrames := terminalAnimationConfig(stdoutTerminal, detectedWidth, detectedHeight, geometryError)
	os.Exit(runWithConfig(os.Args[1:], runConfig{
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, WorkingDir: workingDirectory(),
		StdinTerminal: term.IsTerminal(int(os.Stdin.Fd())), StdoutTerminal: stdoutTerminal, StderrTerminal: term.IsTerminal(int(os.Stderr.Fd())),
		Width: width, Height: height, AnimationFrames: animationFrames, AnimationInterval: sealAnimationInterval,
		Now: time.Now, Environment: environmentMap(os.Environ()),
	}))
}

func terminalAnimationConfig(stdoutTerminal bool, detectedWidth, detectedHeight int, geometryError error) (int, int, int) {
	if geometryError != nil || detectedWidth <= 0 || detectedHeight <= 0 {
		if stdoutTerminal {
			return 80, 30, 0
		}
		return 80, 30, sealAnimationFrames
	}
	return detectedWidth, detectedHeight, sealAnimationFrames
}

func workingDirectory() string {
	workingDir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return workingDir
}

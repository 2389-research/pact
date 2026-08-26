// ABOUTME: Starts the PACT command-line program and delegates argument handling to the app.
// ABOUTME: Keeps process exit behavior out of the testable command adapter.
package main

import (
	"os"

	"golang.org/x/term"
)

func main() {
	width := 80
	if detectedWidth, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && detectedWidth > 0 {
		width = detectedWidth
	}
	os.Exit(runWithConfig(os.Args[1:], runConfig{
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, WorkingDir: workingDirectory(),
		StdoutTerminal: term.IsTerminal(int(os.Stdout.Fd())), StderrTerminal: term.IsTerminal(int(os.Stderr.Fd())),
		Width: width, Environment: environmentMap(os.Environ()),
	}))
}

func workingDirectory() string {
	workingDir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return workingDir
}

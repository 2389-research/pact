// ABOUTME: Tests the finite animated seal shown only for PACT's bare interactive welcome.
// ABOUTME: Proves fallback, color, cursor, and writer-failure contracts through the command adapter.
package main

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBarePactAnimatesThenPrintsHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runWithConfig(nil, runConfig{
		Stdout: &stdout, Stderr: &stderr, StdoutTerminal: true,
		Width: 80, Height: 30, AnimationFrames: 3,
	})
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("bare pact = (%d, %q)", code, stderr.String())
	}
	if strings.Count(stdout.String(), "\x1b[29A") != 2 {
		t.Fatalf("animation cursor ownership = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "signed & sealed") || !strings.Contains(stdout.String(), "Usage: pact COMMAND") {
		t.Fatalf("animated help = %q", stdout.String())
	}
}

func TestBarePactFallbacksAndExplicitHelp(t *testing.T) {
	for _, test := range []struct {
		name       string
		args       []string
		config     runConfig
		wantSeal   bool
		wantCursor bool
	}{
		{name: "redirected static", config: runConfig{Width: 80, Height: 30}, wantSeal: true},
		{name: "TTY unknown geometry", config: runConfig{StdoutTerminal: true, Width: 80, Height: 30, AnimationFrames: 0}},
		{name: "TERM dumb static", config: runConfig{StdoutTerminal: true, Width: 80, Height: 30, AnimationFrames: 3, Environment: map[string]string{"TERM": "dumb"}}, wantSeal: true},
		{name: "JSON no seal", args: []string{"--json"}, config: runConfig{StdoutTerminal: true, Width: 80, Height: 30, AnimationFrames: 3}},
		{name: "explicit help", args: []string{"help"}, config: runConfig{StdoutTerminal: true, Width: 80, Height: 30, AnimationFrames: 3}},
		{name: "explicit flag help", args: []string{"--help"}, config: runConfig{StdoutTerminal: true, Width: 80, Height: 30, AnimationFrames: 3}},
		{name: "too small", config: runConfig{StdoutTerminal: true, Width: 17, Height: 15, AnimationFrames: 3}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			test.config.Stdout, test.config.Stderr = &stdout, &stderr
			if code := runWithConfig(test.args, test.config); code != 0 || stderr.Len() != 0 {
				t.Fatalf("help = (%d, %q)", code, stderr.String())
			}
			if got := strings.Contains(stdout.String(), "signed & sealed"); got != test.wantSeal {
				t.Fatalf("seal = %v, want %v: %q", got, test.wantSeal, stdout.String())
			}
			cursorControl := strings.Contains(stdout.String(), "\x1b[29A") || strings.Contains(stdout.String(), "\x1b[2K")
			if cursorControl != test.wantCursor {
				t.Fatalf("cursor control = %v, want %v: %q", cursorControl, test.wantCursor, stdout.String())
			}
		})
	}
}

func TestTerminalAnimationConfig(t *testing.T) {
	geometryError := errors.New("terminal geometry unavailable")
	for _, test := range []struct {
		name                          string
		stdoutTerminal                bool
		detectedWidth, detectedHeight int
		geometryError                 error
		wantWidth, wantHeight         int
		wantFrames                    int
	}{
		{name: "geometry error", stdoutTerminal: true, geometryError: geometryError, wantWidth: 80, wantHeight: 30},
		{name: "zero width", stdoutTerminal: true, detectedHeight: 30, wantWidth: 80, wantHeight: 30},
		{name: "zero height", stdoutTerminal: true, detectedWidth: 80, wantWidth: 80, wantHeight: 30},
		{name: "valid dimensions", stdoutTerminal: true, detectedWidth: 100, detectedHeight: 40, wantWidth: 100, wantHeight: 40, wantFrames: sealAnimationFrames},
	} {
		t.Run(test.name, func(t *testing.T) {
			width, height, frames := terminalAnimationConfig(test.stdoutTerminal, test.detectedWidth, test.detectedHeight, test.geometryError)
			if width != test.wantWidth || height != test.wantHeight || frames != test.wantFrames {
				t.Fatalf("terminal animation config = (%d, %d, %d), want (%d, %d, %d)", width, height, frames, test.wantWidth, test.wantHeight, test.wantFrames)
			}
		})
	}
}

func TestBarePactSealColorPrecedence(t *testing.T) {
	for _, test := range []struct {
		name      string
		args      []string
		config    runConfig
		wantColor bool
	}{
		{name: "terminal auto", config: runConfig{StdoutTerminal: true, Width: 80, Height: 30, AnimationFrames: 1}, wantColor: true},
		{name: "redirected auto", config: runConfig{Width: 80, Height: 30, AnimationFrames: 1}},
		{name: "NO_COLOR", config: runConfig{StdoutTerminal: true, Width: 80, Height: 30, AnimationFrames: 1, Environment: map[string]string{"NO_COLOR": "1"}}},
		{name: "TERM dumb", config: runConfig{StdoutTerminal: true, Width: 80, Height: 30, AnimationFrames: 1, Environment: map[string]string{"TERM": "dumb"}}},
		{name: "forced always", args: []string{"--color", "always"}, config: runConfig{Width: 80, Height: 30, AnimationFrames: 1}, wantColor: true},
		{name: "forced never", args: []string{"--color", "never"}, config: runConfig{StdoutTerminal: true, Width: 80, Height: 30, AnimationFrames: 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			test.config.Stdout, test.config.Stderr = &stdout, &stderr
			if code := runWithConfig(test.args, test.config); code != 0 || stderr.Len() != 0 {
				t.Fatalf("bare pact = (%d, %q)", code, stderr.String())
			}
			if got := strings.Contains(stdout.String(), "\x1b["); got != test.wantColor {
				t.Fatalf("seal color = %v, want %v: %q", got, test.wantColor, stdout.String())
			}
		})
	}
}

func TestProductionSealAnimationIsFinite(t *testing.T) {
	for _, globeWidth := range []int{40, 12} {
		t.Run("width "+strconv.Itoa(globeWidth), func(t *testing.T) {
			var output bytes.Buffer
			if err := emitSealAnimation(&output, globeWidth, false, sealAnimationFrames, 0); err != nil {
				t.Fatal(err)
			}
			if got := strings.Count(output.String(), "signed & sealed"); got != sealAnimationFrames {
				t.Fatalf("seal frames = %d, want %d", got, sealAnimationFrames)
			}
			rewind := "\x1b[" + strconv.Itoa(globeWidth/2+sealLineAllowance) + "A"
			if got := strings.Count(output.String(), rewind); got != sealAnimationFrames-1 {
				t.Fatalf("cursor rewinds = %d, want %d", got, sealAnimationFrames-1)
			}
			wantRepaints := (sealAnimationFrames - 1) * (globeWidth/2 + sealLineAllowance)
			if got := strings.Count(output.String(), "\r\x1b[2K"); got != wantRepaints {
				t.Fatalf("line repaints = %d, want %d", got, wantRepaints)
			}
		})
	}
}

func TestBarePactSealWriteFailureReturnsUnexpectedExit(t *testing.T) {
	var stderr bytes.Buffer
	code := runWithConfig(nil, runConfig{Stdout: closedOutput{}, Stderr: &stderr, Width: 80, Height: 30})
	if code != exitUnexpectedError || !strings.Contains(stderr.String(), "help output failed") {
		t.Fatalf("seal writer failure = (%d, %q)", code, stderr.String())
	}
}

func TestBarePactCursorWriteFailureReturnsUnexpectedExit(t *testing.T) {
	stdout := &failAfterWriter{remaining: 1}
	var stderr bytes.Buffer
	code := runWithConfig(nil, runConfig{
		Stdout: stdout, Stderr: &stderr, StdoutTerminal: true,
		Width: 80, Height: 30, AnimationFrames: 2,
	})
	if code != exitUnexpectedError || !strings.Contains(stderr.String(), "help output failed") {
		t.Fatalf("seal cursor writer failure = (%d, %q)", code, stderr.String())
	}
}

func TestProductionSealAnimationContract(t *testing.T) {
	if sealAnimationFrames != 16 || sealAnimationInterval != 60*time.Millisecond || sealAnimationStep != 6.0 {
		t.Fatalf("animation contract = (%d, %s, %v)", sealAnimationFrames, sealAnimationInterval, sealAnimationStep)
	}
}

var _ io.Writer = closedOutput{}

type failAfterWriter struct {
	remaining int
}

func (writer *failAfterWriter) Write(value []byte) (int, error) {
	if writer.remaining == 0 {
		return 0, errors.New("closed output")
	}
	writer.remaining--
	return len(value), nil
}

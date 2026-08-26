// ABOUTME: Pins the deterministic PACT globe-and-wax-seal frame and terminal size policy.
// ABOUTME: Proves color is optional decoration and small terminals skip art instead of cropping it.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSealFrameMatchesPlainGolden(t *testing.T) {
	got := sealFrameText(40, 0, false)
	want, err := os.ReadFile(filepath.Join("testdata", "help", "seal-40.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("seal frame:\n%s", got)
	}
}

func TestSealColorStripsToPlainFrame(t *testing.T) {
	plain := sealFrameText(40, 0, false)
	colored := sealFrameText(40, 0, true)
	if !strings.Contains(colored, "\x1b[") {
		t.Fatalf("colored seal lacks ANSI: %q", colored)
	}
	if got := stripANSI(colored); got != plain {
		t.Fatalf("stripped colored frame differs from plain:\n%s", got)
	}
}

func TestPlainSealKeepsLandAndWaterDistinct(t *testing.T) {
	frame := sealFrameText(40, 0, false)
	if !strings.ContainsAny(frame, "+*#%@") {
		t.Fatalf("seal lacks land alphabet: %q", frame)
	}
	if !strings.ContainsAny(frame, ".:") {
		t.Fatalf("seal lacks water alphabet: %q", frame)
	}
}

func TestSealGlobeWidthFitsTerminal(t *testing.T) {
	for _, test := range []struct {
		name          string
		width, height int
		want          int
		wantOK        bool
	}{
		{name: "full", width: 80, height: 30, want: 40, wantOK: true},
		{name: "short", width: 80, height: 24, want: 30, wantOK: true},
		{name: "narrow", width: 30, height: 30, want: 24, wantOK: true},
		{name: "too small", width: 17, height: 15, want: 0, wantOK: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := sealGlobeWidth(test.width, test.height)
			if got != test.want || ok != test.wantOK {
				t.Fatalf("sealGlobeWidth(%d, %d) = (%d, %v), want (%d, %v)", test.width, test.height, got, ok, test.want, test.wantOK)
			}
		})
	}
}

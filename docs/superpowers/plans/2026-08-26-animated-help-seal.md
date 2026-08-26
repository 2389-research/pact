<!-- ABOUTME: Breaks the animated bare-PACT welcome into deterministic rendering, finite motion, and real CLI verification. -->
<!-- ABOUTME: Preserves instant explicit help, terminal safety, catalog ownership, and writer-error contracts. -->

# PACT Animated Help Seal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the rotating globe-and-wax-seal welcome from
`~/Downloads/temp2` to a bare `pact` invocation while keeping explicit help
instant and redirected output deterministic.

**Architecture:** Port the prototype's dependency-free globe, shading, rim,
and plaque into one deterministic frame renderer. A separate finite animation
writer owns cursor movement and timing. The standard-library adapter remains
the only help router and invokes the welcome only when no command remains
after global presentation parsing.

**Tech Stack:** Go 1.26, standard-library math and timing packages, existing
`golang.org/x/term v0.45.0`, catalog-backed help, golden tests, compiled-binary
E2E tests, and `scripts/check`.

## Global Constraints

- Run Go and hook-bearing git commands through
  `env -u GOROOT mise exec -- ...`; this machine's inherited `GOROOT` is stale.
- Follow strict TDD for each behavior slice and retain raw RED/GREEN evidence.
- Use `apply_patch` for edits, run `git status` before staging, never use
  `git add -A`, and never bypass hooks.
- Hand-written Go and Markdown files start with two `ABOUTME` lines.
- Port no standalone prototype flags, infinite loop, screen clear, signal
  handler, or second `main` function.
- Bare TTY animation is exactly 16 frames, 60 milliseconds apart, advancing
  six degrees per frame.
- Never hide the cursor or leave a terminal mode changed after exit.
- `pact help`, `pact --help`, command help, and nested help remain instant and
  seal-free.
- Redirected bare output uses one deterministic frame; `--json` uses existing
  help without a seal.
- Existing color precedence remains authoritative, and ANSI-stripped color
  output equals plain output byte-for-byte.
- Every frame and cursor-control write propagates failure as the existing
  `help output failed` diagnostic and exit code 10.
- No repository discovery, canonical write, index write, prompt, or network
  action may occur on any help path.
- Add no dependency. The command catalog remains the sole help and dispatch
  inventory.
- Do not touch the deferred setup branches or worktrees.

## File and Responsibility Map

- Create `cmd/pact/seal_render.go`: geography, shading, plaque, safe size, and
  one deterministic frame.
- Create `cmd/pact/seal_render_test.go`: frame golden, color equivalence,
  character semantics, and size bounds.
- Create `cmd/pact/testdata/help/seal-40.golden`: exact plain prototype frame
  at longitude zero.
- Create `cmd/pact/seal_animation.go`: finite frame emission, cursor-up line
  ownership, optional delay, and static fallback.
- Create `cmd/pact/seal_animation_test.go`: finite animation bytes, trigger
  matrix, fallback, and failed-writer coverage.
- Modify `cmd/pact/stdlib_adapter.go`: recognize the bare welcome after global
  presentation parsing and route animation failures through help diagnostics.
- Modify `cmd/pact/main.go`: retain terminal height and provide production
  frame timing.
- Modify `tests/e2e/operator_contract_test.go`: distinguish branded bare help
  from seal-free explicit help in real compiled processes.
- Modify `README.md`: document the animated bare welcome and static redirect
  fallback.
- Modify `gotchas.md`: record that motion belongs only to bare `pact` and
  explicit help must stay instant.

---

### Task 1: Port the Deterministic Seal Frame

**Files:**

- Create: `cmd/pact/seal_render.go`
- Create: `cmd/pact/seal_render_test.go`
- Create: `cmd/pact/testdata/help/seal-40.golden`

**Interfaces:**

- Consumes: the geography, ramps, lighting, rim, and plaque behavior in
  `/Users/harper/Downloads/temp2/main.go`.
- Produces:

```go
func sealGlobeWidth(terminalWidth, terminalHeight int) (int, bool)
func renderSealFrame(globeWidth int, spin float64, color bool) []string
func sealFrameText(globeWidth int, spin float64, color bool) string
```

- `sealFrameText` returns exactly one trailing newline.
- The number of frame lines is `globeWidth/2 + 9`.

- [ ] **Step 1: Write failing renderer and size tests**

Create `cmd/pact/seal_render_test.go`:

```go
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
```

- [ ] **Step 2: Run the focused tests and observe RED**

Run:

```bash
env -u GOROOT mise exec -- go test ./cmd/pact -run '^TestSeal' -count=1
```

Expected: compilation fails because `sealFrameText` and `sealGlobeWidth` do
not exist.

- [ ] **Step 3: Add the exact plain golden**

Create `cmd/pact/testdata/help/seal-40.golden` from the exact bytes produced by:

```bash
cd /Users/harper/Downloads/temp2
env -u GOROOT mise exec -- go run . -once -plain -w 40
```

The committed golden begins with the prototype's empty outer row, contains
`P  A  C  T` and `signed & sealed`, and ends with one newline. Do not use a
shell redirect to create it; add the captured bytes with `apply_patch`.

- [ ] **Step 4: Port the minimal renderer**

Create `cmd/pact/seal_render.go`. Copy the exact `box`, `landmass`, `water`,
`isLand`, ramps, light vector, `norm3`, `shade`, `rgb`, `clamp255`, `rimRune`,
`globe`, and `plaque` definitions from
`/Users/harper/Downloads/temp2/main.go`. Keep their domain comments and add the
required file header. Omit only the prototype's flag declarations, `main`,
signal handling, ticker, screen clear, and infinite driver.

Correct one prototype-only ANSI detail while porting: append a row's final
reset only when its current color is neither empty nor already `\x1b[0m`.
This lets `strings.TrimRight(..., " ")` remove outer padding before any needed
reset and makes ANSI-stripped colored rows exactly equal to plain rows. The
golden and `TestSealColorStripsToPlainFrame` pin that output contract.

Add this exact size and frame boundary around the ported functions:

```go
const (
	sealTargetGlobeWidth = 40
	sealMinimumGlobeWidth = 12
	sealWidthAllowance = 6
	sealLineAllowance = 9
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

func sealFrameText(globeWidth int, spin float64, color bool) string {
	return strings.Join(renderSealFrame(globeWidth, spin, color), "\n") + "\n"
}
```

Use the existing built-in `min` and `max`; do not add duplicate helpers.

- [ ] **Step 5: Run focused and package tests**

Run:

```bash
env -u GOROOT mise exec -- go test ./cmd/pact -run '^TestSeal' -count=1
env -u GOROOT mise exec -- go test ./cmd/pact -count=1
```

Expected: PASS with no warnings.

- [ ] **Step 6: Commit Task 1**

Run:

```bash
git status --short
git add cmd/pact/seal_render.go cmd/pact/seal_render_test.go cmd/pact/testdata/help/seal-40.golden
env -u GOROOT mise exec -- git commit -m "feat: add static PACT seal renderer"
```

Expected: hooks pass and only the three Task 1 files are committed.

---

### Task 2: Animate Only the Bare Welcome

**Files:**

- Create: `cmd/pact/seal_animation.go`
- Create: `cmd/pact/seal_animation_test.go`
- Modify: `cmd/pact/stdlib_adapter.go`
- Modify: `cmd/pact/main.go`

**Interfaces:**

- Consumes: `sealGlobeWidth`, `sealFrameText`, `presentation`, `runConfig`,
  `colorEnabled`, and `renderHelp`.
- Adds to `runConfig`:

```go
Height            int
AnimationFrames   int
AnimationInterval time.Duration
```

- Produces:

```go
const sealAnimationFrames = 16
const sealAnimationInterval = 60 * time.Millisecond
const sealAnimationStep = 6.0

func renderBareWelcome(writer io.Writer, config runConfig, display presentation) error
func emitSealAnimation(writer io.Writer, globeWidth int, color bool, frames int, interval time.Duration) error
```

- [ ] **Step 1: Write failing finite-motion and trigger tests**

Create `cmd/pact/seal_animation_test.go` with the required `ABOUTME` header and
these tests:

```go
package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
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
		name string
		args []string
		config runConfig
		wantSeal bool
		wantCursor bool
	}{
		{name: "redirected static", config: runConfig{Width: 80, Height: 30}, wantSeal: true},
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

func TestBarePactSealColorPrecedence(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		config runConfig
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
	var output bytes.Buffer
	if err := emitSealAnimation(&output, 40, false, sealAnimationFrames, 0); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(output.String(), "signed & sealed"); got != sealAnimationFrames {
		t.Fatalf("seal frames = %d, want %d", got, sealAnimationFrames)
	}
	if got := strings.Count(output.String(), "\x1b[29A"); got != sealAnimationFrames-1 {
		t.Fatalf("cursor rewinds = %d, want %d", got, sealAnimationFrames-1)
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
```

Import `errors` and `time` in the real test file. The fallback table
deliberately expects no cursor movement; color checks use a one-frame path so
ANSI style escapes cannot be confused with cursor-control escapes.

- [ ] **Step 2: Run the tests and observe RED**

Run:

```bash
env -u GOROOT mise exec -- go test ./cmd/pact -run 'TestBarePact|TestProductionSealAnimation' -count=1
```

Expected: compilation fails because the new run configuration fields and
animation constants do not exist.

- [ ] **Step 3: Add the finite animation writer**

Create `cmd/pact/seal_animation.go`:

```go
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
	sealAnimationFrames = 16
	sealAnimationInterval = 60 * time.Millisecond
	sealAnimationStep = 6.0
)

func renderBareWelcome(writer io.Writer, config runConfig, display presentation) error {
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
```

- [ ] **Step 4: Thread process dimensions and timing through `runConfig`**

In `cmd/pact/stdlib_adapter.go`, import `time` and add the three fields from the
Interfaces block. In `normalizedRunConfig`, default `Height` to 30; leave
`AnimationFrames` and `AnimationInterval` unchanged so in-process callers must
opt into motion.

After `parsePresentation` and before the general `isHelpRequest` branch, record:

```go
bareWelcome := len(args) == 0 && !display.asJSON
```

Inside the existing help branch, before `renderHelp`, add:

```go
if bareWelcome {
	if err := renderBareWelcome(config.Stdout, config, display); err != nil {
		return writeFailure(config.Stderr, display.asJSON, "help output failed")
	}
}
```

Do not alter `renderHelp` or create a second help inventory.

In `cmd/pact/main.go`, retain both dimensions from `term.GetSize`. Default to
width 80 and height 30. Populate:

```go
Height: height,
AnimationFrames: sealAnimationFrames,
AnimationInterval: sealAnimationInterval,
```

- [ ] **Step 5: Run focused tests and fix only contract mismatches**

Run:

```bash
env -u GOROOT mise exec -- go test ./cmd/pact -run 'TestBarePact|TestProductionSealAnimation|TestSeal|TestCatalogHelp' -count=1
env -u GOROOT mise exec -- go test ./cmd/pact -count=1
```

Expected: PASS. The focused suite must complete without a production-duration
sleep because its animation interval is zero.

- [ ] **Step 6: Run race and writer gates**

Run:

```bash
env -u GOROOT mise exec -- go test -race ./cmd/pact
env -u GOROOT mise exec -- go test ./cmd/pact -run 'TestOperatorWriterFailures|TestBarePact.*WriteFailure' -count=1
```

Expected: PASS with no warnings.

- [ ] **Step 7: Commit Task 2**

Run:

```bash
git status --short
git add cmd/pact/seal_animation.go cmd/pact/seal_animation_test.go cmd/pact/stdlib_adapter.go cmd/pact/main.go
env -u GOROOT mise exec -- git commit -m "feat: animate bare PACT welcome"
```

---

### Task 3: Freeze the Compiled Help Contract and Document It

**Files:**

- Modify: `tests/e2e/operator_contract_test.go`
- Modify: `README.md`
- Modify: `gotchas.md`

**Interfaces:**

- Consumes: the compiled `pact` binary and the existing
  `runOperatorProcess`/`assertCleanHelp` helpers.
- Produces: a black-box distinction between branded bare help and instant
  explicit help.

- [ ] **Step 1: Add compiled-binary contract assertions**

In `runOperatorCLIContract`, retain separate results instead of passing calls
inline:

```go
bareHelp := runOperatorProcess(t, binary, workspace, nil)
assertCleanHelp(t, bareHelp)
if !strings.Contains(bareHelp.Stdout, "signed & sealed") || strings.Contains(bareHelp.Stdout, "\x1b[") {
	 t.Fatalf("bare help branding = %#v", bareHelp)
}

for _, args := range [][]string{{"--help"}, {"help"}, {"status", "--help"}} {
	explicitHelp := runOperatorProcess(t, binary, workspace, nil, args...)
	assertCleanHelp(t, explicitHelp)
	if strings.Contains(explicitHelp.Stdout, "signed & sealed") || strings.Contains(explicitHelp.Stdout, "\x1b[") {
		t.Fatalf("explicit help branding for %q = %#v", args, explicitHelp)
	}
}
```

- [ ] **Step 2: Run the compiled contract**

Run:

```bash
env -u GOROOT mise exec -- go test ./tests/e2e -run TestOperatorCLIContract -count=1
```

Expected: PASS because Task 2 already implements the behavior. Task 2's
in-process test supplied the RED proof before implementation; this step freezes
the same contract at the compiled-process boundary. Never weaken the shared
help assertions to make the seal pass.

- [ ] **Step 3: Document the shipped welcome and durable preference**

Replace the first paragraph under `Operator CLI foundation` with:

```markdown
Running bare `pact` in a terminal plays a brief rotating signed-and-sealed
welcome, then prints top-level help. Redirected bare output prints one static
frame, plain under the default automatic color policy. `pact help` and
`pact --help` skip the animation and remain immediate. `pact help index` shows
the nested index commands, and `pact COMMAND --help` remains seal-free.
```

Replace the current animated-help bullet in `gotchas.md` with this fuller
version without editing other bullets:

```markdown
- The animated seal belongs only to bare `pact`. Explicit `pact help` and
  `pact --help` must remain immediate; redirected bare output gets one static
  frame and follows the normal color policy.
```

- [ ] **Step 4: Run final automated verification**

Run:

```bash
env -u GOROOT mise exec -- go test ./...
env -u GOROOT mise exec -- go test -race ./cmd/pact ./internal/status ./tests/e2e
env -u GOROOT mise exec -- go vet ./...
env -u GOROOT mise exec -- ./scripts/check
```

Expected: every command exits zero, no test is skipped, and the Python suite
reports 18 passing tests.

- [ ] **Step 5: Build and dogfood the real terminal path**

First prove `/tmp/pact-animated-help` does not exist. Build:

```bash
env -u GOROOT mise exec -- go build -o /tmp/pact-animated-help ./cmd/pact
```

Run `/tmp/pact-animated-help` in a real PTY from the repository root. Confirm:

- motion completes in less than one second;
- no full-screen clear or cursor hide sequence appears;
- the final seal stays above help;
- the cursor remains usable after exit;
- `pact help` and `pact --help` are immediate and seal-free.

Then verify redirected behavior:

```bash
/tmp/pact-animated-help > /tmp/pact-animated-help.out
```

Inspect the exact scratch output for one `signed & sealed`, one `Usage:`, and
no ANSI escape. Remove only the two exact scratch files after checking their
types and prove both are absent.

- [ ] **Step 6: Request broad review and fix valid findings with TDD**

Review the exact design-base-to-HEAD diff for terminal corruption, infinite
motion, help/JSON drift, writer loss, size arithmetic, timing in tests, and
duplicate help state. Any Critical or Important finding receives a focused
failing test before its fix and a rereview.

- [ ] **Step 7: Commit Task 3**

Run:

```bash
git status --short
git add tests/e2e/operator_contract_test.go README.md gotchas.md
env -u GOROOT mise exec -- git commit -m "docs: document animated PACT welcome"
git status --short --branch
```

Expected: hooks pass and the worktree is clean.

## Plan Self-Review

- [x] Spec coverage: bare-only motion, static redirect, instant explicit help,
  JSON suppression, terminal size, color, writer errors, finite timing, real
  PTY dogfood, and docs each have an owning task.
- [x] Placeholder scan: the plan contains no unnamed function, deferred error
  path, or unspecified test command.
- [x] Type consistency: renderer, animation, and `runConfig` names match across
  tasks; production and tests use the same timing fields.
- [x] Scope: no setup, query/log/show renderer, new command, dependency, or
  prototype driver enters the change.

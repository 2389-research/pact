<!-- ABOUTME: Defines the animated PACT seal shown before help on a bare interactive invocation. -->
<!-- ABOUTME: Keeps explicit help fast while making the terminal welcome branded, finite, and safe. -->

# PACT Animated Help Seal Design

**Status:** Approved by Doctor Biz on 2026-08-26.

## Goal

Make a bare `pact` invocation feel like a finished terminal product by showing
the rotating globe and wax seal from `~/Downloads/temp2`, then the existing
catalog-backed help. Preserve fast explicit help, deterministic redirected
output, and PACT's current writer and color contracts.

## User Contract

`pact` with no command is the branded welcome path. On a capable terminal it
plays a finite animation, leaves its last seal frame visible, adds one blank
line, and prints the normal top-level help. The command exits zero.

`pact help` and `pact --help` remain instant and do not render the seal. Command
and nested-command help remain unchanged except for continuing to read from
the same command catalog.

Global presentation flags do not create a second path. If no command remains
after parsing `--color`, the invocation still uses the bare welcome. `--json`
suppresses the decorative seal and retains the existing plain help behavior.

## Presentation Matrix

| Invocation | Output |
|---|---|
| Bare, capable stdout TTY | Finite animated seal, then top-level help |
| Bare, redirected stdout | One static seal frame, then top-level help |
| Bare, `TERM=dumb` | One static plain frame, then top-level help |
| Bare, `--json` | Existing help only; no seal or ANSI |
| `pact help` or `pact --help` | Existing instant help only |
| Command or nested help | Existing instant catalog help only |

Color follows the existing precedence: explicit `--color` wins, then
`NO_COLOR`, `TERM=dumb`, and terminal detection govern automatic color. Thus
redirected output is plain under `auto`, while explicit `always` may color its
single static frame. Color reinforces the same land, water, wax, and plaque
characters; stripping ANSI must leave the plain frame byte-for-byte.

## Motion

The production animation uses 16 frames separated by 60 milliseconds. Each
frame advances the globe six degrees, for visible motion without delaying help
for more than one second.

The renderer writes the first frame normally. Later frames move up by the
frame's fixed line count, clear each owned line, and overwrite it. The final
frame stays in terminal history before help appears.

PACT never hides the cursor. Interruption may leave the most recent complete
frame on screen, but cannot strand cursor visibility or another terminal mode.
The animation has no infinite loop and installs no process-wide signal handler.

## Sizing

The source globe targets 40 columns, producing a 46-column seal including its
rim. A terminal frame uses the smaller of:

- the 40-column target;
- the available terminal width minus the rim allowance; and
- the width that keeps the whole frame within the detected terminal height.

The globe never shrinks below 12 columns. If the terminal cannot hold that
minimum frame, PACT skips the seal and prints help. Decorative art may scale;
command names, flags, and other copyable values remain untouched.

`main` retains both dimensions returned by `x/term.GetSize`. `runConfig` owns
the terminal dimensions and finite animation timing because it is the existing
testable process boundary. Production supplies the fixed timing above; focused
tests may use fewer zero-delay frames without adding a mock application mode.

## Code Shape

Create one focused seal renderer under `cmd/pact`. Port the globe geography,
shading, rim, and plaque logic from `~/Downloads/temp2`, but omit its standalone
flags, infinite loop, signal handling, screen clearing, and `main` function.

The seal renderer exposes three small responsibilities:

- choose a safe seal size from terminal dimensions;
- render one deterministic frame for a width, longitude, and color policy;
- emit a finite sequence with writer errors returned to the caller.

The standard-library adapter remains the only help router. It recognizes the
bare welcome after global presentation parsing, calls the seal renderer, then
calls the existing top-level help renderer. Explicit help continues directly
to the existing renderer. The command catalog remains the sole command, flag,
purpose, usage, grouping, and example inventory.

## Failure Behavior

Every frame and cursor-control write propagates its error. A failed seal write
uses the existing `help output failed` diagnostic and exit code 10. PACT does
not retry, fall back to a partial second rendering, or print help after a failed
animation write.

The static and animated paths perform no repository discovery, canonical
write, index write, prompt, or network action.

## Tests

Strict TDD covers:

- a deterministic plain frame against a golden file;
- colored-frame ANSI stripping against that plain frame;
- land and water remaining distinguishable without color;
- 16 finite production frames and exact cursor-up ownership;
- zero-delay focused animation without sleeps;
- width and height scaling plus the too-small-terminal fallback;
- bare TTY animation followed by catalog help;
- redirected bare static output with no ANSI under `auto`;
- `TERM=dumb`, `NO_COLOR`, explicit color, and JSON precedence;
- unchanged instant `help`, `--help`, command help, and nested help;
- animation writer failure returning exit code 10;
- the compiled-binary help contract and canonical repository gate.

A real terminal dogfood run checks that motion is stable, the cursor remains
usable, the final frame stays above help, and narrow output does not ghost.

## Non-Goals

- No continuous `pact seal` command.
- No animation on explicit help.
- No sound, Unicode art, image protocol, alternate logo, or configuration
  file.
- No change to command semantics, JSON result schemas, or repository behavior.
- No setup, log, query, or show renderer work.

## Success Criteria

- Bare interactive `pact` visibly animates for less than one second and exits
  after printing help.
- Explicit help remains immediate.
- Redirected and machine-oriented behavior stays deterministic.
- No terminal state can remain hidden or altered after exit.
- All writer, color, help, E2E, race, vet, and canonical gates pass without
  warnings or skips.

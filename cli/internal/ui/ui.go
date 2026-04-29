// Package ui prints colored, structured progress for the lifecycle commands.
// Status output goes to stderr so subcommand stdout (tables, etc.) stays
// clean. Colors are stripped automatically when stderr is not a TTY or
// when NO_COLOR is set; CLICOLOR_FORCE forces colors on.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Out is where status messages go. Tests can swap it; the CLI leaves it
// at os.Stderr.
var Out io.Writer = os.Stderr

const (
	reset     = "\x1b[0m"
	bold      = "\x1b[1m"
	dim       = "\x1b[2m"
	red       = "\x1b[31m"
	green     = "\x1b[32m"
	yellow    = "\x1b[33m"
	cyan      = "\x1b[36m"
	boldCyan  = "\x1b[1;36m"
	boldGreen = "\x1b[1;32m"
	boldRed   = "\x1b[1;31m"
	boldYel   = "\x1b[1;33m"
)

var useColor = detectColor()

func detectColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("CLICOLOR_FORCE") != "" {
		return true
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// SetColor forces color on/off (overrides auto-detection).
func SetColor(on bool) { useColor = on }

func paint(code, s string) string {
	if !useColor {
		return s
	}
	return code + s + reset
}

// Banner prints a heading on its own line, separated by a blank line.
func Banner(format string, args ...any) {
	fmt.Fprintf(Out, "\n%s\n", paint(bold, fmt.Sprintf(format, args...)))
}

// Step opens a phase with a "▶ <msg>..." line and returns a closure that
// closes the phase with "  ✓ <result> (<elapsed>)". Always call the
// returned closure; pass an empty format to use the original message.
func Step(format string, args ...any) func(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(Out, "%s %s\n", paint(boldCyan, "▶"), msg)
	start := time.Now()
	return func(f string, a ...any) {
		result := fmt.Sprintf(f, a...)
		if result == "" {
			result = "Done"
		}
		fmt.Fprintf(Out, "  %s %s %s\n",
			paint(boldGreen, "✓"),
			result,
			paint(dim, fmt.Sprintf("(%s)", short(time.Since(start)))))
	}
}

// Detail emits an indented dim sub-line under the current step.
func Detail(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	fmt.Fprintf(Out, "  %s\n", paint(dim, line))
}

// Warn prints a yellow "  ! <msg>" line.
func Warn(format string, args ...any) {
	fmt.Fprintf(Out, "  %s %s\n",
		paint(boldYel, "!"),
		fmt.Sprintf(format, args...))
}

// Fail prints a red "  ✗ <msg>" line. Use for non-fatal failures
// inside a phase; for fatal returns, prefer ErrorBlock + return err.
func Fail(format string, args ...any) {
	fmt.Fprintf(Out, "  %s %s\n",
		paint(boldRed, "✗"),
		fmt.Sprintf(format, args...))
}

// Notice prints a free-standing yellow notice block with a heading and
// indented body lines. Used for drift notifications, etc.
func Notice(heading string, lines []string) {
	fmt.Fprintf(Out, "%s %s\n",
		paint(boldYel, "!"),
		paint(bold, heading))
	for _, l := range lines {
		fmt.Fprintf(Out, "    %s\n", l)
	}
}

// Success prints a final green success line, used at the end of a
// command flow. Includes a trailing newline above for breathing room.
func Success(format string, args ...any) {
	fmt.Fprintf(Out, "\n%s %s\n",
		paint(boldGreen, "✓"),
		paint(bold, fmt.Sprintf(format, args...)))
}

// ErrorBlock prints a red multi-line error block. Used when a flow
// aborts and we want to show a structured punch list.
func ErrorBlock(heading string, body string) {
	fmt.Fprintf(Out, "\n%s %s\n", paint(boldRed, "✗"), paint(bold, heading))
	for _, l := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		fmt.Fprintf(Out, "    %s\n", l)
	}
}

// Bold returns the input wrapped in bold escapes (no-op when colors are off).
func Bold(s string) string { return paint(bold, s) }

// Dim returns the input wrapped in dim escapes.
func Dim(s string) string { return paint(dim, s) }

// Red returns the input wrapped in bold-red escapes.
func Red(s string) string { return paint(boldRed, s) }

// Yellow returns the input wrapped in bold-yellow escapes.
func Yellow(s string) string { return paint(boldYel, s) }

// Green returns the input wrapped in bold-green escapes.
func Green(s string) string { return paint(boldGreen, s) }

// short formats a duration as a compact human-friendly string.
func short(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		m := int(d.Minutes())
		s := int(d.Seconds()) - m*60
		return fmt.Sprintf("%dm%02ds", m, s)
	}
}

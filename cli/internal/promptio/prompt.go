// Package promptio is a thin wrapper around an interactive stdin/stdout pair
// used by the `init` handholding flow.
package promptio

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// IO bundles a reader for the user's input and a writer for prompts.
type IO struct {
	In  *bufio.Reader
	Out io.Writer
}

// New returns an IO bound to os.Stdin/os.Stdout.
func New() *IO {
	return &IO{In: bufio.NewReader(os.Stdin), Out: os.Stdout}
}

// IsTTY reports whether stdin is an interactive terminal.
func IsTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// YesNo asks question with [Y/n] semantics. Empty input → defaultYes.
func (p *IO) YesNo(question string, defaultYes bool) (bool, error) {
	suffix := "[Y/n]"
	if !defaultYes {
		suffix = "[y/N]"
	}
	for {
		fmt.Fprintf(p.Out, "%s %s ", question, suffix)
		line, err := p.In.ReadString('\n')
		if err != nil {
			return false, err
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		switch answer {
		case "":
			return defaultYes, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		fmt.Fprintln(p.Out, `please answer "y" or "n"`)
	}
}

// Line asks question and returns the entered line, or fallback if blank.
func (p *IO) Line(question, fallback string) (string, error) {
	if fallback != "" {
		fmt.Fprintf(p.Out, "%s [%s]: ", question, fallback)
	} else {
		fmt.Fprintf(p.Out, "%s: ", question)
	}
	line, err := p.In.ReadString('\n')
	if err != nil {
		return "", err
	}
	answer := strings.TrimSpace(line)
	if answer == "" {
		return fallback, nil
	}
	return answer, nil
}

// Confirm requires the exact word `expected`. Used by destructive ops.
func (p *IO) Confirm(prompt, expected string) (bool, error) {
	fmt.Fprintf(p.Out, "%s ", prompt)
	line, err := p.In.ReadString('\n')
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(line) == expected, nil
}

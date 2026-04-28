package main

import (
	"context"
	"syscall"
	"time"
)

// contextWithTimeout returns a derived context with the given timeout. Mirrors
// context.WithTimeout but accepts a possibly-nil parent (cobra commands can
// have a nil context until SetContext is called).
func contextWithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, d)
}

// execve replaces the current process image. Used by `ai-playground ssh-worker`
// so the user gets a real TTY hand-off (signals, terminal modes) rather than a
// child process the Go runtime is babysitting.
func execve(bin string, argv []string, envv []string) error {
	return syscall.Exec(bin, argv, envv)
}

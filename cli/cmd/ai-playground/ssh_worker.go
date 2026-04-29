package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"

	"github.com/rodrigomideac/ai-playground/internal/ui"
	"github.com/rodrigomideac/ai-playground/internal/worker"
)

var sshWorkerOpts struct {
	wait time.Duration
}

var sshWorkerCmd = &cobra.Command{
	Use:   "ssh-worker [name] [-- ssh-args...]",
	Short: "SSH into a worker (random running one if no name)",
	Long: `Looks up the worker's DHCP-assigned IPv4 lease (waiting if needed)
and execs into ssh as the configured worker user.

If [name] is omitted, a uniformly random *running* worker is chosen.

Any args after '--' are forwarded to ssh.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireBuilt(); err != nil {
			return err
		}
		m, err := newManager()
		if err != nil {
			return err
		}
		ctx, cancel := contextWithTimeout(cmd.Context(), sshWorkerOpts.wait+30*time.Second)
		defer cancel()

		// Distinguish `ssh-worker alpha -- cmd` from `ssh-worker -- cmd`:
		// cmd.ArgsLenAtDash() returns the number of positional args
		// before "--" (or -1 if "--" wasn't used). Anything from the
		// dash onward is forwarded to ssh.
		var name string
		var sshArgs []string
		dash := cmd.ArgsLenAtDash()
		switch {
		case dash == -1: // no "--" used; first arg (if any) is the worker name
			if len(args) >= 1 {
				name = args[0]
			}
		case dash == 0: // "--" with no preceding name → random worker, all args go to ssh
			sshArgs = args
		default: // "ssh-worker <name> -- ssh-args..."
			name = args[0]
			sshArgs = args[dash:]
		}

		var w *worker.Worker
		doneLookup := ui.Step("Selecting worker")
		if name != "" {
			w, err = m.Get(ctx, name)
		} else {
			w, err = m.Random(ctx)
		}
		if err != nil {
			return err
		}
		if w.State != "" && w.State != "running" {
			return fmt.Errorf("worker %s is %q, not running", w.Name, w.State)
		}
		doneLookup("%s (state=%s)", w.Name, w.State)

		doneIP := ui.Step("Waiting for IP address (timeout %s)", sshWorkerOpts.wait)
		ip, err := w.IPWait(ctx, sshWorkerOpts.wait)
		if err != nil {
			return err
		}
		doneIP("Got %s", ip)

		fullArgs := []string{
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
			"-o", "LogLevel=ERROR",
			fmt.Sprintf("%s@%s", globalOpts.sshUser, ip),
		}
		fullArgs = append(fullArgs, sshArgs...)

		bin, err := exec.LookPath("ssh")
		if err != nil {
			return fmt.Errorf("ssh not found in PATH: %w", err)
		}
		ui.Detail("Connecting as %s@%s ...", globalOpts.sshUser, ip)
		return execve(bin, append([]string{"ssh"}, fullArgs...), os.Environ())
	},
}

func init() {
	sshWorkerCmd.Flags().DurationVar(&sshWorkerOpts.wait, "wait", 60*time.Second,
		"How long to wait for the worker's IP address before giving up")
	rootCmd.AddCommand(sshWorkerCmd)
}

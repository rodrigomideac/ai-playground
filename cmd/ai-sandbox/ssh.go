package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
)

var sshOpts struct {
	wait time.Duration
}

var sshCmd = &cobra.Command{
	Use:   "ssh <name> [-- ssh-args...]",
	Short: "SSH into a sandbox VM",
	Long: `Looks up the VM's DHCP-assigned IPv4 lease (waiting if necessary)
and execs into ssh as the configured sandbox user.

Any args after '--' are forwarded to ssh.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, sshArgs := args[0], args[1:]
		m, err := newManager()
		if err != nil {
			return err
		}
		ctx, cancel := contextWithTimeout(cmd.Context(), sshOpts.wait+30*time.Second)
		defer cancel()
		s, err := m.Get(ctx, name)
		if err != nil {
			return err
		}
		if s.State != "running" {
			return fmt.Errorf("sandbox %s is %q, not running", name, s.State)
		}
		ip, err := s.IPWait(ctx, sshOpts.wait)
		if err != nil {
			return err
		}
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
		// Replace the current process so signals/TTY behave naturally.
		return execve(bin, append([]string{"ssh"}, fullArgs...), os.Environ())
	},
}

func init() {
	sshCmd.Flags().DurationVar(&sshOpts.wait, "wait", 60*time.Second,
		"How long to wait for the DHCP lease before giving up")
	rootCmd.AddCommand(sshCmd)
}

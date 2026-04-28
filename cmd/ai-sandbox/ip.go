package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var ipOpts struct {
	wait time.Duration
}

var ipCmd = &cobra.Command{
	Use:   "ip <name>",
	Short: "Print the DHCP-assigned IPv4 address of a sandbox VM",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		m, err := newManager()
		if err != nil {
			return err
		}
		ctx, cancel := contextWithTimeout(cmd.Context(), ipOpts.wait+30*time.Second)
		defer cancel()
		s, err := m.Get(ctx, name)
		if err != nil {
			return err
		}
		ip, err := s.IPWait(ctx, ipOpts.wait)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), ip)
		return nil
	},
}

func init() {
	ipCmd.Flags().DurationVar(&ipOpts.wait, "wait", 30*time.Second,
		"How long to wait for the DHCP lease before giving up")
	rootCmd.AddCommand(ipCmd)
}

package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var destroyCmd = &cobra.Command{
	Use:     "destroy <name>",
	Aliases: []string{"rm"},
	Short:   "Power off and remove a sandbox VM (including its storage)",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		m, err := newManager()
		if err != nil {
			return err
		}
		ctx, cancel := contextWithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		s, err := m.Get(ctx, name)
		if err != nil {
			return err
		}
		if err := s.Destroy(ctx); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Destroyed %s\n", s.Name)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(destroyCmd)
}

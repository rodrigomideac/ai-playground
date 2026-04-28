package main

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all sandbox VMs",
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := newManager()
		if err != nil {
			return err
		}
		ctx, cancel := contextWithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		boxes, err := m.List(ctx)
		if err != nil {
			return err
		}
		if len(boxes) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "(no sandboxes)")
			return nil
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tDOMAIN\tSTATE\tIP")
		for _, b := range boxes {
			ip := ""
			if b.State == "running" {
				if got, err := b.IP(ctx); err == nil {
					ip = got
				} else {
					ip = "(no lease)"
				}
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", b.Name, b.DomainName, b.State, ip)
		}
		return w.Flush()
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}

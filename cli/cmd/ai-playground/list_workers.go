package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/rodrigomideac/ai-playground/internal/worker"
)

var listWorkersCmd = &cobra.Command{
	Use:     "list-workers",
	Aliases: []string{"ls"},
	Short:   "Print the pool with each worker's state and IP",
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := newManager()
		if err != nil {
			return err
		}
		ctx, cancel := contextWithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		return printPool(cmd.OutOrStdout(), m, ctx)
	},
}

func init() {
	rootCmd.AddCommand(listWorkersCmd)
}

// printPool emits a NAME / STATE / IP table for every worker the manager
// sees, sorted by name. It's the canonical "what's in the pool" view used
// by both `list-workers` and the post-add output of `add-worker` and
// `shutdown-worker`.
func printPool(out io.Writer, m *worker.Manager, ctx context.Context) error {
	workers, err := m.List(ctx)
	if err != nil {
		return err
	}
	if len(workers) == 0 {
		fmt.Fprintln(out, "(no workers)")
		return nil
	}
	sort.Slice(workers, func(i, j int) bool { return workers[i].Name < workers[j].Name })

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSTATE\tIP")
	for _, w := range workers {
		ip := "-"
		if w.State == "running" {
			if got, err := w.IP(ctx); err == nil {
				ip = got
			} else {
				ip = "(no lease)"
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", w.Name, w.State, ip)
	}
	return tw.Flush()
}

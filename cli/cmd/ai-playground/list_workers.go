package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/rodrigomideac/ai-playground/cli/internal/ui"
	"github.com/rodrigomideac/ai-playground/cli/internal/worker"
)

var listWorkersCmd = &cobra.Command{
	Use:     "list-workers",
	Aliases: []string{"ls"},
	Short:   "Print the pool with each worker's state and IP",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireBuilt(); err != nil {
			return err
		}
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

// printPool emits a colored, aligned NAME / STATE / IP table for every
// worker the manager sees, sorted by name. Used standalone by
// `list-workers` and embedded in `add-worker` / `shutdown-worker` after
// their action lines.
func printPool(out io.Writer, m *worker.Manager, ctx context.Context) error {
	workers, err := m.List(ctx)
	if err != nil {
		return err
	}
	if len(workers) == 0 {
		fmt.Fprintln(out, ui.Dim("(no workers)"))
		return nil
	}
	sort.Slice(workers, func(i, j int) bool { return workers[i].Name < workers[j].Name })

	rows := make([][]string, 0, len(workers))
	running := 0
	for _, w := range workers {
		state := renderState(w.State)
		ip := "-"
		switch w.State {
		case "running":
			running++
			if got, err := w.IP(ctx); err == nil {
				ip = got
			} else {
				ip = ui.Dim("(no IP yet)")
			}
		default:
			ip = ui.Dim("-")
		}
		rows = append(rows, []string{w.Name, state, ip})
	}

	ui.Table(out, []string{"NAME", "STATE", "IP"}, rows)
	fmt.Fprintf(out, "%s\n",
		ui.Dim(fmt.Sprintf("%d worker(s) — %d running, %d stopped",
			len(workers), running, len(workers)-running)))
	return nil
}

// renderState colors libvirt's domain-state strings: green for running,
// dim for shut off, yellow for transient/paused/anything else.
func renderState(s string) string {
	switch s {
	case "running":
		return ui.Green("● ") + s
	case "shut off", "":
		return ui.Dim("○ " + s)
	default:
		return ui.Yellow("● ") + s
	}
}

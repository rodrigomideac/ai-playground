package main

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/rodrigomideac/ai-playground/internal/ui"
	"github.com/rodrigomideac/ai-playground/internal/worker"
)

var shutdownWorkerCmd = &cobra.Command{
	Use:   "shutdown-worker [name]",
	Short: "Tear down a worker and remove it from the pool (random running one if no name)",
	Long: `Force-stops the worker's libvirt domain and removes all of its
storage volumes. This is destructive — the worker is gone, not just halted.

If [name] is omitted, a uniformly random *running* worker is chosen.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireBuilt(); err != nil {
			return err
		}
		m, err := newManager()
		if err != nil {
			return err
		}
		ctx, cancel := contextWithTimeout(cmd.Context(), 60*time.Second)
		defer cancel()

		var w *worker.Worker
		doneLookup := ui.Step("Selecting worker to shut down")
		if len(args) == 1 {
			w, err = m.Get(ctx, args[0])
		} else {
			w, err = m.Random(ctx)
		}
		if err != nil {
			return err
		}
		doneLookup("%s (state=%s)", w.Name, w.State)

		doneDestroy := ui.Step("Destroying domain and removing storage volumes")
		if err := w.Destroy(ctx); err != nil {
			return err
		}
		doneDestroy("")

		ui.Banner("Pool")
		return printPool(cmd.OutOrStdout(), m, ctx)
	},
}

func init() {
	rootCmd.AddCommand(shutdownWorkerCmd)
}

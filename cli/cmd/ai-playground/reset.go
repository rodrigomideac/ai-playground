package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/rodrigomideac/ai-playground/cli/internal/promptio"
	"github.com/rodrigomideac/ai-playground/cli/internal/ui"
	"github.com/rodrigomideac/ai-playground/cli/internal/worker"
)

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Wipe ai-playground's config, cache, and data directories",
	Long: `Removes:
  $XDG_CONFIG_HOME/ai-playground/
  $XDG_CACHE_HOME/ai-playground/
  $XDG_DATA_HOME/ai-playground/
  /var/lib/libvirt/images/ai-playground-base.qcow2 (the golden image)

If any workers exist with the configured prefix, you will be asked
whether to stop them (and delete their disks) before the directory
wipe. Asks for confirmation before doing anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runReset(cmd.OutOrStdout())
	},
}

func init() {
	rootCmd.AddCommand(resetCmd)
}

func runReset(out io.Writer) error {
	p := cliCtx.Paths
	ui.Banner("ai-playground reset")
	fmt.Fprintf(out, "About to remove:\n  %s\n  %s\n  %s\n  %s\n\n",
		ui.Bold(p.ConfigDir), ui.Bold(p.CacheDir), ui.Bold(p.DataDir), ui.Bold(p.GoldenImage))

	prompt := promptio.New()
	ok, err := prompt.Confirm("Type 'reset' to confirm:", "reset")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, ui.Dim("aborted"))
		return nil
	}

	if err := offerToDestroyDomains(out, prompt); err != nil {
		return err
	}

	for _, dir := range []string{p.ConfigDir, p.CacheDir, p.DataDir} {
		done := ui.Step("Removing %s", dir)
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove %s: %w", dir, err)
		}
		done("")
	}
	// The golden image lives in the libvirt pool dir (so libvirt-qemu can
	// read it without crossing the user's $HOME) and isn't covered by the
	// XDG dir wipe above. The user is in the libvirt group with g+w on the
	// pool dir, so a regular os.Remove suffices — no sudo needed.
	if _, err := os.Stat(p.GoldenImage); err == nil {
		done := ui.Step("Removing %s", p.GoldenImage)
		if err := os.Remove(p.GoldenImage); err != nil {
			return fmt.Errorf("remove %s: %w", p.GoldenImage, err)
		}
		done("")
	}
	ui.Success("ai-playground state wiped. Run 'ai-playground build' to start over.")
	return nil
}

// offerToDestroyDomains lists libvirt domains carrying our prefix and,
// when any are present, asks the user whether to destroy them. Worker
// destruction goes through virsh (no golden/pubkey needed), so it works
// even when the rest of the config is half-broken.
func offerToDestroyDomains(out io.Writer, prompt *promptio.IO) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m := &worker.Manager{Prefix: globalOpts.prefix}
	workers, err := m.List(ctx)
	if err != nil {
		ui.Warn("Could not list workers: %v", err)
		return nil
	}
	if len(workers) == 0 {
		return nil
	}

	fmt.Fprintf(out, "\n%s Found %s worker(s) with prefix %s:\n",
		ui.Yellow("!"),
		ui.Bold(fmt.Sprintf("%d", len(workers))),
		ui.Bold(globalOpts.prefix))
	for _, w := range workers {
		fmt.Fprintf(out, "    - %s %s\n", w.Name, ui.Dim(fmt.Sprintf("(state=%s)", w.State)))
	}

	shut, err := prompt.YesNo("Stop these workers and delete their disks too?", true)
	if err != nil {
		return err
	}
	if !shut {
		ui.Detail("Leaving workers in place. Use 'ai-playground shutdown-worker' later if needed.")
		return nil
	}
	for _, w := range workers {
		done := ui.Step("Stopping %s", w.Name)
		if err := w.Destroy(ctx); err != nil {
			ui.Warn("%v", err)
			continue
		}
		done("")
	}
	return nil
}

package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/rodrigomideac/ai-playground/internal/promptio"
)

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Wipe ai-playground's config, cache, and data directories",
	Long: `Removes:
  $XDG_CONFIG_HOME/ai-playground/
  $XDG_CACHE_HOME/ai-playground/
  $XDG_DATA_HOME/ai-playground/

Existing libvirt domains are NOT touched — use 'ai-playground shutdown-worker'
for those. Asks for confirmation before deleting anything.`,
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
	fmt.Fprintf(out, "About to remove:\n  %s\n  %s\n  %s\n\n",
		p.ConfigDir, p.CacheDir, p.DataDir)

	prompt := promptio.New()
	ok, err := prompt.Confirm("Type 'reset' to confirm:", "reset")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "aborted")
		return nil
	}

	for _, dir := range []string{p.ConfigDir, p.CacheDir, p.DataDir} {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove %s: %w", dir, err)
		}
	}
	fmt.Fprintln(out, "ai-playground state wiped. Run 'ai-playground build' to start over.")
	return nil
}

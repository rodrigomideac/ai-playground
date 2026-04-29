package main

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/rodrigomideac/ai-playground/internal/doctor"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Print a verbose KVM/qemu/libvirt stack diagnostic",
	Long: `doctor runs every host-environment check ai-playground depends on
and prints the results grouped by stack layer (CPU virtualization →
KVM kernel module → qemu → libvirtd → libvirt clients → Packer →
host tooling).

The output is intentionally precise: it uses the exact kernel/libvirt/
qemu terminology and includes a manual-inspection command line for
each check so a human (or coding agent) reading the output has enough
context to debug a broken host without having to guess.

Exit code is non-zero iff any check failed.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, cancel := contextWithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		results := doctor.RunAll(ctx)
		doctor.PrintVerbose(cmd.OutOrStdout(), results)
		for _, r := range results {
			if r.Problem != nil {
				return errSilent
			}
		}
		return nil
	},
}

// errSilent is returned when doctor finds failures. The output is already
// printed by PrintVerbose; main.go's "✗ <err>" line would be redundant.
// We silence it by checking for this sentinel.
var errSilent = &silentError{}

type silentError struct{}

func (*silentError) Error() string { return "" }

func init() {
	rootCmd.AddCommand(doctorCmd)
}

package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/rodrigomideac/ai-playground/internal/worker"
)

var addWorkerOpts struct {
	memory uint
	cpus   uint
	mount  string
	wait   time.Duration
	noWait bool
}

var addWorkerCmd = &cobra.Command{
	Use:   "add-worker [name]",
	Short: "Add a worker to the pool",
	Long: `Spins up a new worker VM by linked-cloning the golden qcow2,
seeding cloud-init for the new instance, and starting the libvirt domain.

If [name] is omitted, a random name like "worker-3f9a17" is generated.

By default, blocks until the new worker has a DHCP-assigned IP, then
prints the full pool table. Pass --no-wait to return immediately
(the new worker will appear in the table without an IP yet).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireBuilt(); err != nil {
			return err
		}
		var name string
		if len(args) == 1 {
			name = args[0]
		}
		m, err := newManager()
		if err != nil {
			return err
		}
		ctx, cancel := contextWithTimeout(cmd.Context(), 5*time.Minute)
		defer cancel()
		w, err := m.Create(ctx, name, worker.CreateOptions{
			Memory:    int(addWorkerOpts.memory),
			CPUs:      int(addWorkerOpts.cpus),
			HostMount: addWorkerOpts.mount,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Added worker %s\n\n", w.Name)
		if !addWorkerOpts.noWait {
			if _, err := w.IPWait(ctx, addWorkerOpts.wait); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n\n", err)
			}
		}
		return printPool(cmd.OutOrStdout(), m, ctx)
	},
}

func init() {
	addWorkerCmd.Flags().UintVar(&addWorkerOpts.memory, "memory", 4096, "Memory in MiB")
	addWorkerCmd.Flags().UintVar(&addWorkerOpts.cpus, "cpus", 2, "Number of vCPUs")
	addWorkerCmd.Flags().StringVar(&addWorkerOpts.mount, "mount", "",
		"Host directory shared at /home/<ssh-user>/project via virtio-9p")
	addWorkerCmd.Flags().DurationVar(&addWorkerOpts.wait, "wait", 90*time.Second,
		"Wait this long for the new worker's IP before printing the pool")
	addWorkerCmd.Flags().BoolVar(&addWorkerOpts.noWait, "no-wait", false,
		"Don't wait for the new worker's IP")
	rootCmd.AddCommand(addWorkerCmd)
}

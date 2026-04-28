package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/rodrigomideac/ai-playground/internal/sandbox"
)

var createOpts struct {
	memory uint
	cpus   uint
	mount  string
}

var createCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new sandbox VM from the golden image",
	Long: `Create provisions a sandbox VM:

  - clones the golden qcow2 into a thin overlay (linked clone)
  - builds a NoCloud cloud-init seed ISO with this VM's identity
  - calls virt-install --import to define and start the domain

If [name] is omitted, a random name like "sandbox-3f9a17" is generated.

The VM lives on libvirt's default network with a DHCP-assigned IP.
'ai-sandbox ssh <name>' will connect once the lease is up.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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
		s, err := m.Create(ctx, name, sandbox.CreateOptions{
			Memory:    int(createOpts.memory),
			CPUs:      int(createOpts.cpus),
			HostMount: createOpts.mount,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(),
			"Created sandbox %s (libvirt domain %s)\n  ai-sandbox ssh %s    # once cloud-init finishes booting (~30s)\n",
			s.Name, s.DomainName, s.Name)
		return nil
	},
}

func init() {
	createCmd.Flags().UintVar(&createOpts.memory, "memory", 4096, "Memory in MiB")
	createCmd.Flags().UintVar(&createOpts.cpus, "cpus", 2, "Number of vCPUs")
	createCmd.Flags().StringVar(&createOpts.mount, "mount", "",
		"Host directory to share at /home/<ssh-user>/project via virtio-9p (empty = no mount)")
	rootCmd.AddCommand(createCmd)
}

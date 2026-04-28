package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rodrigomideac/ai-playground/internal/worker"
)

// globalOpts are persistent flags shared by every subcommand.
var globalOpts struct {
	pubkey  string
	golden  string
	pool    string
	network string
	prefix  string
	sshUser string
}

var rootCmd = &cobra.Command{
	Use:   "ai-playground",
	Short: "Manage a pool of disposable Debian worker VMs (KVM/libvirt + cloud-init)",
	Long: `ai-playground spins up and manages a local pool of Debian worker VMs
cloned from the golden qcow2 image (built by 'make build-from-base').
Each worker gets a DHCP-assigned IP on libvirt's default network.

Per-worker personalization (hostname, user, SSH key, optional 9p mount)
is delivered by a NoCloud cloud-init seed ISO at first boot. The image
is linked-cloned via qcow2 backing files, so creation is fast and cheap.

Domain names are prefixed (default "aip-") so they don't collide with
other libvirt VMs on the host.

Surface:
  add-worker [name]       Spin up a new worker, then print the pool.
  ssh-worker [name]       SSH into a worker (random running one if no name).
  shutdown-worker [name]  Tear down a worker (random running one if no name).
  list-workers            Print the pool.`,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&globalOpts.pubkey, "ssh-pubkey", defaultPubKey(),
		"SSH public key file authorized for the worker user")
	rootCmd.PersistentFlags().StringVar(&globalOpts.golden, "golden", defaultGolden(),
		"Path to the golden qcow2 image")
	rootCmd.PersistentFlags().StringVar(&globalOpts.pool, "pool", "default",
		"libvirt storage pool")
	rootCmd.PersistentFlags().StringVar(&globalOpts.network, "network", "default",
		"libvirt network")
	rootCmd.PersistentFlags().StringVar(&globalOpts.prefix, "prefix", "aip",
		"libvirt domain name prefix")
	rootCmd.PersistentFlags().StringVar(&globalOpts.sshUser, "ssh-user", "vm",
		"User created inside each worker VM")
}

func newManager() (*worker.Manager, error) {
	if globalOpts.pubkey == "" {
		return nil, fmt.Errorf("no SSH public key found; pass --ssh-pubkey")
	}
	pubkey, err := os.ReadFile(globalOpts.pubkey)
	if err != nil {
		return nil, fmt.Errorf("read --ssh-pubkey %s: %w", globalOpts.pubkey, err)
	}
	golden, err := filepath.Abs(globalOpts.golden)
	if err != nil {
		return nil, fmt.Errorf("resolve --golden: %w", err)
	}
	return &worker.Manager{
		GoldenImage: golden,
		Pool:        globalOpts.pool,
		Network:     globalOpts.network,
		Prefix:      globalOpts.prefix,
		SSHUser:     globalOpts.sshUser,
		SSHPubKey:   strings.TrimSpace(string(pubkey)),
	}, nil
}

func defaultPubKey() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, name := range []string{"id_ed25519.pub", "id_rsa.pub"} {
		p := filepath.Join(home, ".ssh", name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func defaultGolden() string {
	return "build/packer-ai-playground-base/ai-playground-base"
}

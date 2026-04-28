package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rodrigomideac/ai-playground/internal/sandbox"
)

// globalOpts are persistent flags shared by every subcommand. Sourced from
// flags or the AI_SANDBOX_* environment variables.
var globalOpts struct {
	pubkey  string
	golden  string
	pool    string
	network string
	prefix  string
	sshUser string
}

var rootCmd = &cobra.Command{
	Use:   "ai-sandbox",
	Short: "Spawn and manage local KVM/libvirt sandbox VMs from the ai-playground golden image",
	Long: `ai-sandbox creates disposable Debian sandbox VMs cloned from a golden
qcow2 image (built by 'make build-from-base'). Each sandbox is a libvirt
domain on the default libvirt network with its own DHCP-assigned IP.

The image is linked-cloned via qcow2 backing files, so creation is fast.
Per-sandbox personalization (hostname, user, SSH key, optional 9p mount)
is delivered by a NoCloud cloud-init seed ISO attached at first boot.

Domain names are prefixed (default "aip-") so they don't collide with
other libvirt VMs on the host.`,
}

func init() {
	defaultPubkey := defaultPubKey()
	rootCmd.PersistentFlags().StringVar(&globalOpts.pubkey, "ssh-pubkey", defaultPubkey,
		"SSH public key file authorized for the sandbox user")
	rootCmd.PersistentFlags().StringVar(&globalOpts.golden, "golden", defaultGolden(),
		"Path to the golden qcow2 image")
	rootCmd.PersistentFlags().StringVar(&globalOpts.pool, "pool", "default",
		"libvirt storage pool")
	rootCmd.PersistentFlags().StringVar(&globalOpts.network, "network", "default",
		"libvirt network")
	rootCmd.PersistentFlags().StringVar(&globalOpts.prefix, "prefix", "aip",
		"libvirt domain name prefix")
	rootCmd.PersistentFlags().StringVar(&globalOpts.sshUser, "ssh-user", "vm",
		"User created inside the sandbox VM")
}

// newManager constructs a sandbox.Manager from the resolved global flags.
func newManager() (*sandbox.Manager, error) {
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
	return &sandbox.Manager{
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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rodrigomideac/ai-playground/internal/config"
	"github.com/rodrigomideac/ai-playground/internal/paths"
	"github.com/rodrigomideac/ai-playground/internal/worker"
)

// globalOpts are persistent flags shared by every subcommand.
var globalOpts struct {
	pubkey   string
	golden   string
	pool     string
	network  string
	prefix   string
	sshUser  string
	repoPath string
}

// cliCtx is populated in PersistentPreRunE and read by each subcommand.
var cliCtx struct {
	Paths       *paths.Paths
	Cfg         *config.Config // nil when config.yaml is missing
	SSHUserSet  bool           // true when --ssh-user was passed explicitly
	GoldenSet   bool           // true when --golden was passed explicitly
}

var rootCmd = &cobra.Command{
	Use:   "ai-playground",
	Short: "Build a Debian golden image and manage a pool of disposable worker VMs",
	Long: `ai-playground builds a Debian golden qcow2 image (Packer + cloud-init)
and orchestrates a local pool of disposable worker VMs cloned from it
via libvirt. Each worker gets a DHCP-assigned IP on libvirt's default
network. Per-worker personalization is delivered by a NoCloud cloud-init
seed at first boot.

Lifecycle:
  init                    Interactive setup; writes config.yaml and populates build/.
  build                   First-run setup-if-needed + Packer build (idempotent).
  add-worker [name]       Spin up a worker, then print the pool.
  ssh-worker [name]       SSH into a worker (random running one if no name).
  shutdown-worker [name]  Tear down a worker (random running one if no name).
  list-workers            Print the pool.
  reset                   Wipe config + cache + data dirs (with confirmation).`,
	SilenceUsage:      true,
	SilenceErrors:     true,
	PersistentPreRunE: persistentPreRun,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&globalOpts.pubkey, "ssh-pubkey", defaultPubKey(),
		"SSH public key file authorized for the worker user")
	rootCmd.PersistentFlags().StringVar(&globalOpts.golden, "golden", "",
		"Path to the golden qcow2 image (default: $XDG_DATA_HOME/ai-playground/golden/ai-playground-base.qcow2)")
	rootCmd.PersistentFlags().StringVar(&globalOpts.pool, "pool", "default",
		"libvirt storage pool")
	rootCmd.PersistentFlags().StringVar(&globalOpts.network, "network", "default",
		"libvirt network")
	rootCmd.PersistentFlags().StringVar(&globalOpts.prefix, "prefix", "aip",
		"libvirt domain name prefix")
	rootCmd.PersistentFlags().StringVar(&globalOpts.sshUser, "ssh-user", "",
		"User created inside each worker VM (default: vm_user from config.yaml, else 'vm')")
	rootCmd.PersistentFlags().StringVar(&globalOpts.repoPath, "repo-path", "",
		"Local repo to use as the source tree (overrides the public repo cache)")
}

// persistentPreRun resolves XDG paths, loads config.yaml if present, and
// applies the lazy defaults for --golden and --ssh-user. It runs before
// every subcommand's RunE.
func persistentPreRun(cmd *cobra.Command, _ []string) error {
	cliCtx.GoldenSet = cmd.Flags().Changed("golden")
	cliCtx.SSHUserSet = cmd.Flags().Changed("ssh-user")

	p, err := paths.Default()
	if err != nil {
		return fmt.Errorf("resolve XDG paths: %w", err)
	}
	cliCtx.Paths = p

	cfg, err := config.MaybeLoad(p.Config)
	if err != nil {
		return err
	}
	cliCtx.Cfg = cfg

	if !cliCtx.GoldenSet {
		globalOpts.golden = p.GoldenImage
	}
	if !cliCtx.SSHUserSet {
		if cfg != nil && cfg.VMUser != "" {
			globalOpts.sshUser = cfg.VMUser
		} else {
			globalOpts.sshUser = "vm"
		}
	}
	return nil
}

// repoOverride returns the active --repo-path, or the AI_PLAYGROUND_REPO
// env var if the flag is unset. Flag wins.
func repoOverride() string {
	if globalOpts.repoPath != "" {
		return globalOpts.repoPath
	}
	return os.Getenv("AI_PLAYGROUND_REPO")
}

// requireBuilt fast-fails the daily commands when config.yaml or the
// golden qcow2 is missing. Honors --golden (so tests / advanced users can
// point the daily commands at an alternate golden image).
func requireBuilt() error {
	if cliCtx.Cfg == nil {
		return fmt.Errorf("config.yaml not found at %s — run 'ai-playground build' first", cliCtx.Paths.Config)
	}
	if _, err := os.Stat(globalOpts.golden); err != nil {
		return fmt.Errorf("golden image not found at %s — run 'ai-playground build' first", globalOpts.golden)
	}
	return nil
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

package worker

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rodrigomideac/ai-playground/cli/internal/ui"
)

// Manager creates and looks up workers. Construct one per CLI invocation;
// it carries the configuration that's the same across all workers.
type Manager struct {
	GoldenImage string // absolute path to the golden qcow2
	Pool        string // libvirt storage pool (typically "default")
	Network     string // libvirt network (typically "default")
	Prefix      string // domain name prefix (e.g. "aip")
	SSHUser     string // user cloud-init creates inside the VM
	SSHPubKey   string // pubkey contents (not path) authorized for SSHUser
}

// CreateOptions are per-worker knobs.
type CreateOptions struct {
	Memory    int    // MiB
	CPUs      int
	HostMount string // host directory shared at /home/<SSHUser>/project; empty disables
}

// DomainName returns the libvirt domain name for a given worker name.
func (m *Manager) DomainName(name string) string {
	return m.Prefix + "-" + name
}

func (m *Manager) stripPrefix(domain string) string {
	pref := m.Prefix + "-"
	if !strings.HasPrefix(domain, pref) {
		return ""
	}
	return domain[len(pref):]
}

// Create provisions a new worker: linked-clone overlay + NoCloud seed +
// virt-install --import. Returns a Worker describing the result.
func (m *Manager) Create(ctx context.Context, name string, opts CreateOptions) (*Worker, error) {
	if name == "" {
		name = GenerateName()
	}
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	if _, err := os.Stat(m.GoldenImage); err != nil {
		return nil, fmt.Errorf("golden image not accessible at %s: %w", m.GoldenImage, err)
	}
	if m.SSHPubKey == "" {
		return nil, fmt.Errorf("ssh pubkey is empty")
	}

	domain := m.DomainName(name)
	if exists, err := m.domainExists(ctx, domain); err != nil {
		return nil, err
	} else if exists {
		return nil, fmt.Errorf("a worker named %q already exists", name)
	}

	poolPath, err := m.poolPath(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve VM disk directory: %w", err)
	}

	w := &Worker{
		Name:       name,
		DomainName: domain,
		DiskPath:   filepath.Join(poolPath, domain+".qcow2"),
		SeedPath:   filepath.Join(poolPath, domain+"-seed.iso"),
	}

	// 1. Linked-clone overlay (instant, kilobytes)
	ui.Detail("Creating VM disk → %s", w.DiskPath)
	if err := runMuted(ctx, "qemu-img", "create",
		"-f", "qcow2", "-F", "qcow2",
		"-b", m.GoldenImage,
		w.DiskPath); err != nil {
		return nil, fmt.Errorf("create VM disk: %w", err)
	}

	// 2. NoCloud seed ISO with this worker's identity + ssh key
	ui.Detail("Writing first-boot config → %s", w.SeedPath)
	hostMount := opts.HostMount != ""
	if err := BuildSeedISO(ctx, w.SeedPath, name, m.SSHUser, m.SSHPubKey, hostMount); err != nil {
		_ = os.Remove(w.DiskPath)
		return nil, fmt.Errorf("write first-boot config: %w", err)
	}

	// 3. Refresh the pool so libvirt sees the new files
	_ = runQuiet(ctx, "virsh", "-c", "qemu:///system", "pool-refresh", m.Pool)

	// 4. Define + start the domain
	ui.Detail("Registering and starting VM %s", domain)
	args := []string{
		"--connect", "qemu:///system",
		"--name", domain,
		"--memory", strconv.Itoa(opts.Memory),
		// Pin a 1-socket topology — virt-install's default for --vcpus N
		// expands to "sockets=N,cores=1,threads=1" (one CPU per socket).
		"--vcpus", fmt.Sprintf("%d,sockets=1,cores=%d,threads=1", opts.CPUs, opts.CPUs),
		"--cpu", "host-passthrough",
		// Match Packer's machine type. The golden image is built on i440fx
		// + SeaBIOS; virt-install defaults to q35 (with UEFI) on Debian 13,
		// which boots into a GRUB loop on this image.
		"--machine", "pc",
		// libvirt's qemu invocation includes -nodefaults, which strips the
		// default VGA. Without a video device on i440fx the cloud kernel
		// triple-faults during early boot — visible only as a once-per-
		// second GRUB reboot loop on the serial console.
		"--video", "vga",
		"--os-variant", "debian13",
		"--import",
		"--disk", fmt.Sprintf("path=%s,format=qcow2,bus=virtio", w.DiskPath),
		"--disk", fmt.Sprintf("path=%s,device=cdrom", w.SeedPath),
		"--network", fmt.Sprintf("network=%s,model=virtio", m.Network),
		"--graphics", "none",
		"--noautoconsole",
	}
	if hostMount {
		abs, err := filepath.Abs(opts.HostMount)
		if err != nil {
			return nil, fmt.Errorf("resolve host mount path: %w", err)
		}
		args = append(args, "--filesystem",
			fmt.Sprintf("type=mount,source=%s,target=hostshare,accessmode=passthrough", abs))
	}
	if err := runMuted(ctx, "virt-install", args...); err != nil {
		_ = os.Remove(w.DiskPath)
		_ = os.Remove(w.SeedPath)
		return nil, fmt.Errorf("virt-install: %w", err)
	}
	return w, nil
}

// Get returns the Worker for a name, or an error if no such worker exists.
func (m *Manager) Get(ctx context.Context, name string) (*Worker, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	domain := m.DomainName(name)
	if exists, err := m.domainExists(ctx, domain); err != nil {
		return nil, err
	} else if !exists {
		return nil, fmt.Errorf("no worker named %q", name)
	}
	state, _ := m.domainState(ctx, domain)
	poolPath, _ := m.poolPath(ctx)
	return &Worker{
		Name:       name,
		DomainName: domain,
		DiskPath:   filepath.Join(poolPath, domain+".qcow2"),
		SeedPath:   filepath.Join(poolPath, domain+"-seed.iso"),
		State:      state,
	}, nil
}

// List returns every worker carrying our prefix, in libvirt's iteration order.
func (m *Manager) List(ctx context.Context) ([]*Worker, error) {
	out, err := runCapture(ctx, "virsh", "-c", "qemu:///system",
		"list", "--all", "--name")
	if err != nil {
		return nil, err
	}
	var result []*Worker
	for _, line := range strings.Split(string(out), "\n") {
		domain := strings.TrimSpace(line)
		name := m.stripPrefix(domain)
		if name == "" {
			continue
		}
		state, _ := m.domainState(ctx, domain)
		result = append(result, &Worker{
			Name:       name,
			DomainName: domain,
			State:      state,
		})
	}
	return result, nil
}

// Random returns a uniformly random worker that is currently running. Errors
// when the pool has no running workers. Used by `ssh-worker` and
// `shutdown-worker` when invoked without a name.
func (m *Manager) Random(ctx context.Context) (*Worker, error) {
	workers, err := m.List(ctx)
	if err != nil {
		return nil, err
	}
	var running []*Worker
	for _, w := range workers {
		if w.State == "running" {
			running = append(running, w)
		}
	}
	if len(running) == 0 {
		return nil, fmt.Errorf("no running workers in the pool")
	}
	return running[rand.IntN(len(running))], nil
}

func (m *Manager) domainExists(ctx context.Context, domain string) (bool, error) {
	if err := runQuiet(ctx, "virsh", "-c", "qemu:///system", "dominfo", domain); err == nil {
		return true, nil
	}
	// virsh exits non-zero for "not found"; we treat any error as "doesn't exist".
	// More specific detection would require parsing the error string and isn't
	// worth the brittleness here.
	return false, nil
}

func (m *Manager) domainState(ctx context.Context, domain string) (string, error) {
	out, err := runCapture(ctx, "virsh", "-c", "qemu:///system", "domstate", domain)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

var poolPathRegex = regexp.MustCompile(`(?s)<path>(.*?)</path>`)

func (m *Manager) poolPath(ctx context.Context) (string, error) {
	out, err := runCapture(ctx, "virsh", "-c", "qemu:///system",
		"pool-dumpxml", m.Pool)
	if err != nil {
		return "", err
	}
	match := poolPathRegex.FindStringSubmatch(string(out))
	if len(match) < 2 {
		return "", fmt.Errorf("could not read disk path from libvirt storage pool %q", m.Pool)
	}
	return strings.TrimSpace(match[1]), nil
}

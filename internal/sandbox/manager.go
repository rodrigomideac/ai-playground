package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Manager creates and looks up sandbox VMs. Construct one per CLI invocation;
// it carries the configuration that's the same across all sandboxes.
type Manager struct {
	GoldenImage string // absolute path to the golden qcow2
	Pool        string // libvirt storage pool (typically "default")
	Network     string // libvirt network (typically "default")
	Prefix      string // domain name prefix (e.g. "aip")
	SSHUser     string // user cloud-init creates inside the VM
	SSHPubKey   string // pubkey contents (not path) authorized for SSHUser
}

// CreateOptions are per-sandbox knobs.
type CreateOptions struct {
	Memory    int    // MiB
	CPUs      int
	HostMount string // host directory to share at /home/<SSHUser>/project; empty disables
}

// DomainName returns the libvirt domain name for a given sandbox name.
func (m *Manager) DomainName(name string) string {
	return m.Prefix + "-" + name
}

// stripPrefix returns the user-facing name from a libvirt domain name,
// or "" if domain doesn't carry our prefix.
func (m *Manager) stripPrefix(domain string) string {
	pref := m.Prefix + "-"
	if !strings.HasPrefix(domain, pref) {
		return ""
	}
	return domain[len(pref):]
}

// Create provisions a new sandbox: linked-clone overlay + NoCloud seed +
// virt-install --import. Returns a Sandbox describing the result.
func (m *Manager) Create(ctx context.Context, name string, opts CreateOptions) (*Sandbox, error) {
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
		return nil, fmt.Errorf("libvirt domain %q already exists", domain)
	}

	poolPath, err := m.poolPath(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve pool path: %w", err)
	}

	s := &Sandbox{
		Name:       name,
		DomainName: domain,
		DiskPath:   filepath.Join(poolPath, domain+".qcow2"),
		SeedPath:   filepath.Join(poolPath, domain+"-seed.iso"),
	}

	// 1. Linked-clone overlay (instant, kilobytes)
	if err := run(ctx, "qemu-img", "create",
		"-f", "qcow2", "-F", "qcow2",
		"-b", m.GoldenImage,
		s.DiskPath); err != nil {
		return nil, fmt.Errorf("create overlay: %w", err)
	}

	// 2. NoCloud seed ISO with this VM's identity + ssh key
	hostMount := opts.HostMount != ""
	if err := BuildSeedISO(ctx, s.SeedPath, name, m.SSHUser, m.SSHPubKey, hostMount); err != nil {
		_ = os.Remove(s.DiskPath)
		return nil, fmt.Errorf("build seed iso: %w", err)
	}

	// 3. Refresh the pool so libvirt sees the new files
	_ = runQuiet(ctx, "virsh", "-c", "qemu:///system", "pool-refresh", m.Pool)

	// 4. Define + start the domain
	args := []string{
		"--connect", "qemu:///system",
		"--name", domain,
		"--memory", strconv.Itoa(opts.Memory),
		// Pin a 1-socket topology. virt-install's default for --vcpus N is
		// "sockets=N,cores=1,threads=1" (one CPU per socket). On i440fx
		// (the machine the image was built on) that produces a multi-
		// socket layout the chipset can't represent, and the guest
		// triple-faults during early boot before printing anything to
		// the serial console — visible only as a GRUB reboot loop.
		"--vcpus", fmt.Sprintf("%d,sockets=1,cores=%d,threads=1", opts.CPUs, opts.CPUs),
		"--cpu", "host-passthrough",
		// Match Packer's machine type. Packer's qemu builder defaults to
		// i440fx ("pc") + SeaBIOS, which is what the golden image's GRUB
		// expects. virt-install defaults to q35 (often with UEFI) for
		// modern guests, which boots into a GRUB loop on this image.
		"--machine", "pc",
		// libvirt's default qemu invocation passes -nodefaults, which
		// strips the default VGA device. Without a VGA card on i440fx
		// the Debian cloud kernel triple-faults during early boot
		// (visible only as a GRUB reboot loop on the serial console).
		// Re-adding any video device fixes it; vga is the cheapest.
		"--video", "vga",
		"--os-variant", "debian13",
		"--import",
		"--disk", fmt.Sprintf("path=%s,format=qcow2,bus=virtio", s.DiskPath),
		"--disk", fmt.Sprintf("path=%s,device=cdrom", s.SeedPath),
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
	if err := run(ctx, "virt-install", args...); err != nil {
		_ = os.Remove(s.DiskPath)
		_ = os.Remove(s.SeedPath)
		return nil, fmt.Errorf("virt-install: %w", err)
	}
	return s, nil
}

// Get returns the Sandbox for a name, or an error if it doesn't exist.
func (m *Manager) Get(ctx context.Context, name string) (*Sandbox, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	domain := m.DomainName(name)
	if exists, err := m.domainExists(ctx, domain); err != nil {
		return nil, err
	} else if !exists {
		return nil, fmt.Errorf("no sandbox named %q", name)
	}
	state, _ := m.domainState(ctx, domain)
	poolPath, _ := m.poolPath(ctx)
	return &Sandbox{
		Name:       name,
		DomainName: domain,
		DiskPath:   filepath.Join(poolPath, domain+".qcow2"),
		SeedPath:   filepath.Join(poolPath, domain+"-seed.iso"),
		State:      state,
	}, nil
}

// List returns every sandbox carrying our prefix.
func (m *Manager) List(ctx context.Context) ([]*Sandbox, error) {
	out, err := runCapture(ctx, "virsh", "-c", "qemu:///system",
		"list", "--all", "--name")
	if err != nil {
		return nil, err
	}
	var result []*Sandbox
	for _, line := range strings.Split(string(out), "\n") {
		domain := strings.TrimSpace(line)
		name := m.stripPrefix(domain)
		if name == "" {
			continue
		}
		state, _ := m.domainState(ctx, domain)
		result = append(result, &Sandbox{
			Name:       name,
			DomainName: domain,
			State:      state,
		})
	}
	return result, nil
}

func (m *Manager) domainExists(ctx context.Context, domain string) (bool, error) {
	err := runQuiet(ctx, "virsh", "-c", "qemu:///system", "dominfo", domain)
	if err == nil {
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
		return "", fmt.Errorf("pool %q has no <path> in dumpxml", m.Pool)
	}
	return strings.TrimSpace(match[1]), nil
}

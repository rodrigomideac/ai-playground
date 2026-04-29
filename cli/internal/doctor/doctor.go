// Package doctor verifies the host environment can build and run workers.
// `init` runs the full set; `build` re-runs the cheap subset to catch
// transient regressions like "rebooted into a session without the libvirt
// group".
package doctor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"syscall"
	"time"
)

// Problem is a single failed check, ready to print as a punch-list entry.
type Problem struct {
	Summary string
	Hint    string // multi-line; rendered with each line indented
}

// Check is one host-state assertion.
type Check struct {
	Name  string
	Cheap bool
	Run   func(ctx context.Context) *Problem
}

// All returns the full set of checks.
func All() []Check {
	return []Check{
		// Required commands
		cmdCheck("git", "Fedora: dnf install git | Ubuntu: apt install git | Arch: pacman -S git"),
		cmdCheck("curl", "Fedora: dnf install curl | Ubuntu: apt install curl | Arch: pacman -S curl"),
		cmdCheck("qemu-system-x86_64", "Fedora: dnf install @virtualization | Ubuntu: apt install qemu-system-x86 | Arch: pacman -S qemu-desktop"),
		cmdCheck("qemu-img", "Shipped with qemu (see qemu-system-x86_64 hint)."),
		cmdCheck("xorriso", "Fedora: dnf install xorriso | Ubuntu: apt install xorriso | Arch: pacman -S libisoburn"),
		cmdCheck("ssh-keygen", "Fedora: dnf install openssh-clients | Ubuntu: apt install openssh-client | Arch: pacman -S openssh"),
		cmdCheck("packer", "https://developer.hashicorp.com/packer/install"),
		cmdCheck("virsh", "Fedora: dnf install libvirt-client | Ubuntu: apt install libvirt-clients | Arch: pacman -S libvirt"),
		cmdCheck("virt-install", "Fedora: dnf install virt-install | Ubuntu: apt install virtinst | Arch: pacman -S virt-install"),

		// Packer is HashiCorp, not cracklib.
		{Name: "packer is HashiCorp Packer", Run: checkPackerIsHashicorp},

		// CPU/KVM
		{Name: "/dev/kvm read+write by current user", Cheap: true, Run: checkKVMAccess},
		{Name: "user is in libvirt group", Cheap: true, Run: checkLibvirtGroup},
		{Name: "CPU has vmx or svm", Run: checkCPUVirt},

		// libvirt
		{Name: "libvirtd reachable via qemu:///system", Run: checkLibvirtd},
		{Name: "libvirt default network is active + autostart", Cheap: true, Run: checkDefaultNetwork},
		{Name: "/var/lib/libvirt/images is group libvirt with g+rwxs", Cheap: true, Run: checkPoolPerms},

		// SSH key for worker auth
		{Name: "an ssh public key is available", Run: checkSSHKey},
	}
}

// Run executes the requested subset of checks (full or cheap).
func Run(ctx context.Context, cheapOnly bool) []Problem {
	var problems []Problem
	for _, c := range All() {
		if cheapOnly && !c.Cheap {
			continue
		}
		if p := c.Run(ctx); p != nil {
			problems = append(problems, *p)
		}
	}
	return problems
}

// PrintProblems writes a punch list to w. Returns the number of problems.
func PrintProblems(w io.Writer, problems []Problem) {
	if len(problems) == 0 {
		return
	}
	fmt.Fprintf(w, "doctor: %d issue(s) need to be fixed before continuing:\n\n", len(problems))
	for _, p := range problems {
		fmt.Fprintf(w, "  - %s\n", p.Summary)
		for _, line := range strings.Split(strings.TrimRight(p.Hint, "\n"), "\n") {
			if line == "" {
				continue
			}
			fmt.Fprintf(w, "      %s\n", line)
		}
	}
}

func cmdCheck(name, hint string) Check {
	return Check{
		Name: name + " is installed",
		Run: func(ctx context.Context) *Problem {
			if _, err := exec.LookPath(name); err != nil {
				return &Problem{Summary: name + " is not installed", Hint: hint}
			}
			return nil
		},
	}
}

func checkPackerIsHashicorp(ctx context.Context) *Problem {
	if _, err := exec.LookPath("packer"); err != nil {
		return nil // already reported by cmdCheck
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "packer", "version").CombinedOutput()
	if err == nil && strings.HasPrefix(strings.TrimSpace(string(out)), "Packer v") {
		return nil
	}
	return &Problem{
		Summary: "the 'packer' binary is not HashiCorp Packer (likely the cracklib utility)",
		Hint: "Fedora/CentOS: dnf remove cracklib-packer\n" +
			"Then install HashiCorp Packer: https://developer.hashicorp.com/packer/install",
	}
}

func checkKVMAccess(ctx context.Context) *Problem {
	info, err := os.Stat("/dev/kvm")
	if err != nil {
		return &Problem{
			Summary: "/dev/kvm does not exist",
			Hint: "Load the kvm_intel or kvm_amd kernel module, or enable\n" +
				"VT-x / AMD-V in BIOS/UEFI firmware.",
		}
	}
	_ = info
	// Check actual r/w access (mode + group membership matter).
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return &Problem{
			Summary: "/dev/kvm exists but the current user lacks read+write access",
			Hint: "sudo usermod -aG kvm $USER\n" +
				"# then log out and back in for the group to take effect",
		}
	}
	_ = f.Close()
	return nil
}

func checkLibvirtGroup(ctx context.Context) *Problem {
	u, err := user.Current()
	if err != nil {
		return &Problem{Summary: "could not look up current user: " + err.Error()}
	}
	g, err := user.LookupGroup("libvirt")
	if err != nil {
		return &Problem{
			Summary: "libvirt group does not exist on this system",
			Hint:    "Install libvirt (see virsh hint above), then re-run.",
		}
	}
	gids, err := u.GroupIds()
	if err != nil {
		return &Problem{Summary: "could not list current user's groups: " + err.Error()}
	}
	for _, id := range gids {
		if id == g.Gid {
			return nil
		}
	}
	return &Problem{
		Summary: "current user is not in the libvirt group",
		Hint: "sudo usermod -aG libvirt $USER\n" +
			"# then log out and back in",
	}
}

func checkCPUVirt(ctx context.Context) *Problem {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return &Problem{Summary: "could not read /proc/cpuinfo: " + err.Error()}
	}
	if strings.Contains(string(data), "vmx") || strings.Contains(string(data), "svm") {
		return nil
	}
	return &Problem{
		Summary: "CPU does not expose virtualization extensions (vmx/svm)",
		Hint:    "Enable VT-x / AMD-V in BIOS/UEFI firmware.",
	}
}

func checkLibvirtd(ctx context.Context) *Problem {
	if _, err := exec.LookPath("virsh"); err != nil {
		return nil // covered by cmdCheck("virsh")
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(cctx, "virsh", "-c", "qemu:///system", "list").Run(); err != nil {
		return &Problem{
			Summary: "libvirtd is not reachable via qemu:///system",
			Hint:    "sudo systemctl enable --now libvirtd",
		}
	}
	return nil
}

func checkDefaultNetwork(ctx context.Context) *Problem {
	if _, err := exec.LookPath("virsh"); err != nil {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "virsh", "-c", "qemu:///system",
		"net-info", "default").Output()
	if err != nil {
		return &Problem{
			Summary: "libvirt default network is missing",
			Hint: "sudo virsh net-define /usr/share/libvirt/networks/default.xml\n" +
				"sudo virsh net-start default\n" +
				"sudo virsh net-autostart default",
		}
	}
	active := false
	autostart := false
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "Active":
			active = strings.EqualFold(v, "yes")
		case "Autostart":
			autostart = strings.EqualFold(v, "yes")
		}
	}
	if active && autostart {
		return nil
	}
	hint := ""
	if !active {
		hint += "sudo virsh net-start default\n"
	}
	if !autostart {
		hint += "sudo virsh net-autostart default\n"
	}
	return &Problem{
		Summary: "libvirt default network is not active and/or not set to autostart",
		Hint:    hint,
	}
}

func checkPoolPerms(ctx context.Context) *Problem {
	const path = "/var/lib/libvirt/images"
	info, err := os.Stat(path)
	if err != nil {
		return &Problem{
			Summary: "libvirt default storage pool path " + path + " is missing",
			Hint:    "sudo mkdir -p " + path,
		}
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	g, err := user.LookupGroup("libvirt")
	if err != nil {
		return nil // libvirt-group check covers this
	}
	hint := fmt.Sprintf("sudo chgrp libvirt %s\nsudo chmod g+rwxs  %s", path, path)
	if fmt.Sprint(st.Gid) != g.Gid {
		return &Problem{
			Summary: path + " is not group-owned by libvirt",
			Hint:    hint,
		}
	}
	mode := info.Mode()
	wantBits := os.ModeSetgid | 0o070
	if mode&wantBits != wantBits {
		return &Problem{
			Summary: path + " does not have g+rwxs",
			Hint:    hint,
		}
	}
	return nil
}

func checkSSHKey(ctx context.Context) *Problem {
	home, err := os.UserHomeDir()
	if err != nil {
		return &Problem{Summary: "cannot resolve home directory: " + err.Error()}
	}
	for _, name := range []string{"id_ed25519.pub", "id_rsa.pub"} {
		if _, err := os.Stat(home + "/.ssh/" + name); err == nil {
			return nil
		}
	}
	return &Problem{
		Summary: "no SSH public key found at ~/.ssh/id_ed25519.pub or ~/.ssh/id_rsa.pub",
		Hint:    "ssh-keygen -t ed25519",
	}
}

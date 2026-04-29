// Package doctor verifies the host environment can build and run workers.
//
// Two output modes:
//   - PrintProblems  — terse punch list of failures, used inline by `init`
//                      and `build` so a normal workflow doesn't get spammed
//                      on the success path.
//   - PrintVerbose   — full diagnostic with stack-layer headings and
//                      per-check Verifies/Inspect lines, used by the
//                      standalone `ai-playground doctor` subcommand. The
//                      verbose form is intentionally precise (libvirt
//                      domain, qemu:///system, /dev/kvm, vmx/svm, etc.) so
//                      a coding agent reading the output has enough
//                      context to debug without guessing.
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

	"github.com/rodrigomideac/ai-playground/internal/ui"
)

// Problem is a single failed check, ready to print as a punch-list entry.
type Problem struct {
	Summary string
	Hint    string // multi-line; rendered with each line indented
}

// Layer labels group checks by which slice of the KVM/qemu/libvirt stack
// they exercise. The order in which they appear here is the order the
// verbose printer prints them.
const (
	LayerCPU         = "Layer 0  CPU virtualization extensions"
	LayerKernel      = "Layer 1  Linux kernel KVM module"
	LayerQEMU        = "Layer 2  qemu userspace VMM"
	LayerLibvirtd    = "Layer 3  libvirtd daemon"
	LayerLibvirtCli  = "Layer 4  libvirt clients"
	LayerPacker      = "Layer 5  Packer (golden image builder)"
	LayerHostTooling = "Layer 6  ai-playground host tooling"
)

// Check is one host-state assertion.
type Check struct {
	Name     string
	Layer    string
	Cheap    bool
	Verifies string // 1-3 sentences explaining what this check actually asserts
	Inspect  string // shell command(s) the user can run to inspect manually
	Run      func(ctx context.Context) *Problem
}

// stackPreamble describes the layered architecture the doctor verifies.
// Printed once at the top of the verbose output. Intentionally precise —
// targeted at humans and coding agents debugging a broken host.
const stackPreamble = `ai-playground builds and runs Debian VMs through this stack:

  Layer 0  CPU virtualization extensions (Intel VT-x → 'vmx' flag,
           AMD-V → 'svm' flag in /proc/cpuinfo). Without these the
           kvm_intel/kvm_amd kernel module won't load and qemu falls back
           to TCG software emulation (~50× slower); '-enable-kvm' fails.

  Layer 1  Linux kernel KVM module (kvm + kvm_intel|kvm_amd). Exposes the
           character device /dev/kvm. Userspace processes that hold an
           open fd on /dev/kvm can run guest code in non-root mode via
           ioctl(KVM_CREATE_VM, KVM_RUN, ...). Access is gated by the
           file's mode (typically 0660) and group (typically 'kvm').

  Layer 2  qemu userspace VMM. The qemu-system-x86_64 binary is the
           per-VM process: it allocates guest RAM, emulates the chipset,
           BIOS, virtio devices, and the disk/NIC backends. With
           '-enable-kvm' it offloads the CPU/MMU hot path to /dev/kvm and
           only emulates I/O. qemu-img is the disk-image tool used to
           create qcow2 overlays backed by the golden image.

  Layer 3  libvirtd daemon. Long-running root process that supervises
           qemu-system-x86_64 children, owns domain XML at
           /etc/libvirt/qemu/<domain>.xml, manages storage pools
           (typically /var/lib/libvirt/images), and creates the default
           virtual network (virbr0 + dnsmasq for DHCP/DNS, NAT via
           iptables). Reachable on the system bus at qemu:///system —
           a UNIX socket at /var/run/libvirt/libvirt-sock, mode 0770
           owned by group 'libvirt'.

  Layer 4  libvirt clients. virsh is the canonical CLI; virt-install
           constructs domain XML from CLI arguments. Both connect to
           libvirtd over the qemu:///system socket. ai-playground itself
           shells out to these.

  Layer 5  Packer (HashiCorp). Drives qemu-system-x86_64 directly (not
           through libvirt) to boot the upstream Debian cloud image, run
           provision scripts over SSH, sysprep, and capture the final
           qcow2 as the golden image. Note: a similarly-named 'packer'
           binary ships with cracklib on Fedora/CentOS — a different
           tool. The doctor distinguishes them via 'packer version'.

  Layer 6  ai-playground host tooling. xorriso (NoCloud seed ISO),
           ssh-keygen (build-only ed25519 keypair), git (cloning the
           public repo into the cache), curl (used by some provision
           scripts).
`

// All returns the full set of checks in stack order.
func All() []Check {
	return []Check{
		// Layer 0 — CPU
		{
			Name:     "CPU exposes vmx or svm in /proc/cpuinfo",
			Layer:    LayerCPU,
			Verifies: "/proc/cpuinfo lists Intel VT-x ('vmx') or AMD-V ('svm') CPU flags. Without one of these, the kvm_intel/kvm_amd kernel module fails to load and qemu can't use -enable-kvm.",
			Inspect:  "grep -oE '(vmx|svm)' /proc/cpuinfo | sort -u",
			Run:      checkCPUVirt,
		},

		// Layer 1 — kernel/KVM
		{
			Name:     "/dev/kvm character device is read+write by current user",
			Layer:    LayerKernel,
			Cheap:    true,
			Verifies: "Opens /dev/kvm with O_RDWR. qemu calls ioctl(KVM_CREATE_VM) on this fd for every -enable-kvm invocation; failure surfaces as 'Could not access KVM kernel module'. The device's mode is typically 0660 with group 'kvm', so success here also implies kvm-group membership.",
			Inspect:  "ls -l /dev/kvm; stat -c '%U:%G %a' /dev/kvm; id -nG | tr ' ' '\\n' | grep -qx kvm && echo 'in kvm group' || echo 'NOT in kvm group'",
			Run:      checkKVMAccess,
		},

		// Layer 2 — qemu
		cmdCheck("qemu-system-x86_64",
			LayerQEMU,
			"qemu's per-VM userspace VMM binary. Drives the guest CPU/MMU via /dev/kvm ioctls and emulates the chipset, BIOS, and virtio devices. Invoked directly by Packer at build time and indirectly (via libvirtd) at worker runtime.",
			"command -v qemu-system-x86_64 && qemu-system-x86_64 --version | head -1",
			"Fedora: dnf install @virtualization | Ubuntu: apt install qemu-system-x86 | Arch: pacman -S qemu-desktop"),
		cmdCheck("qemu-img",
			LayerQEMU,
			"qemu's disk-image utility. ai-playground shells out to 'qemu-img create -f qcow2 -F qcow2 -b GOLDEN OVERLAY' for every add-worker.",
			"command -v qemu-img && qemu-img --version | head -1",
			"Shipped with qemu (see qemu-system-x86_64 hint)."),

		// Layer 3 — libvirtd
		{
			Name:     "current user is a member of group 'libvirt'",
			Layer:    LayerLibvirtd,
			Cheap:    true,
			Verifies: "libvirtd's UNIX socket /var/run/libvirt/libvirt-sock is mode 0770 owned by user/group 'libvirt'. Non-members get EACCES on connect, so virsh -c qemu:///system fails for everything.",
			Inspect:  "ls -l /var/run/libvirt/libvirt-sock; id -nG | tr ' ' '\\n' | grep -qx libvirt && echo 'in libvirt group' || echo 'NOT in libvirt group'",
			Run:      checkLibvirtGroup,
		},
		{
			Name:     "libvirtd is reachable via qemu:///system",
			Layer:    LayerLibvirtd,
			Verifies: "Runs 'virsh -c qemu:///system list' and asserts exit 0. Failure means the libvirtd systemd unit is masked/stopped, the socket isn't being created, or this user can't reach it (kernel namespace, AppArmor/SELinux). qemu:///system is the system-wide instance — qemu:///session is per-user but uses SLIRP networking and is unsupported by ai-playground.",
			Inspect:  "systemctl status libvirtd; virsh -c qemu:///system list --all",
			Run:      checkLibvirtd,
		},
		{
			Name:     "libvirt 'default' virtual network is active and autostart=yes",
			Layer:    LayerLibvirtd,
			Cheap:    true,
			Verifies: "Parses 'virsh net-info default' for Active: yes and Autostart: yes. The 'default' network creates virbr0, runs a dnsmasq instance for DHCP/DNS on 192.168.122.0/24, and sets iptables NAT rules. Without it, workers boot but never get a DHCP lease and 'virsh domifaddr' returns empty.",
			Inspect:  "virsh -c qemu:///system net-info default; virsh -c qemu:///system net-dumpxml default",
			Run:      checkDefaultNetwork,
		},
		{
			Name:     "/var/lib/libvirt/images is group=libvirt with mode g+rwxs",
			Layer:    LayerLibvirtd,
			Cheap:    true,
			Verifies: "The default storage pool path. ai-playground writes worker overlays here as the calling user, so the directory must be group-writable (g+rwx) and group-owned by 'libvirt'. The setgid bit (the 's') makes new files inherit group=libvirt, which interacts correctly with libvirt's dynamic_ownership (libvirtd chowns disk files to qemu/libvirt-qemu when starting a VM).",
			Inspect:  "stat -c '%U:%G %a' /var/lib/libvirt/images; ls -ld /var/lib/libvirt/images",
			Run:      checkPoolPerms,
		},

		// Layer 4 — libvirt clients
		cmdCheck("virsh",
			LayerLibvirtCli,
			"libvirt's CLI client. ai-playground shells out to virsh for: list/destroy/undefine domains, dominfo, domstate, domifaddr (DHCP lease), pool-dumpxml (storage path), pool-refresh, net-info, net-dhcp-leases.",
			"command -v virsh && virsh --version",
			"Fedora: dnf install libvirt-client | Ubuntu: apt install libvirt-clients | Arch: pacman -S libvirt"),
		cmdCheck("virt-install",
			LayerLibvirtCli,
			"libvirt's domain-creation tool. Constructs domain XML from CLI flags (--memory, --vcpus, --disk, --network, --machine, --video, ...) and submits it to libvirtd via the libvirt API. add-worker invokes this with --import to define + start a worker from an existing qcow2 overlay.",
			"command -v virt-install && virt-install --version",
			"Fedora: dnf install virt-install | Ubuntu: apt install virtinst | Arch: pacman -S virt-install"),

		// Layer 5 — Packer
		cmdCheck("packer",
			LayerPacker,
			"HashiCorp Packer. The 'build' subcommand drives qemu-system-x86_64 directly (not through libvirt) to boot the upstream Debian cloud image, SSH in with the build-only ed25519 keypair, run packer/provision/*.sh in numeric order, sysprep, and write the final qcow2 to ARTIFACT_DIR.",
			"command -v packer && packer version",
			"https://developer.hashicorp.com/packer/install"),
		{
			Name:     "'packer' binary is HashiCorp Packer (not cracklib-packer)",
			Layer:    LayerPacker,
			Verifies: "Runs 'packer version' and asserts the first output line starts with 'Packer v'. Fedora/CentOS ship a 'packer' binary as part of cracklib (a different tool); if it's first on PATH our 'packer init' invocation will fail with a confusing error.",
			Inspect:  "command -v packer; packer version 2>&1 | head -1",
			Run:      checkPackerIsHashicorp,
		},

		// Layer 6 — host tooling
		cmdCheck("git",
			LayerHostTooling,
			"Used to clone https://github.com/rodrigomideac/ai-playground into $XDG_CACHE_HOME/ai-playground/repo/ on first init/build, and to fetch+reset --hard origin/master on subsequent runs. Bypassed entirely when --repo-path or AI_PLAYGROUND_REPO is set.",
			"command -v git && git --version",
			"Fedora: dnf install git | Ubuntu: apt install git | Arch: pacman -S git"),
		cmdCheck("curl",
			LayerHostTooling,
			"Used by some provision scripts (e.g. claude-code installer, get-docker.sh) but not by the CLI itself. Listed here because a missing curl breaks the build inside the VM, not on the host.",
			"command -v curl && curl --version | head -1",
			"Fedora: dnf install curl | Ubuntu: apt install curl | Arch: pacman -S curl"),
		cmdCheck("xorriso",
			LayerHostTooling,
			"ISO 9660 packer used to build the per-worker NoCloud seed (cidata.iso containing user-data + meta-data). Invoked as 'xorriso -as mkisofs -volid CIDATA -joliet -rock -output OUT user-data meta-data'. cloud-init's NoCloud datasource looks for the CIDATA volume label.",
			"command -v xorriso && xorriso --version 2>&1 | head -1",
			"Fedora: dnf install xorriso | Ubuntu: apt install xorriso | Arch: pacman -S libisoburn"),
		cmdCheck("ssh-keygen",
			LayerHostTooling,
			"Generates the build-only ed25519 keypair under $XDG_CACHE_HOME/ai-playground/seed/id_ed25519. The public key is injected via cloud-init at Packer build time so Packer can SSH in as 'debian'; the matching authorized_keys is wiped by 'userdel -rf debian' as the last act of the build, so the keypair never reaches the produced golden image.",
			"command -v ssh-keygen && ssh-keygen -V 2>&1 | head -1 || ssh -V 2>&1",
			"Fedora: dnf install openssh-clients | Ubuntu: apt install openssh-client | Arch: pacman -S openssh"),
		{
			Name:     "an SSH public key is available at ~/.ssh/id_ed25519.pub or ~/.ssh/id_rsa.pub",
			Layer:    LayerHostTooling,
			Verifies: "Stat's both filenames in $HOME/.ssh. The first one found is read by add-worker as a literal string and embedded in the per-worker NoCloud seed under users[0].ssh_authorized_keys. This is the key used to ssh-worker into a running VM. Distinct from the build-only keypair generated under the seed cache.",
			Inspect:  "ls -l ~/.ssh/id_ed25519.pub ~/.ssh/id_rsa.pub 2>/dev/null",
			Run:      checkSSHKey,
		},
	}
}

// Run executes the requested subset of checks (full or cheap) and
// returns only the failures. Used by inline init/build callers.
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

// Result pairs a Check with the outcome of running it.
type Result struct {
	Check   Check
	Problem *Problem // nil → passed
}

// RunAll returns one Result per check, in stack order, including
// passing checks. Used by the standalone `doctor` subcommand.
func RunAll(ctx context.Context) []Result {
	checks := All()
	out := make([]Result, len(checks))
	for i, c := range checks {
		out[i] = Result{Check: c, Problem: c.Run(ctx)}
	}
	return out
}

// PrintProblems writes a colored punch list to w. Used inline by init/build
// when at least one check fails.
func PrintProblems(w io.Writer, problems []Problem) {
	if len(problems) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s %s\n",
		ui.Red("✗"),
		ui.Bold(fmt.Sprintf("doctor: %d issue(s) need to be fixed before continuing:", len(problems))))
	for _, p := range problems {
		fmt.Fprintf(w, "  %s %s\n", ui.Red("-"), p.Summary)
		for _, line := range strings.Split(strings.TrimRight(p.Hint, "\n"), "\n") {
			if line == "" {
				continue
			}
			fmt.Fprintf(w, "      %s\n", ui.Dim(line))
		}
	}
	fmt.Fprintf(w, "\n%s\n", ui.Dim("Run 'ai-playground doctor' for the full diagnostic with stack-layer context."))
}

// PrintVerbose writes the full diagnostic — stack preamble, all checks
// (passing and failing) grouped by stack layer, with each check's
// Verifies and Inspect lines. Used by the `ai-playground doctor`
// subcommand.
func PrintVerbose(w io.Writer, results []Result) {
	fmt.Fprintln(w, ui.Bold("ai-playground doctor — KVM/qemu/libvirt stack diagnostic"))
	fmt.Fprintln(w)
	fmt.Fprint(w, stackPreamble)
	fmt.Fprintln(w)

	// Group by Layer, preserving the order in All().
	currentLayer := ""
	failures := 0
	for _, r := range results {
		if r.Check.Layer != currentLayer {
			fmt.Fprintf(w, "\n%s\n", ui.Bold(r.Check.Layer))
			currentLayer = r.Check.Layer
		}
		marker := ui.Green("✓")
		statusLine := r.Check.Name
		if r.Problem != nil {
			marker = ui.Red("✗")
			statusLine = r.Problem.Summary
			failures++
		}
		fmt.Fprintf(w, "  %s %s\n", marker, statusLine)
		if r.Check.Verifies != "" {
			fmt.Fprintf(w, "      %s %s\n", ui.Dim("Verifies:"), wrapDim(r.Check.Verifies, "      "+strings.Repeat(" ", len("Verifies: "))))
		}
		if r.Check.Inspect != "" {
			fmt.Fprintf(w, "      %s %s\n", ui.Dim("Inspect: "), ui.Dim(r.Check.Inspect))
		}
		if r.Problem != nil && r.Problem.Hint != "" {
			fmt.Fprintf(w, "      %s\n", ui.Yellow("Fix:"))
			for _, line := range strings.Split(strings.TrimRight(r.Problem.Hint, "\n"), "\n") {
				if line == "" {
					continue
				}
				fmt.Fprintf(w, "        %s\n", line)
			}
		}
	}

	fmt.Fprintln(w)
	if failures == 0 {
		fmt.Fprintf(w, "%s All %d checks passed.\n", ui.Green("✓"), len(results))
	} else {
		fmt.Fprintf(w, "%s %d of %d check(s) failed.\n", ui.Red("✗"), failures, len(results))
	}
}

// wrapDim re-wraps a long string at ~88 columns, indenting continuation
// lines so they line up under the first word after a label like
// "Verifies: ". Each visible line is wrapped in dim escape codes.
func wrapDim(text, contIndent string) string {
	const width = 80
	words := strings.Fields(text)
	var lines []string
	var line strings.Builder
	for _, w := range words {
		if line.Len() == 0 {
			line.WriteString(w)
			continue
		}
		if line.Len()+1+len(w) > width {
			lines = append(lines, line.String())
			line.Reset()
			line.WriteString(w)
			continue
		}
		line.WriteByte(' ')
		line.WriteString(w)
	}
	if line.Len() > 0 {
		lines = append(lines, line.String())
	}
	for i := range lines {
		lines[i] = ui.Dim(lines[i])
	}
	return strings.Join(lines, "\n"+contIndent)
}

// cmdCheck builds a "<name> is on PATH" check at the given stack layer.
func cmdCheck(name, layer, verifies, inspect, hint string) Check {
	return Check{
		Name:     name + " is on PATH",
		Layer:    layer,
		Verifies: verifies,
		Inspect:  inspect,
		Run: func(ctx context.Context) *Problem {
			if _, err := exec.LookPath(name); err != nil {
				return &Problem{Summary: name + " is not on PATH", Hint: hint}
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
		Summary: "the 'packer' binary on PATH is not HashiCorp Packer (likely cracklib-packer)",
		Hint: "Fedora/CentOS: sudo dnf remove cracklib-packer\n" +
			"Then install HashiCorp Packer: https://developer.hashicorp.com/packer/install",
	}
}

func checkKVMAccess(ctx context.Context) *Problem {
	info, err := os.Stat("/dev/kvm")
	if err != nil {
		return &Problem{
			Summary: "/dev/kvm character device is missing",
			Hint: "Load the kvm_intel or kvm_amd kernel module (modprobe kvm_intel),\n" +
				"or enable Intel VT-x / AMD-V in BIOS/UEFI firmware.",
		}
	}
	_ = info
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return &Problem{
			Summary: "/dev/kvm exists but the current user can't open it O_RDWR",
			Hint: "sudo usermod -aG kvm $USER\n" +
				"# then log out and back in for the new gid to take effect on /dev/kvm (mode 0660 group=kvm)",
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
			Summary: "POSIX group 'libvirt' does not exist on this system",
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
		Summary: "current user is not a member of POSIX group 'libvirt'",
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
		Summary: "CPU does not expose virtualization extensions (no 'vmx' or 'svm' flag in /proc/cpuinfo)",
		Hint:    "Enable Intel VT-x / AMD-V in BIOS/UEFI firmware.",
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
			Summary: "libvirtd is not reachable on the qemu:///system socket",
			Hint: "sudo systemctl enable --now libvirtd\n" +
				"# also confirm /var/run/libvirt/libvirt-sock exists and your user is in group 'libvirt'",
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
			Summary: "libvirt 'default' virtual network is not defined",
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
		Summary: "libvirt 'default' network is defined but not Active=yes and Autostart=yes",
		Hint:    hint,
	}
}

func checkPoolPerms(ctx context.Context) *Problem {
	const path = "/var/lib/libvirt/images"
	info, err := os.Stat(path)
	if err != nil {
		return &Problem{
			Summary: "default storage pool path " + path + " does not exist",
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
			Summary: path + " is not group-owned by 'libvirt'",
			Hint:    hint,
		}
	}
	mode := info.Mode()
	wantBits := os.ModeSetgid | 0o070
	if mode&wantBits != wantBits {
		return &Problem{
			Summary: path + " does not have mode g+rwxs (group rwx + setgid)",
			Hint:    hint,
		}
	}
	return nil
}

func checkSSHKey(ctx context.Context) *Problem {
	home, err := os.UserHomeDir()
	if err != nil {
		return &Problem{Summary: "cannot resolve $HOME: " + err.Error()}
	}
	for _, name := range []string{"id_ed25519.pub", "id_rsa.pub"} {
		if _, err := os.Stat(home + "/.ssh/" + name); err == nil {
			return nil
		}
	}
	return &Problem{
		Summary: "no SSH public key at ~/.ssh/id_ed25519.pub or ~/.ssh/id_rsa.pub",
		Hint:    "ssh-keygen -t ed25519",
	}
}

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
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rodrigomideac/ai-playground/cli/internal/paths"
	"github.com/rodrigomideac/ai-playground/cli/internal/ui"
)

// Problem is a single failed check, ready to print as a punch-list entry.
//
// A check provides EITHER structured fix data (Packages and/or Commands) or
// a free-form Hint (or both). Structured data is what powers the consolidated
// "Quick fix" summary block at the bottom of doctor output — it's collated
// across all problems and rendered as a single per-distro install command +
// follow-up commands. Hint stays free-form for things that can't be expressed
// as a package install or a literal command (BIOS settings, "log out and
// back in", etc.).
type Problem struct {
	Summary  string
	Hint     string                    // multi-line free-form note, rendered indented
	Packages map[paths.Distro][]string // optional; distro→package list to install
	Commands []string                  // optional; sudo / shell commands to run, in order
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
           qcow2 as the golden image. Vendored: ai-playground downloads
           a SHA256-pinned release into $XDG_DATA_HOME/ai-playground/bin/
           on first build, so the host's PATH packer (which on Fedora/
           CentOS may shadow with cracklib's 'packer' tool) is ignored.

  Layer 6  ai-playground host tooling. ssh-keygen (build-only ed25519
           keypair), git (cloning the public repo into the cache),
           curl (used by some provision scripts). The per-worker
           NoCloud seed ISO is built in-process via go-diskfs, so no
           external ISO tool is required.
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
			map[paths.Distro][]string{
				paths.DistroDebian: {"qemu-system-x86", "qemu-utils"},
				paths.DistroFedora: {"@virtualization"},
				paths.DistroArch:   {"qemu-desktop"},
			}),
		cmdCheck("qemu-img",
			LayerQEMU,
			"qemu's disk-image utility. ai-playground shells out to 'qemu-img create -f qcow2 -F qcow2 -b GOLDEN OVERLAY' for every add-worker.",
			"command -v qemu-img && qemu-img --version | head -1",
			map[paths.Distro][]string{
				paths.DistroDebian: {"qemu-utils"},
				paths.DistroFedora: {"@virtualization"},
				paths.DistroArch:   {"qemu-desktop"},
			}),

		// Layer 3 — libvirtd
		{
			Name:     "current process has the 'libvirt' supplementary gid",
			Layer:    LayerLibvirtd,
			Cheap:    true,
			Verifies: "libvirtd's UNIX socket /var/run/libvirt/libvirt-sock is mode 0770 owned by user/group 'libvirt'. Non-members get EACCES on connect, so virsh -c qemu:///system fails. Authoritative test: os.Getgroups() (kernel process credentials) — usermod updates /etc/group immediately but the running shell's credentials are only refreshed by a new login session.",
			Inspect:  "ls -l /var/run/libvirt/libvirt-sock; id -G | tr ' ' '\\n' | grep -qx \"$(getent group libvirt | cut -d: -f3)\" && echo 'in libvirt group (process)' || echo 'NOT in libvirt group (process credentials)'",
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
			map[paths.Distro][]string{
				paths.DistroDebian: {"libvirt-clients", "libvirt-daemon-system"},
				paths.DistroFedora: {"libvirt-client", "libvirt-daemon"},
				paths.DistroArch:   {"libvirt"},
			}),
		cmdCheck("virt-install",
			LayerLibvirtCli,
			"libvirt's domain-creation tool. Constructs domain XML from CLI flags (--memory, --vcpus, --disk, --network, --machine, --video, ...) and submits it to libvirtd via the libvirt API. add-worker invokes this with --import to define + start a worker from an existing qcow2 overlay.",
			"command -v virt-install && virt-install --version",
			map[paths.Distro][]string{
				paths.DistroDebian: {"virtinst"},
				paths.DistroFedora: {"virt-install"},
				paths.DistroArch:   {"virt-install"},
			}),

		// Layer 5 — Packer is vendored by 'ai-playground build' on first
		// run (see internal/vendoring/packer); no host check needed.

		// Layer 6 — host tooling
		cmdCheck("git",
			LayerHostTooling,
			"Used to clone https://github.com/rodrigomideac/ai-playground into $XDG_CACHE_HOME/ai-playground/repo/ on first init/build, and to fetch+reset --hard origin/master on subsequent runs. Bypassed entirely when --repo-path or AI_PLAYGROUND_REPO is set.",
			"command -v git && git --version",
			map[paths.Distro][]string{
				paths.DistroDebian: {"git"},
				paths.DistroFedora: {"git"},
				paths.DistroArch:   {"git"},
			}),
		cmdCheck("curl",
			LayerHostTooling,
			"Used by some provision scripts (e.g. claude-code installer, get-docker.sh) but not by the CLI itself. Listed here because a missing curl breaks the build inside the VM, not on the host.",
			"command -v curl && curl --version | head -1",
			map[paths.Distro][]string{
				paths.DistroDebian: {"curl"},
				paths.DistroFedora: {"curl"},
				paths.DistroArch:   {"curl"},
			}),
		cmdCheck("ssh-keygen",
			LayerHostTooling,
			"Generates the build-only ed25519 keypair under $XDG_CACHE_HOME/ai-playground/seed/id_ed25519. The public key is injected via cloud-init at Packer build time so Packer can SSH in as 'debian'; the matching authorized_keys is wiped by 'userdel -rf debian' as the last act of the build, so the keypair never reaches the produced golden image.",
			"command -v ssh-keygen && ssh-keygen -V 2>&1 | head -1 || ssh -V 2>&1",
			map[paths.Distro][]string{
				paths.DistroDebian: {"openssh-client"},
				paths.DistroFedora: {"openssh-clients"},
				paths.DistroArch:   {"openssh"},
			}),
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
		for _, line := range fixLines(&p) {
			fmt.Fprintf(w, "      %s\n", ui.Dim(line))
		}
	}
	Summarize(w, problems)
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
		if r.Problem != nil {
			lines := fixLines(r.Problem)
			if len(lines) > 0 {
				fmt.Fprintf(w, "      %s\n", ui.Yellow("Fix:"))
				for _, line := range lines {
					fmt.Fprintf(w, "        %s\n", line)
				}
			}
		}
	}

	fmt.Fprintln(w)
	if failures == 0 {
		fmt.Fprintf(w, "%s All %d checks passed.\n", ui.Green("✓"), len(results))
		return
	}
	fmt.Fprintf(w, "%s %d of %d check(s) failed.\n", ui.Red("✗"), failures, len(results))

	probs := make([]Problem, 0, failures)
	for _, r := range results {
		if r.Problem != nil {
			probs = append(probs, *r.Problem)
		}
	}
	Summarize(w, probs)
}

// distroOrder controls the order distros appear in per-check Fix lines and
// in the labelled fallback summary when DetectDistro returned Unknown.
var distroOrder = []paths.Distro{paths.DistroDebian, paths.DistroFedora, paths.DistroArch}

// distroLabel is the human label used in per-check Fix lines (e.g.
// "Debian/Ubuntu: apt install …"). Avoid the precise-register
// "DistroDebian" enum spelling — this is the user-facing surface.
func distroLabel(d paths.Distro) string {
	switch d {
	case paths.DistroDebian:
		return "Debian/Ubuntu"
	case paths.DistroFedora:
		return "Fedora/RHEL"
	case paths.DistroArch:
		return "Arch"
	}
	return d.String()
}

// installCmd renders "<pkg-mgr> install <pkgs>" for the given distro family.
// The leading "sudo " is added by callers that need it; the per-check Fix
// line shows three side-by-side commands without sudo (tighter), while the
// consolidated Quick fix block prepends sudo (a copy-pasteable command).
func installCmd(d paths.Distro, pkgs []string) string {
	if len(pkgs) == 0 {
		return ""
	}
	switch d {
	case paths.DistroDebian:
		return "apt install " + strings.Join(pkgs, " ")
	case paths.DistroFedora:
		return "dnf install " + strings.Join(pkgs, " ")
	case paths.DistroArch:
		return "pacman -S " + strings.Join(pkgs, " ")
	}
	return ""
}

// formatPerCheckPackages renders "Debian/Ubuntu: apt install … | Fedora: …"
// for the per-check Fix block.
func formatPerCheckPackages(pkgs map[paths.Distro][]string) string {
	var parts []string
	for _, d := range distroOrder {
		if list, ok := pkgs[d]; ok && len(list) > 0 {
			parts = append(parts, fmt.Sprintf("%s: %s", distroLabel(d), installCmd(d, list)))
		}
	}
	return strings.Join(parts, " | ")
}

// fixLines flattens a Problem's structured fix data + free-form Hint into
// a sequence of lines ready to be indented under "Fix:". Blank lines from
// Hint are dropped.
func fixLines(p *Problem) []string {
	var lines []string
	if pkgLine := formatPerCheckPackages(p.Packages); pkgLine != "" {
		lines = append(lines, pkgLine)
	}
	lines = append(lines, p.Commands...)
	for _, line := range strings.Split(strings.TrimRight(p.Hint, "\n"), "\n") {
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// Summarize writes a consolidated "Quick fix" block aggregated across all
// problems and tailored to the detected Linux distribution. When the host
// distro is unknown it falls back to a per-distro labelled list so the user
// can pick the right column themselves.
//
// The block is intentionally action-first: a single install command with
// every missing package deduplicated, then any follow-up shell commands in
// the order their checks fired (which matches the dependency order — install
// before usermod, usermod before service start, etc.).
func Summarize(w io.Writer, problems []Problem) {
	if len(problems) == 0 {
		return
	}

	pkgsByDistro := map[paths.Distro][]string{}
	seenPkg := map[paths.Distro]map[string]bool{}
	for _, d := range distroOrder {
		seenPkg[d] = map[string]bool{}
	}
	var cmds []string
	seenCmd := map[string]bool{}
	var notes []string
	seenNote := map[string]bool{}

	for _, p := range problems {
		for _, d := range distroOrder {
			for _, pk := range p.Packages[d] {
				if seenPkg[d][pk] {
					continue
				}
				seenPkg[d][pk] = true
				pkgsByDistro[d] = append(pkgsByDistro[d], pk)
			}
		}
		for _, c := range p.Commands {
			if seenCmd[c] {
				continue
			}
			seenCmd[c] = true
			cmds = append(cmds, c)
		}
		for _, line := range strings.Split(strings.TrimRight(p.Hint, "\n"), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || seenNote[line] {
				continue
			}
			seenNote[line] = true
			notes = append(notes, line)
		}
	}

	d, raw, _ := paths.DetectDistro()
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s %s\n", ui.Yellow("→"), ui.Bold("Quick fix"))

	if d == paths.DistroUnknown {
		label := raw
		if label == "" {
			label = "unknown"
		}
		fmt.Fprintf(w, "  %s\n", ui.Dim(fmt.Sprintf("(distro %q is not auto-supported — pick the column for your package manager)", label)))
		distros := make([]paths.Distro, 0, len(pkgsByDistro))
		for k := range pkgsByDistro {
			distros = append(distros, k)
		}
		sort.Slice(distros, func(i, j int) bool { return distros[i] < distros[j] })
		for _, dk := range distros {
			if cmd := installCmd(dk, pkgsByDistro[dk]); cmd != "" {
				fmt.Fprintf(w, "    %s  %s\n", ui.Bold(distroLabel(dk)+":"), "sudo "+cmd)
			}
		}
	} else {
		fmt.Fprintf(w, "  %s\n", ui.Dim(fmt.Sprintf("(detected %s family — %s)", d, raw)))
		if cmd := installCmd(d, pkgsByDistro[d]); cmd != "" {
			fmt.Fprintf(w, "    %s\n", "sudo "+cmd)
		}
	}

	for _, c := range cmds {
		fmt.Fprintf(w, "    %s\n", c)
	}
	for _, n := range notes {
		fmt.Fprintf(w, "    %s\n", ui.Dim(n))
	}
	fmt.Fprintf(w, "\n  %s\n", ui.Dim("Then re-run 'ai-playground doctor' to verify."))
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
// Pkgs is the per-distro install package list — used both for the per-check
// Fix line and aggregated into the consolidated "Quick fix" summary.
func cmdCheck(name, layer, verifies, inspect string, pkgs map[paths.Distro][]string) Check {
	return Check{
		Name:     name + " is on PATH",
		Layer:    layer,
		Verifies: verifies,
		Inspect:  inspect,
		Run: func(ctx context.Context) *Problem {
			if _, err := exec.LookPath(name); err != nil {
				return &Problem{
					Summary:  name + " is not on PATH",
					Packages: pkgs,
				}
			}
			return nil
		},
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
			Summary:  "/dev/kvm exists but the current user can't open it O_RDWR",
			Commands: []string{"sudo usermod -aG kvm $USER"},
			Hint:     "# then log out and back in for the new gid to take effect on /dev/kvm (mode 0660 group=kvm)",
		}
	}
	_ = f.Close()
	return nil
}

func checkLibvirtGroup(ctx context.Context) *Problem {
	g, err := user.LookupGroup("libvirt")
	if err != nil {
		// The libvirt group is created by the libvirt-daemon-system /
		// libvirt-daemon / libvirt package. Ship the install set here so the
		// user gets a single command rather than a "see virsh hint above".
		return &Problem{
			Summary: "POSIX group 'libvirt' does not exist on this system",
			Packages: map[paths.Distro][]string{
				paths.DistroDebian: {"libvirt-daemon-system"},
				paths.DistroFedora: {"libvirt-daemon"},
				paths.DistroArch:   {"libvirt"},
			},
			Hint: "After install, re-run 'ai-playground doctor' to pick up the new group.",
		}
	}
	libvirtGid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return &Problem{Summary: "libvirt group has non-numeric gid: " + g.Gid}
	}
	// Authoritative test: kernel-credential supplementary groups of *this*
	// process. /etc/group membership (what u.GroupIds() reads) updates the
	// instant 'usermod -aG' runs, but the running shell's process keeps its
	// pre-usermod gid set until a new login session is started. The libvirt
	// socket's mode 0770 + group=libvirt is gated by the kernel credentials,
	// so this is what actually controls whether 'virsh -c qemu:///system'
	// succeeds. Same idea as checkKVMAccess opening /dev/kvm — let the kernel
	// answer.
	procGroups, err := os.Getgroups()
	if err != nil {
		return &Problem{Summary: "could not read process supplementary groups: " + err.Error()}
	}
	for _, id := range procGroups {
		if id == libvirtGid {
			return nil
		}
	}
	// Process doesn't have libvirt gid. Distinguish "user never got added"
	// (needs usermod) from "added but this shell predates the change" (needs
	// a fresh login session). The two require different fixes.
	u, err := user.Current()
	if err != nil {
		return &Problem{Summary: "could not look up current user: " + err.Error()}
	}
	onPaperGids, err := u.GroupIds()
	if err != nil {
		return &Problem{Summary: "could not list current user's on-paper groups: " + err.Error()}
	}
	onPaper := false
	for _, id := range onPaperGids {
		if id == g.Gid {
			onPaper = true
			break
		}
	}
	if !onPaper {
		return &Problem{
			Summary:  "current user is not a member of POSIX group 'libvirt'",
			Commands: []string{"sudo usermod -aG libvirt $USER"},
			Hint:     "# then log out and log back in for the new gid to apply to your shell",
		}
	}
	return &Problem{
		Summary: "this shell session is missing the 'libvirt' gid (usermod ran but the shell predates it)",
		Hint: "Log out and log back in (a fresh login session re-fetches the supplementary group set).\n" +
			"# alternative: 'exec su - $USER' starts a new login session in this terminal",
	}
}

// processInLibvirtGroup reports whether the libvirt gid is in this process's
// kernel-credential supplementary group set. Used by the libvirtd /
// default-network checks to decline reporting downstream socket-EACCES
// failures whose root cause is checkLibvirtGroup's responsibility.
func processInLibvirtGroup() bool {
	g, err := user.LookupGroup("libvirt")
	if err != nil {
		return false
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return false
	}
	procGroups, err := os.Getgroups()
	if err != nil {
		return false
	}
	for _, id := range procGroups {
		if id == gid {
			return true
		}
	}
	return false
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
	const sockPath = "/var/run/libvirt/libvirt-sock"
	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		return &Problem{
			Summary:  "libvirtd is not running (socket " + sockPath + " is missing)",
			Commands: []string{"sudo systemctl enable --now libvirtd"},
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(cctx, "virsh", "-c", "qemu:///system", "list").Run(); err != nil {
		// Socket is present but virsh failed. The most common cause is
		// missing libvirt gid in the process credentials — the libvirt-group
		// check is the authority for that, so don't double-report (otherwise
		// the Quick fix block ends up suggesting 'systemctl enable libvirtd'
		// which is a no-op for a permission problem).
		if !processInLibvirtGroup() {
			return nil
		}
		return &Problem{
			Summary: "libvirtd socket is present but 'virsh -c qemu:///system list' failed",
			Hint:    "Inspect: systemctl status libvirtd; journalctl -u libvirtd --no-pager | tail -50",
		}
	}
	return nil
}

func checkDefaultNetwork(ctx context.Context) *Problem {
	if _, err := exec.LookPath("virsh"); err != nil {
		return nil
	}
	// Without libvirt gid in process credentials, virsh net-info would fail
	// with EACCES on the socket and the error would be misread as "network
	// not defined". checkLibvirtGroup is the authority for that case.
	if !processInLibvirtGroup() {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "virsh", "-c", "qemu:///system",
		"net-info", "default").Output()
	if err != nil {
		return &Problem{
			Summary: "libvirt 'default' virtual network is not defined",
			Commands: []string{
				"sudo virsh net-define /usr/share/libvirt/networks/default.xml",
				"sudo virsh net-start default",
				"sudo virsh net-autostart default",
			},
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
	var cmds []string
	if !active {
		cmds = append(cmds, "sudo virsh net-start default")
	}
	if !autostart {
		cmds = append(cmds, "sudo virsh net-autostart default")
	}
	return &Problem{
		Summary:  "libvirt 'default' network is defined but not Active=yes and Autostart=yes",
		Commands: cmds,
	}
}

func checkPoolPerms(ctx context.Context) *Problem {
	const path = "/var/lib/libvirt/images"
	info, err := os.Stat(path)
	if err != nil {
		// Bundle mkdir + chgrp + chmod so the user fixes this in one shot
		// rather than re-running doctor between each step.
		return &Problem{
			Summary: "default storage pool path " + path + " does not exist",
			Commands: []string{
				"sudo mkdir -p " + path,
				"sudo chgrp libvirt " + path,
				"sudo chmod g+rwxs " + path,
			},
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
	cmds := []string{
		"sudo chgrp libvirt " + path,
		"sudo chmod g+rwxs " + path,
	}
	if fmt.Sprint(st.Gid) != g.Gid {
		return &Problem{
			Summary:  path + " is not group-owned by 'libvirt'",
			Commands: cmds,
		}
	}
	mode := info.Mode()
	wantBits := os.ModeSetgid | 0o070
	if mode&wantBits != wantBits {
		return &Problem{
			Summary:  path + " does not have mode g+rwxs (group rwx + setgid)",
			Commands: cmds,
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
		Summary:  "no SSH public key at ~/.ssh/id_ed25519.pub or ~/.ssh/id_rsa.pub",
		Commands: []string{"ssh-keygen -t ed25519"},
	}
}

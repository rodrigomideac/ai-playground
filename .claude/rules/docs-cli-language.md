---
paths:
  - "cli/cmd/ai-playground/**"
  - "cli/internal/ui/**"
  - "cli/internal/doctor/**"
  - "cli/internal/worker/manager.go"
  - "cli/internal/worker/worker.go"
  - "cli/internal/buildflow/**"
  - "cli/internal/repo/**"
  - "cli/internal/seed/**"
  - "cli/internal/promptio/**"
---

# CLI language: two audiences, two registers

The `ai-playground` CLI emits text for two different audiences and the
register it uses must match. This rule auto-loads when editing any
file that produces user-facing strings so the distinction stays
consistent.

| Surface | Audience | Register |
|---|---|---|
| `init`, `build`, `add-worker`, `ssh-worker`, `shutdown-worker`, `list-workers`, `reset` Step labels, prompts, banners, success lines, error messages | end users | **plain language** — common nouns, project-internal terms, no internal URIs/IDs |
| `ai-playground doctor` verbose output, the doctor punch list embedded in `init`/`build`, and any text targeted at someone debugging a broken host (or a coding agent doing the same) | humans + agents debugging | **maximally precise** — exact kernel/libvirt/qemu terminology, layer labels, manual-inspection commands, no softening |

Both registers can co-exist in the same flow: the user-facing path
through `add-worker` says "Waiting for IP address (timeout 1m30s)";
when *that* fails, doctor's punch list still says "libvirtd is not
reachable on the qemu:///system socket". Don't mix the registers
within one surface.

## Plain-language register (end-user surface)

The vocabulary the daily-use commands should use:

| Use | Don't use |
|---|---|
| **worker** (or **VM** when worker is ambiguous) | libvirt domain |
| **IP address** | DHCP lease, IPv4 lease |
| **VM disk** | qcow2, qcow2 overlay, linked-clone overlay |
| **first-boot config** | NoCloud seed ISO, cidata.iso, cloud-init seed |
| **setup script** | provision script, provisioner |
| **boots Debian, runs setup scripts, cleans up** | Debian boot, provisioners, sysprep |
| **Building golden image** | Running 'packer build' |
| **Preparing Packer** | Running 'packer init' |
| **upstream changes / available updates** | drift, upstream drift |
| **stop / delete its disks** | destroy, undefine, --remove-all-storage |
| **Build SSH key + first-boot config** | ed25519 keypair + user-data |
| **(no internal URIs)** | qemu:///system, /var/run/libvirt/libvirt-sock |

**Always keep literal:**
- Tool/binary names (`Packer`, `virsh`, `qemu-img`, `xorriso`, `ssh-keygen`, `git`)
- Project-internal config keys (`vm_user`, `provision.include`, `on_conflict`)
- Project-internal flag names (`--ssh-user`, `--repo-path`, `--prefix`)
- Brand terms we've defined (`worker`, `golden image`, `provision/`, `chroot/etc/skel/`)
- Filesystem paths (`$XDG_CONFIG_HOME/ai-playground/build/`, `~/.ssh/id_ed25519.pub`)

The principle: the user shouldn't need to know the libvirt/qemu/cloud-init
mental model to read the output. They should know what *their* command
is doing in *their* vocabulary.

## Precise register (`doctor` and agent-targeted output)

The principle inverts: every term must be exact, every layer of the
stack named, every check must include a manual-inspection command.
The audience is debugging — false simplification leads to false
conclusions. This is the right place for libvirt domain, qemu:///system,
/dev/kvm, vmx/svm, /var/run/libvirt/libvirt-sock, dynamic_ownership,
ioctl(KVM_CREATE_VM), TCG software emulation, etc.

`internal/doctor.Check` carries three fields specifically for this:

- **`Layer`** — one of the seven layer constants
  (`LayerCPU`/`LayerKernel`/`LayerQEMU`/`LayerLibvirtd`/`LayerLibvirtCli`/`LayerPacker`/`LayerHostTooling`).
  Verbose output groups checks under their layer heading so a
  reader knows which slice of the stack they're looking at.
- **`Verifies`** — a 1-3 sentence technical assertion of *what the
  check actually proves*. Should be specific enough to rule out
  adjacent failures (e.g. "Opens /dev/kvm with O_RDWR. qemu calls
  ioctl(KVM_CREATE_VM) on this fd for every -enable-kvm invocation;
  failure surfaces as 'Could not access KVM kernel module'."). Don't
  paraphrase the check in vaguer English — that defeats the purpose.
- **`Inspect`** — a shell command (or short pipeline) the user can
  paste to verify the same thing manually. Read-only — no `sudo`, no
  state mutation. Examples: `ls -l /dev/kvm`, `virsh -c qemu:///system list --all`, `id -nG | tr ' ' '\n' | grep -qx libvirt`.

Adding a new check: fill in all three fields. Updating an existing
one: keep the precision when you edit `Verifies`. The
`stackPreamble` constant in `internal/doctor/doctor.go` is the
shared architectural overview that prints once at the top of
`ai-playground doctor`; keep it in sync with the layer constants
and add a paragraph for any new layer.

When the inline `init`/`build` punch list fires, end with a pointer
back to the verbose form: "Run 'ai-playground doctor' for the full
diagnostic with stack-layer context." (already wired into
`PrintProblems`).

## When in doubt

If a string is going to be read by a user during a normal workflow,
use the plain register. If it's going to be read by someone *because
something is wrong*, use the precise register. The doctor is a
diagnostic, not a UX flow — its job is to lead the reader to a
specific cause without ambiguity.

## When you change either register

- Renaming a Step label or prompt → search the `tests/*.bats` suite
  for `grep -q '<old text>'` and update the assertions in the same
  PR.
- Adding/removing a doctor check → update the `Layer*` constant set
  if needed, and update the count in any docs that mention "the X
  doctor checks".
- Anything that changes user-visible wording → run the
  `docs-reviewer` agent on this rule and the affected `docs-*.md`
  rules to confirm the docs still describe the actual output.

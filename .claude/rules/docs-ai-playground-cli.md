---
paths:
  - "cmd/ai-playground/**"
  - "internal/worker/**"
---

# `ai-playground` Go CLI — worker pool orchestration

`ai-playground` is the Go binary in `cmd/ai-playground/` that manages a
local pool of Debian *worker* VMs cloned from the golden qcow2 image
(built by `make build-from-base`). It shells out to `virsh`,
`virt-install`, `qemu-img`, and `xorriso`; libvirt is the source of
truth for state. Use this rule when changing the public command
surface or the virt-install argument list — the non-obvious traps
below were learned from the GRUB-loop debugging session and must not
be regressed.

## Public surface (intentionally narrow)

```
ai-playground add-worker [name]       # spin up a worker, print the pool
ai-playground ssh-worker [name]       # ssh into one (random running one if no name)
ai-playground shutdown-worker [name]  # tear down one (random running one if no name)
ai-playground list-workers            # print the pool
```

The `[name]`-optional commands fall back to a uniformly random
*running* worker via `Manager.Random(ctx)`. Auto-generated names look
like `worker-3f9a17`. Names that come from users are validated against
`[a-z0-9][a-z0-9-]{0,29}` (`internal/worker/name.go`).

Persistent flags (every subcommand): `--ssh-pubkey`, `--golden`,
`--pool`, `--network`, `--prefix`, `--ssh-user`. Defaults work for
the standard layout; document them in `--help`.

## Architecture in two paragraphs

Each worker is a libvirt domain (system bus, `qemu:///system`) on the
default libvirt network (`virbr0` + dnsmasq). The disk is a qcow2
linked-clone overlay backed by the golden image; the seed is a
NoCloud ISO labeled `CIDATA` containing per-VM `user-data` +
`meta-data`; the personalization (worker user, SSH key, optional 9p
mount) is delivered by cloud-init at first boot. Domain name =
`<prefix>-<name>` (default prefix `aip`), so `virsh list --all` is the
canonical way to enumerate ours.

There is intentionally no local state file. Everything is derived
live from `virsh dominfo`, `virsh domstate`, `virsh domifaddr`, and
`virsh pool-dumpxml`. Deleting the binary strands no data. Manual
fallback (`virsh destroy && virsh undefine --remove-all-storage
<domain>`) is always possible.

## Non-obvious traps in the virt-install args

These are encoded in `internal/worker/manager.go`'s `Create` `args`
slice. Every flag listed here has a comment in code; do not strip
them without re-reading this section.

### `--machine pc` (i440fx, not q35)

Packer's qemu builder defaults to **i440fx + SeaBIOS**. virt-install
defaults to **q35** (with UEFI on Debian 13). The golden image's GRUB
is the BIOS variant — on q35 the firmware loads GRUB but the kernel
never starts.

### `--video vga`

libvirt's generated qemu cmdline includes `-nodefaults`, which strips
qemu's default VGA device. **Without any video device on i440fx, the
Debian cloud kernel triple-faults during early boot** before
producing any console output. The only visible symptom is GRUB
looping ("Booting \`Debian GNU/Linux'" repeating once per second on
the serial console).

This is NOT what `--graphics none` controls. `--graphics` selects the
display *protocol* (VNC/SPICE/none); `--video` configures the *device*
the guest sees. Any video device is enough; `vga` is the cheapest.

### `--vcpus N,sockets=1,cores=N,threads=1`

virt-install's default expansion of `--vcpus N` is
`sockets=N,cores=1,threads=1` — one CPU per socket. Pin a
single-socket topology explicitly. (This was *not* the cause of the
i440fx boot loop, but it is a defensive correctness fix worth
keeping.)

## Storage pool permissions (one-time host setup)

The default libvirt pool at `/var/lib/libvirt/images/` is `root:root
755` on a fresh Manjaro install. The CLI runs `qemu-img create` and
`xorriso` as the user, so it cannot write there out of the box.
One-time fix:

```bash
sudo chgrp libvirt /var/lib/libvirt/images
sudo chmod g+rwxs  /var/lib/libvirt/images
```

The user must be in the `libvirt` group. The setgid bit makes files
created in the pool inherit the `libvirt` group, which interacts
correctly with libvirt's `dynamic_ownership` (libvirt chowns disk
files to qemu/libvirt-qemu when the VM starts).

## NoCloud seed mechanics (`internal/worker/seed.go`)

`BuildSeedISO` writes `user-data` (Go-template-rendered) and
`meta-data` to a temp dir, then runs `xorriso -as mkisofs -volid
CIDATA -joliet -rock` to pack a CIDATA-labeled ISO. The seed is
attached as an IDE CD-ROM via `--disk path=…,device=cdrom`.

The 9p mount feature (`add-worker --mount /host/path`):
- adds `--filesystem
  type=mount,source=<path>,target=hostshare,accessmode=passthrough`
- writes `mounts: [hostshare, /home/<user>/project, 9p, ...]` into
  user-data
- adds `bootcmd: [modprobe 9p, modprobe 9pnet_virtio]` so the modules
  are loaded before cloud-init's mounts module runs

## Diagnostic recipes (in rough escalation order)

1. **Pool view:** `ai-playground list-workers`. Equivalent to
   `virsh list --all` filtered to our prefix.
2. **State + IP:** `virsh domstate <domain>`, `virsh domifaddr
   <domain>`. Wrapped by `Worker.IP` and `Worker.IPWait`.
3. **DHCP leases:** `virsh net-dhcp-leases default` — if our domain's
   MAC isn't here, the guest never reached networking.
4. **Block IO:** `virsh domblkstat <domain> vda`. Zero writes after a
   minute means stuck in early boot / initramfs.
5. **Host-side tap stats:** find vnet via `virsh domiflist`, then `ip
   -s link show vnet<N>`. RX=0 + TX<200 bytes ≈ kernel never reached
   networking.
6. **Serial console (no TTY):** `sudo timeout 5 cat $(virsh
   ttyconsole <domain>)` grabs whatever's on the pty without an
   interactive terminal. The pty is owned by `libvirt-qemu`, so sudo
   is required.
7. **Full qemu cmdline:** `sudo cat /var/log/libvirt/qemu/<domain>.log`
   shows the literal qemu invocation libvirt generated. When the bug
   feels libvirt-specific, the standard move is to bypass libvirt
   entirely with `scripts/test-boot.sh` and diff the two invocations.
   See [`docs-test-boot.md`](docs-test-boot.md) for that workflow.

## Intentionally out of scope

- **Snapshots, autostart, host-path SSHFS.** Defer until requested.
- **Graceful stop without removal.** `shutdown-worker` always
  destroys the worker; the pool model assumes workers are ephemeral.
- **Remote hosts.** The CLI hard-codes `qemu:///system`. Remote URIs
  (`qemu+ssh://…`) are not supported.
- **`qemu:///session` (per-user libvirt).** Session-mode VMs use
  SLIRP by default and don't get a DHCP lease on `virbr0`, which
  defeats the per-VM-IP requirement.

## When changes here require touching the Packer template

`base-iso/packer/template.pkr.hcl` builds the golden image with
machine type `pc` (Packer qemu default) + `-cpu host`. If you ever
change the build to a different machine type or CPU model, the CLI's
`--machine` and `--cpu` flags must move in lockstep — that's the
invariant the GRUB loop teaches.

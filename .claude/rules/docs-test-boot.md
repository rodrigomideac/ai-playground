---
paths:
  - "scripts/test-boot.sh"
---

# `scripts/test-boot.sh` — libvirt-bypass single-VM smoke boot

A manual diagnostic that boots the golden qcow2 with the simplest
possible raw `qemu-system-x86_64` invocation and SLIRP NAT, *bypassing
libvirt entirely*. Use it when you need to disambiguate "is the image
broken" from "is libvirt broken" or "is the CLI broken." Earned its
keep during the migrate-to-cloud-init debugging session: it
established that the image was fine, which pointed at libvirt's
`-nodefaults` (and the missing `--video vga`) as the cause of a GRUB
boot loop.

The script's own header comment carries the full operational reference
(flags, env vars, exit shortcut, limitations); read it first. This
rule covers the *architectural* context the header doesn't.

## When to reach for it (vs. `ai-playground add-worker`)

| Symptom | Pick |
|---|---|
| New image won't boot at all | `test-boot.sh` — strips libvirt as a variable |
| Image boots, but workers never get DHCP leases | `test-boot.sh` — SLIRP NAT bypasses dnsmasq/virbr0 |
| Workers boot but provisioning didn't land (cloud-init, `/etc/skel`, etc.) | `ai-playground add-worker` — same boot path users see |
| Long-running test VM with a real DHCP IP | `ai-playground add-worker` — libvirt + DHCP |

## How it differs from the CLI

| | `test-boot.sh` | `ai-playground add-worker` |
|---|---|---|
| Hypervisor | raw `qemu-system-x86_64` | libvirt → qemu |
| Networking | SLIRP NAT, host port-forward | tap on `virbr0`, real DHCP IP |
| Persistence | none — kill qemu, delete overlay | libvirt domain XML survives reboot |
| Multi-VM | one per `SANDBOX_DIR` | unlimited |
| Host setup needed | none | libvirt running, pool perms, default network up |

## Reference qemu invocation

The qemu line in this script is *intentionally* minimal — exactly the
flags needed to boot the golden image and nothing else. That's the
point: when something boots here but not via the CLI, compare against
`/var/log/libvirt/qemu/<domain>.log` and look for what libvirt added
that this script doesn't have. The `--video vga` trap encoded in
[`docs-ai-playground-cli.md`](docs-ai-playground-cli.md) was found
exactly this way.

If you simplify the qemu invocation (drop a flag, change machine type,
swap virtio-blk for sata), you're changing the diff baseline — make
sure the change is intentional.

## Lockstep with the Packer template

`base-iso/packer/template.pkr.hcl` builds the golden image with
machine type `pc` (Packer's qemu-builder default) and `-cpu host`. If
either ever changes there, the qemu invocation in this script must
move in lockstep — otherwise `test-boot.sh` will appear to fail on a
perfectly healthy image. Same invariant noted in
`docs-ai-playground-cli.md`'s final section.

---
paths:
  - "base-iso/packer/template.pkr.hcl"
---

# Libvirt-bypass smoke boot for the golden image

Auto-loaded when working on the Packer template because the question
that comes up most often after a template change is "is the image
broken, or is libvirt breaking it?" The answer is to boot the same
qcow2 under raw qemu (no libvirt) and compare.

The recipe below is the canonical libvirt-bypass invocation —
deliberately the *minimum* set of qemu flags that boots the golden
image, so it doubles as a diff baseline against
`/var/log/libvirt/qemu/<domain>.log` when the CLI's workers
misbehave. The `--video vga` trap noted in
[`docs-ai-playground-cli.md`](docs-ai-playground-cli.md) was found
exactly this way.

## Decision matrix

| Symptom | Pick |
|---|---|
| New image won't boot at all | raw qemu (recipe below) — strips libvirt as a variable |
| Image boots, but `add-worker` workers never get DHCP leases | raw qemu — SLIRP NAT bypasses dnsmasq/virbr0 |
| Workers boot but provisioning didn't land (cloud-init, `/etc/skel`, etc.) | `ai-playground add-worker` — same boot path users see |

## Recipe

Run from the repo root. Generates a throwaway keypair, renders a
NoCloud seed, overlay-clones the golden image, and boots qemu in the
foreground. Use **Ctrl-A then X** to power off cleanly.

```bash
GOLDEN="$(realpath build/packer-ai-playground-base/ai-playground-base)"
SBX=/tmp/golden-smoke
mkdir -p "$SBX"

# Throwaway keypair (idempotent)
[ -f "$SBX/id_ed25519" ] || ssh-keygen -t ed25519 -N '' -f "$SBX/id_ed25519"

# NoCloud seed: creates user `vm` with our key
cat > "$SBX/user-data" <<EOF
#cloud-config
hostname: smoke
users:
  - name: vm
    sudo: ALL=(ALL) NOPASSWD:ALL
    lock_passwd: true
    ssh_authorized_keys:
      - $(cat "$SBX/id_ed25519.pub")
ssh_pwauth: false
EOF
cat > "$SBX/meta-data" <<EOF
instance-id: smoke-001
local-hostname: smoke
EOF
xorriso -as mkisofs -quiet -volid CIDATA -joliet -rock \
  -output "$SBX/cidata.iso" "$SBX/user-data" "$SBX/meta-data"

# Linked-clone overlay (golden stays read-only)
qemu-img create -f qcow2 -F qcow2 -b "$GOLDEN" "$SBX/disk.qcow2" >/dev/null

# Boot — minimum flags that work
qemu-system-x86_64 -enable-kvm -cpu host -m 4G -smp 2 \
  -drive if=virtio,file="$SBX/disk.qcow2" \
  -drive media=cdrom,file="$SBX/cidata.iso" \
  -nic user,model=virtio,hostfwd=tcp::2222-:22 \
  -nographic
```

In another terminal, once cloud-init has finished (about 30s):

```bash
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
    -i /tmp/golden-smoke/id_ed25519 -p 2222 vm@127.0.0.1
```

Reset between attempts:

```bash
rm -rf /tmp/golden-smoke   # full reset (recreates everything)
# or just delete the overlay to keep the seed:
rm /tmp/golden-smoke/disk.qcow2
```

## How this differs from `ai-playground add-worker`

| | raw qemu (recipe) | `ai-playground add-worker` |
|---|---|---|
| Hypervisor | `qemu-system-x86_64` directly | libvirt → qemu |
| Networking | SLIRP NAT, host port-forward | tap on `virbr0`, real DHCP IP |
| Persistence | none — kill qemu, delete sandbox dir | libvirt domain XML survives reboot |
| Multi-VM | one per `SBX` + port | unlimited |
| Host setup needed | none | libvirt running, pool perms, default network up |

## Lockstep with the Packer template

This template builds the golden image with machine type `pc` (Packer
qemu-builder default) and `-cpu host`. If either ever changes here,
the recipe's qemu line must move with it — otherwise the recipe will
appear to fail on a perfectly healthy image. Same invariant noted in
[`docs-ai-playground-cli.md`](docs-ai-playground-cli.md).

## Limitations

- **No DHCP, no routable IP.** SSH only via the host port-forward.
  Don't use this to test inter-VM networking — that's what the
  libvirt-driven CLI is for.
- **One VM at a time per `SBX`.** Use a different `SBX` and host
  port to run several in parallel.
- **Manual cleanup.** `rm -rf /tmp/golden-smoke` after the qemu
  process exits to reclaim disk.

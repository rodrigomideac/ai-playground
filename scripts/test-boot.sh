#!/bin/bash
set -euo pipefail

# Boots a single sandbox VM from the golden qcow2 produced by `make
# build-from-base`, for manual smoke-testing. Uses a qcow2 overlay so the
# golden image stays immutable, and seeds a NoCloud user-data ISO that
# creates user `rodrigo` with the host's SSH public key.
#
# This is intentionally minimal — no DHCP, no per-VM IP, no fleet
# management. SLIRP NAT with port-forwarding is enough to ssh in and
# verify provisioning. Use libvirt for anything beyond one-off testing.
#
# Usage:
#   scripts/test-boot.sh           # boot (creates overlay+seed if missing)
#   scripts/test-boot.sh reset     # delete overlay+seed, then boot fresh
#
# Environment overrides:
#   GOLDEN_IMAGE   path to the qcow2 produced by Packer
#                  default: build/packer-ai-playground-base/ai-playground-base
#   SANDBOX_DIR    where overlay+seed live
#                  default: $HOME/ai-playground-test
#   SSH_PUBKEY     path to a public key to authorize for user `rodrigo`
#                  default: first of ~/.ssh/id_ed25519.pub, ~/.ssh/id_rsa.pub
#   SSH_PORT       host port forwarded to guest:22
#                  default: 2222
#   VM_MEMORY      MiB
#                  default: 4096
#   VM_CPUS        vCPUs
#                  default: 2
#
# After boot:
#   ssh -p $SSH_PORT rodrigo@127.0.0.1   (in another terminal; ~30s cold-start)
#
# To exit qemu cleanly while it's in the foreground console: Ctrl-A then X.
#
# Limitations:
#   - SLIRP NAT only — no inter-VM connectivity or routable per-VM IPs.
#   - Single VM at a time per SANDBOX_DIR. Use a different SANDBOX_DIR
#     and SSH_PORT to run multiple in parallel.
#   - The overlay's backing file is the absolute path to GOLDEN_IMAGE; if
#     you move the golden image you must rebuild the overlay.

REPO_ROOT="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"

GOLDEN_IMAGE="${GOLDEN_IMAGE:-$REPO_ROOT/build/packer-ai-playground-base/ai-playground-base}"
SANDBOX_DIR="${SANDBOX_DIR:-$HOME/ai-playground-test}"
SSH_PORT="${SSH_PORT:-2222}"
VM_MEMORY="${VM_MEMORY:-4096}"
VM_CPUS="${VM_CPUS:-2}"

# Pick a default pubkey if not provided
if [ -z "${SSH_PUBKEY:-}" ]; then
    for candidate in "$HOME/.ssh/id_ed25519.pub" "$HOME/.ssh/id_rsa.pub"; do
        if [ -f "$candidate" ]; then
            SSH_PUBKEY="$candidate"
            break
        fi
    done
fi

if [ -z "${SSH_PUBKEY:-}" ] || [ ! -f "$SSH_PUBKEY" ]; then
    echo "error: no SSH public key found." >&2
    echo "  Generate one with: ssh-keygen -t ed25519 -N ''" >&2
    echo "  Or set SSH_PUBKEY=/path/to/key.pub" >&2
    exit 1
fi

if [ ! -f "$GOLDEN_IMAGE" ]; then
    echo "error: golden image not found at $GOLDEN_IMAGE" >&2
    echo "  Run 'make build-from-base' first, or override GOLDEN_IMAGE." >&2
    exit 1
fi

# Optional reset: wipe per-VM state but keep the golden image
if [ "${1:-}" = "reset" ]; then
    echo "Resetting $SANDBOX_DIR..."
    rm -rf "$SANDBOX_DIR"
fi

mkdir -p "$SANDBOX_DIR"
cd "$SANDBOX_DIR"

# Linked-clone overlay
if [ ! -f test-disk.qcow2 ]; then
    echo "Creating overlay test-disk.qcow2 backed by $GOLDEN_IMAGE..."
    qemu-img create -f qcow2 -F qcow2 -b "$GOLDEN_IMAGE" test-disk.qcow2 >/dev/null
fi

# NoCloud seed: regenerated each run so SSH key changes are picked up
PUBKEY_CONTENT="$(cat "$SSH_PUBKEY")"
cat > user-data <<EOF
#cloud-config
hostname: ai-playground-test
users:
  - name: rodrigo
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    lock_passwd: true
    ssh_authorized_keys:
      - $PUBKEY_CONTENT
ssh_pwauth: false
EOF

cat > meta-data <<EOF
instance-id: ai-playground-test-001
local-hostname: ai-playground-test
EOF

xorriso -as mkisofs -quiet -volid CIDATA -joliet -rock \
    -output cidata.iso user-data meta-data

# Boot
echo
echo "Booting VM. SSH in with:"
echo "  ssh -o StrictHostKeyChecking=no -p $SSH_PORT rodrigo@127.0.0.1"
echo "Cold boot takes ~30s while cloud-init runs. Exit qemu with: Ctrl-A then X"
echo

exec qemu-system-x86_64 \
    -enable-kvm -cpu host \
    -m "$VM_MEMORY" -smp "$VM_CPUS" \
    -drive if=virtio,file=test-disk.qcow2 \
    -drive media=cdrom,file=cidata.iso \
    -nic user,model=virtio,hostfwd=tcp::"$SSH_PORT"-:22 \
    -nographic

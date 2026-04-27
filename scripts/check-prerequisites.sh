#!/bin/bash
set -euo pipefail

# Verifies that the tools and system state needed for the qemu-based
# Packer build are present. Exits non-zero with a summary if anything is
# missing.

errors=0

check_command() {
  local cmd="$1"
  local install_hint="$2"

  if ! command -v "$cmd" &>/dev/null; then
    echo "error: '$cmd' is not installed." >&2
    echo "  Install hint: $install_hint" >&2
    errors=$((errors + 1))
  fi
}

echo "Checking prerequisites..."
echo

# --- Required commands ---

check_command curl \
  "Fedora: dnf install curl | Ubuntu: apt install curl | Manjaro: pacman -S curl"

check_command git \
  "Fedora: dnf install git | Ubuntu: apt install git | Manjaro: pacman -S git"

check_command qemu-system-x86_64 \
  "Fedora: dnf install @virtualization | Ubuntu: apt install qemu-system-x86 | Manjaro: pacman -S qemu-desktop"

check_command qemu-img \
  "Shipped with qemu; see qemu-system-x86_64 hint above."

check_command xorriso \
  "Fedora: dnf install xorriso | Ubuntu: apt install xorriso | Manjaro: pacman -S libisoburn"

check_command ssh-keygen \
  "Fedora/Ubuntu: apt/dnf install openssh-clients | Manjaro: pacman -S openssh"

# --- Packer: installed and not the cracklib imposter ---

check_command packer \
  "See https://developer.hashicorp.com/packer/install"

if command -v packer &>/dev/null; then
  packer_version_output=$(packer version 2>&1 || true)
  if ! echo "$packer_version_output" | grep -q "^Packer v"; then
    echo "error: 'packer' binary found but it does not appear to be HashiCorp Packer." >&2
    echo "  The installed binary may be the cracklib 'packer' utility." >&2
    echo "  On Fedora/CentOS: dnf remove cracklib-packer  (then install HashiCorp Packer)" >&2
    echo "  Detected output: $packer_version_output" >&2
    errors=$((errors + 1))
  fi
fi

# --- KVM access ---

if [ ! -e /dev/kvm ]; then
  echo "error: /dev/kvm does not exist. Load the kvm_intel or kvm_amd module," >&2
  echo "  or enable VT-x / AMD-V in BIOS/UEFI firmware." >&2
  errors=$((errors + 1))
elif [ ! -r /dev/kvm ] || [ ! -w /dev/kvm ]; then
  echo "error: /dev/kvm exists but the current user lacks read+write access." >&2
  echo "  Add yourself to the 'kvm' group and re-login: sudo usermod -aG kvm \$USER" >&2
  errors=$((errors + 1))
fi

if ! grep -Eq '(vmx|svm)' /proc/cpuinfo; then
  echo "error: CPU does not expose virtualization extensions (vmx/svm)." >&2
  echo "  Enable VT-x / AMD-V in BIOS/UEFI firmware." >&2
  errors=$((errors + 1))
fi

# --- Summary ---

echo
if [ "$errors" -gt 0 ]; then
  echo "FAILED: $errors prerequisite(s) missing. Fix the issues above before building." >&2
  exit 1
fi

echo "All prerequisites satisfied."

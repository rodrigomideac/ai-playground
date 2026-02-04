#!/bin/bash
set -euo pipefail

# Verifies that all required tools and system state are present before
# running the build. Exits with a non-zero status and a summary of
# missing prerequisites if any check fails.

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
  "Fedora/CentOS: dnf install curl | Ubuntu: apt install curl | Manjaro: pacman -S curl"

check_command git \
  "Fedora/CentOS: dnf install git | Ubuntu: apt install git | Manjaro: pacman -S git"

check_command vagrant \
  "See https://developer.hashicorp.com/vagrant/install"

check_command VBoxManage \
  "Fedora: dnf install VirtualBox | Ubuntu: apt install virtualbox | CentOS: see https://www.virtualbox.org/wiki/Linux_Downloads | Manjaro: pacman -S virtualbox"

# --- Packer: installed and not the cracklib imposter ---

check_command packer \
  "See https://developer.hashicorp.com/packer/install"

if command -v packer &>/dev/null; then

  # The cracklib 'packer' binary does not support the 'version' subcommand.
  # HashiCorp Packer prints its version string starting with "Packer v".
  packer_version_output=$(packer version 2>&1 || true)
  if ! echo "$packer_version_output" | grep -q "^Packer v"; then
    echo "error: 'packer' binary found but it does not appear to be HashiCorp Packer." >&2
    echo "  The installed binary may be the cracklib 'packer' utility." >&2
    echo "  On Fedora/CentOS: dnf remove cracklib-packer  (then install HashiCorp Packer)" >&2
    echo "  Detected output: $packer_version_output" >&2
    errors=$((errors + 1))
  fi
fi

# --- VirtualBox kernel modules ---

if command -v VBoxManage &>/dev/null; then
  if ! VBoxManage list hostinfo &>/dev/null; then
    echo "error: VirtualBox kernel modules are not loaded (vboxdrv)." >&2
    echo "  This commonly happens after a kernel update without reboot," >&2
    echo "  or when Secure Boot blocks unsigned modules." >&2
    echo "  Try: reboot, or load modules manually, or enroll MOK for Secure Boot." >&2
    errors=$((errors + 1))
  fi
fi

# --- Summary ---

echo
if [ "$errors" -gt 0 ]; then
  echo "FAILED: $errors prerequisite(s) missing. Fix the issues above before building." >&2
  exit 1
fi

echo "All prerequisites satisfied."

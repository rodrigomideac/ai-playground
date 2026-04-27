#!/bin/bash
set -euo pipefail

echo "==> Installing base packages"
sudo apt-get update
sudo apt-get install -y \
  cloud-init \
  curl \
  git \
  neovim \
  qemu-guest-agent \
  rsync \
  zsh

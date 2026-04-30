#!/bin/bash
set -euo pipefail

# Description: apt install of cloud-init, qemu-guest-agent, curl, git, neovim, rsync, zsh

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

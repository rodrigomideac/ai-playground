#!/bin/bash
set -euo pipefail

echo "==> Installing base packages"
sudo apt-get update
sudo apt-get install -y \
  curl \
  git \
  neovim \
  rsync \
  zsh

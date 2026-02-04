#!/bin/bash

sudo apt-get update
sudo apt install -y \
  curl \
  git \
  neovim \
  rsync \
  zsh

sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)"
curl -fsSL https://claude.ai/install.sh | bash
# shellcheck disable=SC2016,SC1090
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc && source ~/.bashrc
sed -i 's/#force_color_prompt=yes/force_color_prompt=yes/' ~/.bashrc
echo 'export COLORTERM=truecolor' >> ~/.bashrc
echo 'export EDITOR=nvim' >> ~/.bashrc

# Docker install
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh ./get-docker.sh
sudo modprobe tun
sudo apt-get install -y uidmap
dockerd-rootless-setuptool.sh install
echo 'export PATH=/usr/bin:$PATH' >> ~/.bashrc
echo 'export DOCKER_HOST=unix:///run/user/1000/docker.sock' >> ~/.bashrc


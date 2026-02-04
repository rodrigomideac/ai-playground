#!/bin/bash

sudo apt-get update
sudo apt install -y \
  curl \
  git \
  neovim \
  zsh

sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)"
curl -fsSL https://claude.ai/install.sh | bash
# shellcheck disable=SC2016,SC1090
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc && source ~/.bashrc
sed -i 's/#force_color_prompt=yes/force_color_prompt=yes/' ~/.bashrc
echo 'export COLORTERM=truecolor' >> ~/.bashrc
echo 'export EDITOR=nvim' >> ~/.bashrc

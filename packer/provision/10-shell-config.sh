#!/bin/bash
set -euo pipefail

# Description: oh-my-zsh + bashrc tweaks (color prompt, COLORTERM=truecolor, EDITOR=nvim)

echo "==> Installing oh-my-zsh"
RUNZSH=no CHSH=no sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)"

echo "==> Configuring shell"
sed -i 's/#force_color_prompt=yes/force_color_prompt=yes/' ~/.bashrc
# shellcheck disable=SC2016
echo 'export COLORTERM=truecolor' >> ~/.bashrc
echo 'export EDITOR=nvim' >> ~/.bashrc

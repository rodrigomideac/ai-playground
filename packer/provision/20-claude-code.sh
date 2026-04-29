#!/bin/bash
set -euo pipefail

# Description: Claude Code CLI (anthropic) installed under ~/.local/bin

echo "==> Installing Claude Code CLI"
curl -fsSL https://claude.ai/install.sh | bash

echo "==> Adding ~/.local/bin to PATH"
# shellcheck disable=SC2016
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc

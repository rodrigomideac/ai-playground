#!/bin/bash
set -euo pipefail

REPO_ROOT="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"

if ! command -v shellcheck &>/dev/null; then
    echo "error: shellcheck is not installed" >&2
    echo "Install it with: pacman -S shellcheck" >&2
    exit 1
fi

mapfile -t sh_files < <(find "$REPO_ROOT" -name '*.sh' -not -path '*/vendor/*' -not -path '*/.git/*')

if [ ${#sh_files[@]} -eq 0 ]; then
    echo "No .sh files found"
    exit 0
fi

echo "Running shellcheck on ${#sh_files[@]} file(s)..."
printf '  %s\n' "${sh_files[@]}"
echo

shellcheck "${sh_files[@]}"
echo "All files passed."

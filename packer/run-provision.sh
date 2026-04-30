#!/bin/bash
set -euo pipefail

# Provision runner: discovers and executes scripts from a single directory in
# numeric-prefix order. Bails on the first non-zero exit.

PROVISION_DIR="${1:?Usage: run-provision.sh <provision-dir>}"

if [[ ! -d "$PROVISION_DIR" ]]; then
    echo "[provision] ERROR: Directory not found: $PROVISION_DIR" >&2
    exit 1
fi

# extract_prefix returns the leading digits from a filename
extract_prefix() {
    local filename
    filename="$(basename "$1")"
    echo "$filename" | grep -oE '^[0-9]+' || true
}

declare -a entries

for script in "$PROVISION_DIR"/*.sh; do
    [[ -f "$script" ]] || continue
    prefix="$(extract_prefix "$script")"
    if [[ -z "$prefix" ]]; then
        echo "[provision] WARNING: Skipping script without numeric prefix: $script"
        continue
    fi
    entries+=("$prefix|$script")
done

if [[ ${#entries[@]} -eq 0 ]]; then
    echo "[provision] No provision scripts found in $PROVISION_DIR. Nothing to do."
    exit 0
fi

mapfile -t sorted < <(printf '%s\n' "${entries[@]}" | sort -n -t '|' -k1)

echo "============================================"
echo " Provision Runner"
echo "============================================"
echo
echo " Execution Plan (${#sorted[@]} scripts)"
for entry in "${sorted[@]}"; do
    prefix="${entry%%|*}"
    script="${entry#*|}"
    echo "  [$prefix] $script"
done
echo

for entry in "${sorted[@]}"; do
    prefix="${entry%%|*}"
    script="${entry#*|}"
    echo "============================================"
    echo " Running [$prefix]: $script"
    echo "============================================"
    bash "$script"
    echo "[provision] Completed: $script"
    echo
done

echo "============================================"
echo " All provision scripts completed successfully"
echo "============================================"

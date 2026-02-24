#!/bin/bash
set -euo pipefail

# Provision runner: discovers and executes scripts from default-provision/
# and custom-provision/ directories. Custom scripts override defaults when
# they share the same numeric prefix.

BASE_DIR="${1:?Usage: run-provision.sh <base-dir>}"
DEFAULT_DIR="$BASE_DIR/default-provision"
CUSTOM_DIR="$BASE_DIR/custom-provision"

declare -A scripts
declare -A sources

# extract_prefix returns the leading digits from a filename
extract_prefix() {
    local filename
    filename="$(basename "$1")"
    echo "$filename" | grep -oE '^[0-9]+' || true
}

# Register scripts from a directory, optionally overriding previous entries
register_scripts() {
    local dir="$1"
    local source_label="$2"

    if [[ ! -d "$dir" ]]; then
        echo "[provision] Directory not found, skipping: $dir"
        return
    fi

    local found=0
    for script in "$dir"/*.sh; do
        [[ -f "$script" ]] || continue
        found=1

        local prefix
        prefix="$(extract_prefix "$script")"
        if [[ -z "$prefix" ]]; then
            echo "[provision] WARNING: Skipping script without numeric prefix: $script"
            continue
        fi

        if [[ -n "${scripts[$prefix]:-}" ]]; then
            echo "[provision] OVERRIDE: $prefix — replacing ${sources[$prefix]} (${scripts[$prefix]}) with $source_label ($script)"
        else
            echo "[provision] Registered: $prefix — $source_label ($script)"
        fi

        scripts[$prefix]="$script"
        sources[$prefix]="$source_label"
    done

    if [[ $found -eq 0 ]]; then
        echo "[provision] No .sh scripts found in: $dir"
    fi
}

echo "============================================"
echo " Provision Runner"
echo "============================================"
echo ""

# Register defaults first, then customs (customs override at same prefix)
register_scripts "$DEFAULT_DIR" "default"
register_scripts "$CUSTOM_DIR" "custom"

# Sort prefixes numerically and execute
mapfile -t sorted_prefixes < <(echo "${!scripts[@]}" | tr ' ' '\n' | sort -n)

if [[ ${#sorted_prefixes[@]} -eq 0 ]]; then
    echo "[provision] No provision scripts found. Nothing to do."
    exit 0
fi

echo ""
echo "============================================"
echo " Execution Plan (${#sorted_prefixes[@]} scripts)"
echo "============================================"
for prefix in "${sorted_prefixes[@]}"; do
    echo "  [$prefix] ${scripts[$prefix]} (${sources[$prefix]})"
done
echo ""

for prefix in "${sorted_prefixes[@]}"; do
    script="${scripts[$prefix]}"
    echo "============================================"
    echo " Running [$prefix]: $script"
    echo "============================================"
    bash "$script"
    echo "[provision] Completed: $script"
    echo ""
done

echo "============================================"
echo " All provision scripts completed successfully"
echo "============================================"

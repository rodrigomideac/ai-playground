#!/bin/bash
set -euo pipefail

# Prepares the NoCloud seed used by the Packer qemu build.
#
# Generates a throwaway ed25519 keypair (reused if it already exists) and
# renders packer/seed/user-data from user-data.tpl by substituting
# __SSH_PUBKEY__ with the public key. The private key is used only during
# the build to SSH into the cloud image; it's never baked into the output.
#
# Outputs (all under packer/seed/, all gitignored):
#   - id_ed25519       private key used as ssh_private_key_file
#   - id_ed25519.pub   public key injected via cloud-init
#   - user-data        rendered NoCloud user-data
#
# Usage: scripts/prepare-packer-seed.sh
# Called by: make build-from-base

REPO_ROOT="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
SEED_DIR="$REPO_ROOT/packer/seed"
KEY_FILE="$SEED_DIR/id_ed25519"
TEMPLATE="$SEED_DIR/user-data.tpl"
OUTPUT="$SEED_DIR/user-data"

if [ ! -f "$TEMPLATE" ]; then
    echo "error: missing template $TEMPLATE" >&2
    exit 1
fi

if [ ! -f "$KEY_FILE" ]; then
    echo "Generating build-only SSH keypair at $KEY_FILE..."
    ssh-keygen -t ed25519 -N "" -C "packer-build@ai-playground" -f "$KEY_FILE" >/dev/null
    chmod 600 "$KEY_FILE"
else
    echo "Reusing existing keypair at $KEY_FILE"
fi

PUBKEY_CONTENT="$(cat "$KEY_FILE.pub")"

echo "Rendering $OUTPUT from $TEMPLATE..."
awk -v key="$PUBKEY_CONTENT" '{gsub(/__SSH_PUBKEY__/, key); print}' "$TEMPLATE" > "$OUTPUT"

echo "Seed ready."

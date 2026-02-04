#!/bin/bash
set -euo pipefail

# Downloads the Debian 13 netinst ISO and verifies its SHA256 checksum against
# the official SHA256SUMS file. Also verifies the packer template references the
# correct checksum.

REPO_ROOT="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"

ISO_URL="https://cdimage.debian.org/debian-cd/current/amd64/iso-cd/debian-13.3.0-amd64-netinst.iso"
SHA256SUMS_URL="https://cdimage.debian.org/debian-cd/current/amd64/iso-cd/SHA256SUMS"
ISO_FILENAME="debian-13.3.0-amd64-netinst.iso"
ISO_DEST="$REPO_ROOT/base-iso/$ISO_FILENAME"
PACKER_TEMPLATE="$REPO_ROOT/base-iso/packer/template.pkr.hcl"

if ! command -v curl &>/dev/null; then
    echo "error: curl is not installed" >&2
    exit 1
fi

# Download SHA256SUMS and extract checksum for our ISO
echo "Fetching SHA256SUMS from $SHA256SUMS_URL..."
sha256sums=$(curl --fail --silent --show-error "$SHA256SUMS_URL")

expected_checksum=$(echo "$sha256sums" | grep "$ISO_FILENAME" | awk '{print $1}')
if [ -z "$expected_checksum" ]; then
    echo "error: could not find checksum for $ISO_FILENAME in SHA256SUMS" >&2
    echo "SHA256SUMS contents:" >&2
    echo "$sha256sums" >&2
    exit 1
fi
echo "Expected SHA256: $expected_checksum"

# Download the ISO (resume partial downloads with -C -)
if [ -f "$ISO_DEST" ]; then
    echo "ISO already exists at $ISO_DEST, verifying existing file..."
else
    echo "Downloading ISO from $ISO_URL..."
    curl --fail --show-error --location --progress-bar -C - -o "$ISO_DEST" "$ISO_URL"
    echo "Download complete."
fi

# Verify the ISO checksum
echo "Computing SHA256 of $ISO_DEST..."
actual_checksum=$(sha256sum "$ISO_DEST" | awk '{print $1}')
echo "Actual SHA256:   $actual_checksum"

if [ "$actual_checksum" != "$expected_checksum" ]; then
    echo "error: checksum mismatch" >&2
    echo "  expected: $expected_checksum" >&2
    echo "  actual:   $actual_checksum" >&2
    echo "The downloaded file may be corrupted. Delete it and re-run this script." >&2
    exit 1
fi
echo "ISO checksum verified."

# Verify the packer template has the correct checksum
echo "Checking packer template at $PACKER_TEMPLATE..."
packer_checksum=$(grep 'iso_checksum' "$PACKER_TEMPLATE" | sed -E 's/.*"([a-f0-9]{64})".*/\1/')

if [ -z "$packer_checksum" ]; then
    echo "error: could not extract iso_checksum from packer template" >&2
    exit 1
fi

if [ "$packer_checksum" != "$expected_checksum" ]; then
    echo "error: packer template checksum does not match the official checksum" >&2
    echo "  packer template: $packer_checksum" >&2
    echo "  official:        $expected_checksum" >&2
    echo "Update iso_checksum in $PACKER_TEMPLATE to: $expected_checksum" >&2
    exit 1
fi

echo "Packer template checksum matches."
echo "All checks passed."

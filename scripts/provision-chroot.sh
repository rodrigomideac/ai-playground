#!/bin/bash
# provision-chroot.sh
# Copies chroot overlay contents into the VM filesystem preserving paths.
# Everything in the overlay lands as root-owned; that's correct because the
# overlay should target system-wide locations (/etc/skel for per-user files,
# /etc, /usr/local, etc.). Files in /etc/skel are copied by `useradd -m` into
# new users' homes with the right ownership at user-creation time, which is
# how cloud-init creates the sandbox user on first boot.

set -euo pipefail

SOURCE_DIR="${1:-}"

if [ -z "$SOURCE_DIR" ]; then
  echo "[provision-chroot] ERROR: No source directory argument provided."
  echo "Usage: $0 <source-directory>"
  exit 1
fi

if [ ! -d "$SOURCE_DIR" ]; then
  echo "[provision-chroot] WARNING: Source directory '$SOURCE_DIR' does not exist. Nothing to do."
  exit 0
fi

if [ -z "$(ls -A "$SOURCE_DIR" 2>/dev/null)" ]; then
  echo "[provision-chroot] WARNING: Source directory '$SOURCE_DIR' is empty. Nothing to do."
  exit 0
fi

echo "[provision-chroot] Syncing overlay from '$SOURCE_DIR' to '/' ..."
sudo rsync --archive --verbose --chown=root:root "$SOURCE_DIR/" /
echo "[provision-chroot] Done."

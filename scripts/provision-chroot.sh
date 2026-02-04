#!/bin/bash
# provision-chroot.sh
# Copies chroot overlay contents into the VM filesystem preserving paths,
# then fixes ownership for any files landing under /home/*/.

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

# Check if source directory is empty
if [ -z "$(ls -A "$SOURCE_DIR" 2>/dev/null)" ]; then
  echo "[provision-chroot] WARNING: Source directory '$SOURCE_DIR' is empty. Nothing to do."
  exit 0
fi

echo "[provision-chroot] Syncing overlay from '$SOURCE_DIR' to '/' ..."
sudo rsync --archive --verbose "$SOURCE_DIR/" /
echo "[provision-chroot] Sync complete."

# Fix ownership for home directories
if [ -d "$SOURCE_DIR/home" ]; then
  for user_dir in "$SOURCE_DIR"/home/*/; do
    username="$(basename "$user_dir")"

    if ! id "$username" &>/dev/null; then
      echo "[provision-chroot] WARNING: User '$username' does not exist on this system. Skipping ownership fix for /home/$username."
      continue
    fi

    echo "[provision-chroot] Fixing ownership for /home/$username ..."
    sudo chown -R "$username:$username" "/home/$username"
    echo "[provision-chroot] Ownership fixed for /home/$username."
  done
fi

echo "[provision-chroot] Done."

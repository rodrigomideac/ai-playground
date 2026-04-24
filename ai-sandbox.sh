#!/bin/bash
# ai-sandbox.sh — Spin up disposable Vagrant VMs for coding agents.
#
# Uses a "warm" template VM + VBoxManage linked clones so each sandbox
# starts in seconds. Multiple sandboxes can run in parallel.
#
# Usage:
#   ai-sandbox.sh warmup              Build the warm template VM (run once)
#   ai-sandbox.sh run [REPO_PATH]     Fork a sandbox for the given repo (default: cwd)
#   ai-sandbox.sh ssh [REPO_PATH]     SSH into a running sandbox
#   ai-sandbox.sh stop [REPO_PATH]    Halt the sandbox VM
#   ai-sandbox.sh destroy [REPO_PATH] Destroy the sandbox VM
#   ai-sandbox.sh list                List all sandbox sessions
#   ai-sandbox.sh sync-back [REPO_PATH] Rsync changes from VM back to host
#
# Options (via env vars):
#   AI_SANDBOX_MEMORY   VM memory in MB (default: 4096)
#   AI_SANDBOX_CPUS     VM CPU count (default: 4)
#
# Examples:
#   ai-sandbox.sh warmup               # one-time setup (~60s)
#   ai-sandbox.sh run ~/dev/ai          # fork a sandbox (~30s)
#   ai-sandbox.sh run ~/dev/other       # another one, in parallel
#   ai-sandbox.sh ssh ~/dev/ai          # shell into it
#   ai-sandbox.sh sync-back ~/dev/ai    # pull changes back
#   ai-sandbox.sh destroy ~/dev/ai      # tear it down
#   ai-sandbox.sh list                  # see all sandboxes

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WARM_DIR="${HOME}/.ai-sandbox/warm"
SESSIONS_DIR="${HOME}/.ai-sandbox/sessions"
TEMPLATE="${SCRIPT_DIR}/Vagrantfile.template"
WARM_VM_NAME="ai-sandbox-warm"
SNAPSHOT_NAME="clean"

SSH_OPTS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o IdentitiesOnly=yes -o LogLevel=ERROR"

RSYNC_EXCLUDES=(
  --exclude ".git/"
  --exclude "node_modules/"
  --exclude ".vagrant/"
  --exclude "__pycache__/"
  --exclude ".venv/"
  --exclude ".claude/worktrees/"
)

die() { echo "ERROR: $*" >&2; exit 1; }

resolve_repo() {
  local repo="${1:-$(pwd)}"
  repo="$(cd "$repo" && pwd)" || die "Directory does not exist: $1"
  echo "$repo"
}

session_name_for() {
  basename "$1"
}

session_dir_for() {
  echo "${SESSIONS_DIR}/$(session_name_for "$1")"
}

find_free_port() {
  local port=2222
  while ss -tln | grep -q ":${port} " 2>/dev/null; do
    port=$((port + 1))
  done
  echo "$port"
}

wait_for_ssh() {
  local port="$1"
  local key="$2"
  local max=60
  local i=0
  echo "[ai-sandbox] Waiting for SSH on port ${port}..."
  while ! ssh $SSH_OPTS -o ConnectTimeout=2 -o BatchMode=yes \
    -p "$port" -i "$key" rodrigo@127.0.0.1 true 2>/dev/null; do
    i=$((i + 1))
    if [ "$i" -ge "$max" ]; then
      die "SSH not available after ${max} attempts"
    fi
    sleep 1
  done
}

rsync_to_vm() {
  local repo="$1"
  local port="$2"
  local key="$3"
  echo "[ai-sandbox] Syncing ${repo} -> VM:/home/rodrigo/project ..."
  # Ensure target directory exists
  ssh $SSH_OPTS -p "$port" -i "$key" rodrigo@127.0.0.1 "mkdir -p /home/rodrigo/project"
  rsync --archive --delete -z --safe-links --no-owner --no-group \
    "${RSYNC_EXCLUDES[@]}" \
    -e "ssh $SSH_OPTS -p ${port} -i ${key}" \
    "${repo}/" "rodrigo@127.0.0.1:/home/rodrigo/project"
}

rsync_from_vm() {
  local repo="$1"
  local port="$2"
  local key="$3"
  echo "[ai-sandbox] Syncing VM:/home/rodrigo/project -> ${repo} ..."
  rsync --archive -z --safe-links --no-owner --no-group \
    "${RSYNC_EXCLUDES[@]}" \
    -e "ssh $SSH_OPTS -p ${port} -i ${key}" \
    "rodrigo@127.0.0.1:/home/rodrigo/project/" "${repo}/"
}

vm_state() {
  local vm_name="$1"
  VBoxManage showvminfo "$vm_name" --machinereadable 2>/dev/null \
    | grep '^VMState=' | cut -d'"' -f2 || echo "not_found"
}

# --- Commands ---

cmd_warmup() {
  if VBoxManage showvminfo "$WARM_VM_NAME" &>/dev/null; then
    echo "[ai-sandbox] Warm VM '${WARM_VM_NAME}' already exists."
    echo "[ai-sandbox] To rebuild, destroy it first: VBoxManage unregistervm ${WARM_VM_NAME} --delete"
    return
  fi

  mkdir -p "$WARM_DIR"
  cp "$TEMPLATE" "${WARM_DIR}/Vagrantfile"

  echo "[ai-sandbox] Creating warm template VM..."
  cd "$WARM_DIR"
  vagrant up

  echo "[ai-sandbox] Taking snapshot '${SNAPSHOT_NAME}'..."
  vagrant snapshot save "$SNAPSHOT_NAME"

  echo "[ai-sandbox] Halting warm VM..."
  vagrant halt

  echo "[ai-sandbox] Warmup complete. SSH key at: ${WARM_DIR}/.vagrant/machines/default/virtualbox/private_key"
}

cmd_run() {
  local repo
  repo="$(resolve_repo "${1:-}")"
  local name
  name="$(session_name_for "$repo")"
  local sdir
  sdir="$(session_dir_for "$repo")"
  local vm_name="sandbox-${name}"

  # Check warm VM exists
  VBoxManage showvminfo "$WARM_VM_NAME" &>/dev/null \
    || die "No warm VM found. Run 'ai-sandbox.sh warmup' first."

  local ssh_key="${WARM_DIR}/.vagrant/machines/default/virtualbox/private_key"
  [ -f "$ssh_key" ] || die "SSH key not found at ${ssh_key}. Re-run warmup."

  # If session already exists, re-use it
  if [ -f "${sdir}/vm_name" ]; then
    local existing_vm
    existing_vm="$(cat "${sdir}/vm_name")"
    local port
    port="$(cat "${sdir}/ssh_port")"
    local state
    state="$(vm_state "$existing_vm")"

    if [ "$state" != "running" ]; then
      echo "[ai-sandbox] Starting existing sandbox '${existing_vm}'..."
      VBoxManage startvm "$existing_vm" --type headless
      wait_for_ssh "$port" "$ssh_key"
    fi

    rsync_to_vm "$repo" "$port" "$ssh_key"
    echo "[ai-sandbox] Sandbox ready: ${name} (ssh port ${port})"
    return
  fi

  # Create a new linked clone from the warm snapshot
  local t0=$SECONDS
  echo "[ai-sandbox] Cloning from warm snapshot..."
  VBoxManage clonevm "$WARM_VM_NAME" \
    --snapshot "$SNAPSHOT_NAME" \
    --options link \
    --name "$vm_name" \
    --register
  echo "[ai-sandbox] [$(( SECONDS - t0 ))s] Clone done"

  # Replace the inherited NAT rule with one on a free port
  local port
  port="$(find_free_port)"
  VBoxManage modifyvm "$vm_name" --natpf1 delete "ssh" 2>/dev/null || true
  VBoxManage modifyvm "$vm_name" --natpf1 "ssh,tcp,,${port},,22"
  echo "[ai-sandbox] [$(( SECONDS - t0 ))s] NAT configured"

  # Save session metadata
  mkdir -p "$sdir"
  echo "$vm_name" > "${sdir}/vm_name"
  echo "$port" > "${sdir}/ssh_port"
  echo "$repo" > "${sdir}/repo_path"

  echo "[ai-sandbox] Starting sandbox '${vm_name}' (ssh port ${port})..."
  VBoxManage startvm "$vm_name" --type headless
  echo "[ai-sandbox] [$(( SECONDS - t0 ))s] VM started"

  wait_for_ssh "$port" "$ssh_key"
  echo "[ai-sandbox] [$(( SECONDS - t0 ))s] SSH ready"

  rsync_to_vm "$repo" "$port" "$ssh_key"
  echo "[ai-sandbox] [$(( SECONDS - t0 ))s] Rsync done"

  echo "[ai-sandbox] Sandbox ready: ${name} (ssh port ${port})"
}

cmd_ssh() {
  local repo
  repo="$(resolve_repo "${1:-}")"
  local sdir
  sdir="$(session_dir_for "$repo")"

  [ -f "${sdir}/ssh_port" ] || die "No session found. Run 'ai-sandbox.sh run' first."

  local port
  port="$(cat "${sdir}/ssh_port")"
  local ssh_key="${WARM_DIR}/.vagrant/machines/default/virtualbox/private_key"

  ssh $SSH_OPTS -p "$port" -i "$ssh_key" rodrigo@127.0.0.1
}

cmd_stop() {
  local repo
  repo="$(resolve_repo "${1:-}")"
  local sdir
  sdir="$(session_dir_for "$repo")"

  [ -f "${sdir}/vm_name" ] || die "No session found."

  local vm_name
  vm_name="$(cat "${sdir}/vm_name")"

  echo "[ai-sandbox] Stopping '${vm_name}'..."
  VBoxManage controlvm "$vm_name" acpipowerbutton 2>/dev/null \
    || VBoxManage controlvm "$vm_name" poweroff 2>/dev/null \
    || echo "[ai-sandbox] VM was not running."
}

cmd_destroy() {
  local repo
  repo="$(resolve_repo "${1:-}")"
  local sdir
  sdir="$(session_dir_for "$repo")"

  [ -f "${sdir}/vm_name" ] || die "No session found."

  local vm_name
  vm_name="$(cat "${sdir}/vm_name")"

  echo "[ai-sandbox] Destroying '${vm_name}'..."
  VBoxManage controlvm "$vm_name" poweroff 2>/dev/null || true
  sleep 1
  VBoxManage unregistervm "$vm_name" --delete 2>/dev/null || true
  rm -rf "$sdir"
  echo "[ai-sandbox] Destroyed: ${vm_name}"
}

cmd_list() {
  if [ ! -d "$SESSIONS_DIR" ] || [ -z "$(ls -A "$SESSIONS_DIR" 2>/dev/null)" ]; then
    echo "No sandbox sessions."
    return
  fi

  printf "%-25s %-15s %-8s %s\n" "NAME" "STATE" "PORT" "REPO"
  printf "%-25s %-15s %-8s %s\n" "----" "-----" "----" "----"
  for sdir in "${SESSIONS_DIR}"/*/; do
    [ -d "$sdir" ] || continue
    local name
    name="$(basename "$sdir")"
    local vm_name
    vm_name="$(cat "${sdir}/vm_name" 2>/dev/null || echo "?")"
    local port
    port="$(cat "${sdir}/ssh_port" 2>/dev/null || echo "?")"
    local repo
    repo="$(cat "${sdir}/repo_path" 2>/dev/null || echo "?")"
    local state
    state="$(vm_state "$vm_name")"
    printf "%-25s %-15s %-8s %s\n" "$name" "$state" "$port" "$repo"
  done
}

cmd_sync_back() {
  local repo
  repo="$(resolve_repo "${1:-}")"
  local sdir
  sdir="$(session_dir_for "$repo")"

  [ -f "${sdir}/ssh_port" ] || die "No session found."

  local port
  port="$(cat "${sdir}/ssh_port")"
  local ssh_key="${WARM_DIR}/.vagrant/machines/default/virtualbox/private_key"

  rsync_from_vm "$repo" "$port" "$ssh_key"
}

# --- Main ---
command="${1:-}"
shift || true

case "$command" in
  warmup)     cmd_warmup ;;
  run)        cmd_run "$@" ;;
  ssh)        cmd_ssh "$@" ;;
  stop)       cmd_stop "$@" ;;
  destroy)    cmd_destroy "$@" ;;
  list)       cmd_list ;;
  sync-back)  cmd_sync_back "$@" ;;
  *)
    echo "Usage: ai-sandbox.sh {warmup|run|ssh|stop|destroy|list|sync-back} [REPO_PATH]"
    exit 1
    ;;
esac

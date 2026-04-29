# shellcheck shell=bash
#
# Shared helpers for the bats end-to-end suite. Sourced from each .bats file
# via `load test_helper` (bats looks for test_helper.bash in the test's dir).
#
# To run the suite:
#   make build-cli            # ensures bin/ai-playground is fresh
#   make test                 # invokes bats tests/
#
# Tests run with --prefix aiptest so the libvirt domains they create
# (aiptest-foo, aiptest-bar, ...) cannot collide with the user's normal
# aip-* pool. cleanup_all_test_workers wipes every aiptest- domain whether
# the test passed, failed, or crashed mid-flight.

REPO_ROOT="$(git -C "$(dirname "$BATS_TEST_FILENAME")" rev-parse --show-toplevel)"
AIP_BIN="$REPO_ROOT/cli/bin/ai-playground"
GOLDEN_IMAGE="${XDG_DATA_HOME:-$HOME/.local/share}/ai-playground/golden/ai-playground-base.qcow2"
TEST_PREFIX="aiptest"

# Tests run with an isolated XDG_CONFIG_HOME so they don't depend on (or
# clobber) the user's real config.yaml. We pre-place a minimal config so
# the daily commands' "config.yaml + golden image required" precondition
# is satisfied; the actual golden image is consumed from the user's real
# $XDG_DATA_HOME/ai-playground/golden/ (built by `ai-playground build`).
_aip_test_setup_config() {
    local cfg_root cfg_dir cfg_file
    cfg_root="$(mktemp -d -t aiptest-config-XXXXXX)"
    cfg_dir="$cfg_root/ai-playground"
    cfg_file="$cfg_dir/config.yaml"
    mkdir -p "$cfg_dir"
    cat > "$cfg_file" <<'EOF'
vm_user: vm
provision:
  include: []
on_conflict: keep
EOF
    export XDG_CONFIG_HOME="$cfg_root"
}

_aip_test_setup_config

# aip wraps ai-playground with the test-scoped prefix. Use this in tests
# instead of calling $AIP_BIN directly so list-workers, shutdown-worker, etc.
# all stay scoped to the test pool.
aip() {
    "$AIP_BIN" --prefix "$TEST_PREFIX" "$@"
}

# cleanup_all_test_workers tears down every libvirt domain carrying our
# test prefix, regardless of state. Idempotent — safe to call from
# setup_file *and* teardown_file. Bypasses the CLI to be robust against
# CLI bugs.
cleanup_all_test_workers() {
    local d
    while read -r d; do
        [ -z "$d" ] && continue
        virsh -c qemu:///system destroy   "$d" >/dev/null 2>&1 || true
        virsh -c qemu:///system undefine --remove-all-storage "$d" >/dev/null 2>&1 || true
    done < <(virsh -c qemu:///system list --all --name 2>/dev/null \
                | grep "^${TEST_PREFIX}-" || true)
}

# wait_for_ssh polls until SSH on the given worker actually answers. Returns
# 0 and prints the IP on success; returns 1 on timeout. cloud-init typically
# takes 10-30s after the IP is assigned before sshd is ready.
#
# We parse list-workers for the IP (the CLI doesn't expose a standalone
# `ip` subcommand — IP is shown in add-worker's post-output and list-workers
# table).
wait_for_ssh() {
    local name="$1"
    local timeout="${2:-120}"
    local deadline=$(( SECONDS + timeout ))
    local ip=""
    while [ "$SECONDS" -lt "$deadline" ]; do
        ip="$(aip list-workers 2>/dev/null \
                | awk -v n="$name" '$1==n && $3 ~ /^[0-9]+\./ {print $3; exit}')"
        [ -n "$ip" ] && break
        sleep 3
    done
    [ -z "$ip" ] && return 1
    until ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
              -o LogLevel=ERROR -o ConnectTimeout=2 -o BatchMode=yes \
              "vm@$ip" 'true' 2>/dev/null; do
        [ "$SECONDS" -ge "$deadline" ] && return 1
        sleep 3
    done
    echo "$ip"
}

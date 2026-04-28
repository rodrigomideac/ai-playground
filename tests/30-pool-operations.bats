#!/usr/bin/env bats
#
# Multi-worker pool semantics: random selection for ssh-worker and
# shutdown-worker when invoked without a name, and pool-count changes
# under those operations.

load test_helper

setup_file() {
    cleanup_all_test_workers
    aip add-worker pool1 --no-wait
    aip add-worker pool2 --no-wait
    wait_for_ssh pool1 180 >/dev/null || return 1
    wait_for_ssh pool2 180 >/dev/null || return 1
}

teardown_file() {
    cleanup_all_test_workers
}

# count_workers prints the number of rows under the NAME header in
# list-workers (i.e. the actual worker count, ignoring the header line).
count_workers() {
    aip list-workers | tail -n +2 | grep -c '.'
}

@test "list-workers shows both workers running with IPs" {
    run aip list-workers
    [ "$status" -eq 0 ]
    echo "$output" | grep -qE '^pool1[[:space:]]+running[[:space:]]+[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+'
    echo "$output" | grep -qE '^pool2[[:space:]]+running[[:space:]]+[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+'
}

@test "ssh-worker without a name lands on one of the running workers" {
    run aip ssh-worker -- 'hostname'
    [ "$status" -eq 0 ]
    # The hostname comes back on its own line; the "Connecting to ..." line
    # also matches but won't hit ^pool[12]$ exactly.
    echo "$output" | grep -qE '^pool[12]$'
}

@test "ssh-worker pool1 -- runs the command on pool1 specifically" {
    run aip ssh-worker pool1 -- 'hostname'
    [ "$status" -eq 0 ]
    echo "$output" | grep -qE '^pool1$'
}

@test "shutdown-worker without a name reduces the pool by one" {
    before="$(count_workers)"
    [ "$before" -ge 1 ]

    run aip shutdown-worker
    [ "$status" -eq 0 ]

    after="$(count_workers)"
    [ "$after" -eq $((before - 1)) ]
}

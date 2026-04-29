#!/usr/bin/env bats
#
# Single-worker lifecycle: add → list → shutdown.
# Each test creates and destroys its own worker so they're independent.
# Slow (~3 spawns) but proves the CRUD path is solid.

load test_helper

setup_file() {
    cleanup_all_test_workers
}

teardown_file() {
    cleanup_all_test_workers
}

@test "add-worker creates a worker that appears in list-workers" {
    run aip add-worker lifecycle1 --no-wait
    [ "$status" -eq 0 ]
    echo "$output" | grep -q 'Worker lifecycle1 ready'

    run aip list-workers
    [ "$status" -eq 0 ]
    echo "$output" | grep -q '^lifecycle1'

    aip shutdown-worker lifecycle1
}

@test "add-worker (default --wait) returns with an IP visible in the table" {
    run aip add-worker lifecycle2
    [ "$status" -eq 0 ]
    echo "$output" | grep -qE 'lifecycle2[[:space:]]+running[[:space:]]+[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+'

    aip shutdown-worker lifecycle2
}

@test "shutdown-worker by name removes it from the pool" {
    aip add-worker lifecycle3 --no-wait

    run aip shutdown-worker lifecycle3
    [ "$status" -eq 0 ]

    run aip list-workers
    if echo "$output" | grep -q '^lifecycle3'; then
        echo "lifecycle3 still in pool: $output" >&2
        return 1
    fi
}

@test "add-worker rejects an existing name" {
    aip add-worker lifecycle4 --no-wait
    run aip add-worker lifecycle4
    [ "$status" -ne 0 ]
    echo "$output" | grep -q 'already exists'
    aip shutdown-worker lifecycle4
}

@test "add-worker rejects an invalid name" {
    run aip add-worker 'BadName!'
    [ "$status" -ne 0 ]
    echo "$output" | grep -q 'invalid name'
}

@test "shutdown-worker on empty pool errors cleanly" {
    cleanup_all_test_workers
    run aip shutdown-worker
    [ "$status" -ne 0 ]
    echo "$output" | grep -q 'no running workers'
}

#!/usr/bin/env bats
#
# Host-side prerequisite checks. No worker VMs are spawned here; this is
# the fastest possible "is the environment sane" pass, run first so the
# rest of the suite can assume a healthy baseline.

load test_helper

@test "ai-playground binary exists and runs" {
    [ -x "$AIP_BIN" ]
    run "$AIP_BIN" --help
    [ "$status" -eq 0 ]
}

@test "golden qcow2 image is present" {
    [ -f "$GOLDEN_IMAGE" ]
}

@test "libvirtd is active" {
    run systemctl is-active libvirtd
    [ "$status" -eq 0 ]
}

@test "user has read+write on /dev/kvm" {
    [ -r /dev/kvm ]
    [ -w /dev/kvm ]
}

@test "default libvirt network is running" {
    run virsh -c qemu:///system net-info default
    [ "$status" -eq 0 ]
    echo "$output" | grep -q 'Active:.*yes'
}

@test "default storage pool is running" {
    run virsh -c qemu:///system pool-info default
    [ "$status" -eq 0 ]
    echo "$output" | grep -q 'State:.*running'
}

@test "user can write to the storage pool" {
    poolpath="$(virsh -c qemu:///system pool-dumpxml default \
                  | grep -oP '<path>\K[^<]+')"
    [ -n "$poolpath" ]
    [ -w "$poolpath" ]
}

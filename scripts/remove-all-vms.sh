#!/bin/bash
set -euo pipefail

# Removes all VirtualBox VMs: powers them off if running, then unregisters and deletes them.

vms=$(vboxmanage list vms | sed -E 's/^"(.+)" \{.*\}$/\1/')

if [ -z "$vms" ]; then
    echo "No VMs found."
    exit 0
fi

echo "Found VMs:"
echo "$vms"
echo

while IFS= read -r vm; do
    state=$(vboxmanage showvminfo "$vm" --machinereadable | grep -E '^VMState=' | cut -d'"' -f2)

    if [ "$state" = "running" ] || [ "$state" = "paused" ]; then
        echo "Powering off '$vm' (state: $state)..."
        vboxmanage controlvm "$vm" poweroff || true
        sleep 2
    fi

    echo "Unregistering and deleting '$vm'..."
    vboxmanage unregistervm "$vm" --delete
    echo
done <<< "$vms"

echo "All VMs removed."


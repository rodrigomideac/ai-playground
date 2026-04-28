# ai-playground

A local KVM/libvirt worker pool for running coding agents in disposable
Debian 13 VMs.

`ai-playground` is a Go CLI that manages a pool of throwaway worker VMs
cloned from a custom-built Debian 13 golden qcow2 image. Each worker
gets a DHCP-assigned IP on libvirt's default network, runs cloud-init
at first boot for per-VM personalization (user, SSH key, optional
shared folder), and tears down to nothing when you're done.

The golden image ships with Docker, oh-my-zsh, neovim, and
qemu-guest-agent by default. Add anything else via the [provisioning
hooks](#customization).

## Why VMs and not containers

Coding agents that run unsupervised are productive but risky — they can
`rm -rf /`, exfiltrate secrets, or open network listeners by accident.
Containers reduce that risk; full VMs go further:

- Full kernel isolation from the host
- Only the project folder is shared (and only when you ask, via
  virtio-9p)
- VM-escape CVEs exist but are narrower than container-escape ones

This is a personal/local-first tool, not a hardened multi-tenant
sandbox.

## Quick start

### Prerequisites

You need: KVM/QEMU, libvirt, Packer, Go, xorriso, and bats (for tests).

<details>
<summary><b>Manjaro / Arch</b></summary>

```bash
sudo pacman -S qemu-desktop libvirt virt-install bridge-utils \
               packer go libisoburn bats
sudo systemctl enable --now libvirtd
sudo usermod -aG kvm,libvirt "$USER"
# log out / back in for the new groups to take effect
```

</details>

<details>
<summary><b>Debian / Ubuntu</b></summary>

```bash
sudo apt install qemu-system-x86 libvirt-daemon-system virtinst \
                 bridge-utils packer golang xorriso bats
sudo systemctl enable --now libvirtd
sudo usermod -aG kvm,libvirt "$USER"
```

</details>

<details>
<summary><b>Fedora</b></summary>

```bash
sudo dnf install @virtualization libvirt virt-install bridge-utils \
                 packer golang xorriso bats
sudo systemctl enable --now libvirtd
sudo usermod -aG kvm,libvirt "$USER"
```

</details>

One-time host fix so the CLI can write disk overlays without sudo:

```bash
sudo chgrp libvirt /var/lib/libvirt/images
sudo chmod g+rwxs  /var/lib/libvirt/images
```

(The setgid bit makes new files inherit the `libvirt` group, which
plays nicely with libvirt's `dynamic_ownership`.)

### Build

```bash
make build-from-base   # downloads the Debian cloud qcow2, bakes provisioners (~3-5 min)
make build-cli         # builds bin/ai-playground (~5s)
```

Optionally put the CLI on `$PATH`:

```bash
sudo install -m 0755 bin/ai-playground /usr/local/bin/
```

### Use

```bash
ai-playground add-worker            # spin up a worker (auto-named, prints pool table)
ai-playground add-worker my-task    # named worker
ai-playground list-workers          # show the pool
ai-playground ssh-worker            # ssh into a random running worker
ai-playground ssh-worker my-task    # ssh into a specific one
ai-playground shutdown-worker       # tear down a random running worker
ai-playground shutdown-worker my-task
```

`add-worker` accepts:

- `--mount /host/path` — share a host directory inside the worker at
  `/home/vm/project` via virtio-9p
- `--memory MiB` (default 4096), `--cpus N` (default 2) — sizing
- `--no-wait` — return without waiting for the new worker's IP

### Test

```bash
make test   # bats tests/ — ~3-5 minutes including worker spawns
```

The suite verifies host prerequisites, the CLI's CRUD path, golden
image content (vm user, no debian user, /home/vm/.claude overlay,
docker daemon, etc.), and multi-worker pool semantics.

## Project layout

```
base-iso/packer/      Packer template (qemu builder, cloud-image input)
  default-provision/    Numbered provisioning scripts run during build
  seed/                 Build-only NoCloud seed (gitignored runtime files)
chroot/etc/skel/      Files copied into each worker's home at user creation
cmd/ai-playground/    Go CLI source
internal/worker/      Worker package (Manager, Worker, seed builder)
scripts/              Build helpers (prereqs, prep seed, provision-chroot, test-boot)
tests/                bats end-to-end test suite
docs/                 Long-form docs (e.g. custom-provisioning.md)
```

## Customization

Drop scripts into `base-iso/packer/custom-provision/` to extend or
override provisioning steps. By default:

| Prefix | Script | What it does |
|--------|--------|--------------|
| `00` | `base-packages.sh` | apt install of cloud-init, qemu-guest-agent, curl, git, neovim, rsync, zsh |
| `10` | `shell-config.sh` | oh-my-zsh, bashrc settings |
| `20` | `claude-code.sh` | Claude Code CLI |
| `30` | `docker.sh` | Docker, rootless setup |

Same numeric prefix replaces the default. To add a step at position 25
between Claude Code and Docker, drop `custom-provision/25-my-tools.sh`.
To skip Docker, drop `custom-provision/30-skip.sh` containing only
`echo "skipping"`. Full details in
[docs/custom-provisioning.md](docs/custom-provisioning.md).

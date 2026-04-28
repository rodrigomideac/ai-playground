This repo builds a Debian 13 (Trixie) golden qcow2 image and provides
a Go CLI (`ai-playground`) that orchestrates a local pool of disposable
worker VMs cloned from it via libvirt. Intended use: hosting coding
agents in per-task ephemeral VMs with their own DHCP-assigned IPs on
the libvirt default network.

The image is built by Packer's qemu builder from the Debian
generic-cloud qcow2 (snapshot pinned in `base-iso/packer/template.pkr.hcl`).
Per-VM personalization at first boot is delivered by cloud-init via a
NoCloud seed ISO that the CLI builds per worker.

`scripts/` holds repository scripts (build helpers, lint, prerequisite
checks, single-VM smoke boot). `chroot/` holds files rsync'd into the
image at build time, preserving the folder structure under target
system locations (root-owned). Per-user content goes under
`chroot/etc/skel/` so cloud-init's `useradd -m` copies it into each
worker's home with the correct ownership.

The build targets Debian 13 amd64 only. Bumping to a different
snapshot requires updating `debian_snapshot_dir` and
`debian_image_filename` in `base-iso/packer/template.pkr.hcl`.

## Provisioning hooks

Provisioning at build time is split into numbered scripts in
`base-iso/packer/default-provision/`. Users can override or extend by
placing scripts in `base-iso/packer/custom-provision/` (gitignored);
same numeric prefix replaces the default. See
[docs/custom-provisioning.md](docs/custom-provisioning.md) for full
details.

## Documentation index

Subsystem docs live as path-scoped rules under
`.claude/rules/docs-*.md` and auto-load when matching files are read
or edited.

- [`docs-ai-playground-cli.md`](.claude/rules/docs-ai-playground-cli.md)
  — `cmd/ai-playground` Go CLI + `internal/worker`: libvirt-based
  orchestration for a pool of disposable worker VMs cloned from the
  golden qcow2. Covers the public surface
  (`add-worker`/`ssh-worker`/`shutdown-worker`/`list-workers`), the
  non-obvious virt-install traps (`--machine pc`, `--video vga`, vCPU
  topology), and the `/var/lib/libvirt/images` permission setup.

This repo builds a Debian 13 (Trixie) golden qcow2 image and provides
a Go CLI (`ai-playground`) that orchestrates a local pool of disposable
worker VMs cloned from it via libvirt. Intended use: hosting coding
agents in per-task ephemeral VMs with their own DHCP-assigned IPs on
the libvirt default network.

The image is built by Packer's qemu builder from the Debian
generic-cloud qcow2 (snapshot pinned in `packer/template.pkr.hcl`).
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
`debian_image_filename` in `packer/template.pkr.hcl`.

## Provisioning hooks

Provisioning at build time is split into numbered scripts in
`packer/default-provision/`. Users can override or extend by
placing scripts in `packer/custom-provision/` (gitignored); same
numeric prefix replaces the default. Full reference (override rule,
naming, script-writing rules, common patterns) lives in the
auto-loaded rule [`docs-provisioning-hooks.md`](.claude/rules/docs-provisioning-hooks.md).

## Documentation index

Subsystem docs live as path-scoped rules under
`.claude/rules/docs-*.md` and auto-load when matching files are read
or edited.

- [`docs-ai-playground-cli.md`](.claude/rules/docs-ai-playground-cli.md)
  — `cli/cmd/ai-playground` Go CLI + `cli/internal/worker`: libvirt-based
  orchestration for a pool of disposable worker VMs cloned from the
  golden qcow2. Covers the public surface
  (`add-worker`/`ssh-worker`/`shutdown-worker`/`list-workers`), the
  non-obvious virt-install traps (`--machine pc`, `--video vga`, vCPU
  topology), and the `/var/lib/libvirt/images` permission setup.
- [`docs-test-boot.md`](.claude/rules/docs-test-boot.md) — Libvirt-
  bypass smoke-boot recipe (raw qemu + SLIRP) auto-loaded when editing
  `packer/template.pkr.hcl`. Used to disambiguate image bugs
  from libvirt/CLI bugs, and as the diff baseline for libvirt-specific
  qemu issues.
- [`docs-provisioning-hooks.md`](.claude/rules/docs-provisioning-hooks.md)
  — Packer's numbered hook chain (`packer/run-provision.sh` +
  `packer/{default,custom}-provision/`): override rule, naming
  convention, script-writing rules, and common patterns for inserting,
  replacing, or skipping default provisioners.

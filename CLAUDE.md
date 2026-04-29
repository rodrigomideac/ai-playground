This repo builds a Debian 13 (Trixie) golden qcow2 image and provides
a Go CLI (`ai-playground`) that orchestrates the build itself plus a
local pool of disposable worker VMs cloned from the resulting image
via libvirt. Intended use: hosting coding agents in per-task ephemeral
VMs with their own DHCP-assigned IPs on the libvirt default network.

The image is built by Packer's qemu builder from the Debian
generic-cloud qcow2 (snapshot pinned in `packer/template.pkr.hcl`).
Per-VM personalization at first boot is delivered by cloud-init via a
NoCloud seed ISO that the CLI builds per worker.

`ai-playground init` walks first-time users through doctor checks,
script selection, and seeding `$XDG_CONFIG_HOME/ai-playground/build/`.
`ai-playground build` runs (or re-runs) Packer against that build
directory and writes the golden image to
`$XDG_DATA_HOME/ai-playground/golden/ai-playground-base.qcow2`. Daily
commands (`add-worker`, `ssh-worker`, `shutdown-worker`,
`list-workers`) read both `config.yaml` and the golden image; if
either is missing they exit with `run 'ai-playground build' first`.

The in-repo `packer/provision/` and `chroot/` trees are the *source*
the CLI populates the user's `build/` from. `chroot/` is rsync'd into
the image at build time, preserving the folder structure under target
system locations (root-owned). Per-user content goes under
`chroot/etc/skel/` so `useradd -m` copies it into each worker's home
with the correct ownership.

The build targets Debian 13 amd64 only. Bumping to a different
snapshot requires updating `debian_snapshot_dir` and
`debian_image_filename` in `packer/template.pkr.hcl`.

## Documentation index

Subsystem docs live as path-scoped rules under
`.claude/rules/docs-*.md` and auto-load when matching files are read
or edited.

- [`docs-ai-playground-cli.md`](.claude/rules/docs-ai-playground-cli.md)
  — `cli/cmd/ai-playground` Go CLI + `cli/internal/worker`: libvirt-based
  orchestration for the pool of disposable worker VMs cloned from the
  golden qcow2. Covers the public surface
  (`add-worker`/`ssh-worker`/`shutdown-worker`/`list-workers`), the
  non-obvious virt-install traps (`--machine pc`, `--video vga`, vCPU
  topology), and the `/var/lib/libvirt/images` permission setup.
- [`docs-ai-playground-build.md`](.claude/rules/docs-ai-playground-build.md)
  — `init`/`build`/`reset` lifecycle: state machine, doctor checks,
  XDG filesystem layout, `config.yaml` contract, repo-source
  resolution (`--repo-path` / `AI_PLAYGROUND_REPO`), and the
  CLI ↔ Packer-template variable contract.
- [`docs-test-boot.md`](.claude/rules/docs-test-boot.md) — Libvirt-
  bypass smoke-boot recipe (raw qemu + SLIRP) auto-loaded when editing
  `packer/template.pkr.hcl`. Used to disambiguate image bugs
  from libvirt/CLI bugs, and as the diff baseline for libvirt-specific
  qemu issues.
- [`docs-provisioning-hooks.md`](.claude/rules/docs-provisioning-hooks.md)
  — Single-directory provision-script chain (`packer/run-provision.sh`
  + `packer/provision/`): naming convention, `# Description:` header
  used by the `init` per-script approval, and script-writing rules.

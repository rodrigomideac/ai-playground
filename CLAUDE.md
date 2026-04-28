This repo contains a solution to build a custom tailored Vagrant Box that will be use to run coding agents.

We will use a starting debian13 netinst iso to create a simple vagrant box.

base-iso/ contains a debian13 minimal netinst iso.
base-iso/packer contains a Packer template that will generate a simple vagrant box for debian13.

scripts/ must contain repository scripts.

The build targets only Debian 13.3.0 amd64. Updating to a different Debian point release requires changing the ISO URL, filename, and checksums in scripts/download-iso.sh and base-iso/packer/template.pkr.hcl.

Contents of chroot/ will be copied to the vagrant box preserving the folder structure.

## Provisioning Hooks

Provisioning is split into numbered scripts in `base-iso/packer/default-provision/`. Users can override or extend provisioning by placing scripts in `base-iso/packer/custom-provision/` (gitignored). Scripts with the same numeric prefix as a default script replace it. See [docs/custom-provisioning.md](docs/custom-provisioning.md) for full details.

## Documentation index

Subsystem docs live as path-scoped rules under `.claude/rules/docs-*.md` and auto-load when matching files are read or edited.

- [`docs-ai-playground-cli.md`](.claude/rules/docs-ai-playground-cli.md) — `cmd/ai-playground` Go CLI + `internal/worker`: libvirt-based orchestration for a pool of disposable worker VMs cloned from the golden qcow2. Covers the public surface (`add-worker`/`ssh-worker`/`shutdown-worker`/`list-workers`), the non-obvious virt-install traps (`--machine pc`, `--video vga`, vCPU topology), and the `/var/lib/libvirt/images` permission setup.
- [`docs-ai-sandbox-script.md`](.claude/rules/docs-ai-sandbox-script.md) — `ai-sandbox.sh` shell CLI + `Vagrantfile.template`: warm-VM + linked-clone architecture for spawning disposable sandbox VMs. Will be retired once the Go CLI fully replaces it.


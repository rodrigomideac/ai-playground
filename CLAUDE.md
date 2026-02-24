This repo contains a solution to build a custom tailored Vagrant Box that will be use to run coding agents.

We will use a starting debian13 netinst iso to create a simple vagrant box.

base-iso/ contains a debian13 minimal netinst iso.
base-iso/packer contains a Packer template that will generate a simple vagrant box for debian13.

scripts/ must contain repository scripts.

The build targets only Debian 13.3.0 amd64. Updating to a different Debian point release requires changing the ISO URL, filename, and checksums in scripts/download-iso.sh and base-iso/packer/template.pkr.hcl.

Contents of chroot/ will be copied to the vagrant box preserving the folder structure.

## Provisioning Hooks

Provisioning is split into numbered scripts in `base-iso/packer/default-provision/`. Users can override or extend provisioning by placing scripts in `base-iso/packer/custom-provision/` (gitignored). Scripts with the same numeric prefix as a default script replace it. See [docs/custom-provisioning.md](docs/custom-provisioning.md) for full details.


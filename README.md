# ai-playground

Have you wanted reproducible VMs to run agent workloads?

This repo contains a CLI tool that can be used to spin up KVM/libvirt VMs, all preconfigured with tools that you decide.

The golden image ships with Docker, oh-my-zsh, neovim, and qemu-guest-agent by default. Add anything else via the [provisioning hooks](#customization).

## Why VMs and not containers

There is plethora of tools providing sandbox environments, but I wanted something that allowed me to:
- Run agents with their own docker stack, since several projects that I use rely on `docker-compose.yaml` to spin up local stack;
- Not worry too much if the sandboxing is secure enough.

Basically, I wanted a reproducible development environment. Some benefits of using VMs:
- Full kernel isolation from the host
- Only the project folder is shared (and only when you ask)
- VM-escape CVEs exist but are narrower than container-escape ones

This is a personal/local-first tool, use it at your own risk!

## Quick start

### Prerequisites

You need: KVM/QEMU, libvirt, Packer, Go (to build the CLI), xorriso, git, ssh-keygen, and bats (for tests). `ai-playground` will run a doctor check on first run that names anything missing.

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

(The setgid bit makes new files inherit the `libvirt` group, which plays nicely with libvirt's `dynamic_ownership`.)

### Build the CLI

```bash
make build-cli
sudo install -m 0755 cli/bin/ai-playground /usr/local/bin/   # optional: put on $PATH
```

### First run

```bash
ai-playground build
```

`build` is the unified entrypoint. The first time it runs, it:

1. Runs doctor checks against your host, listing any fixes you need to apply.
2. Clones the public repo into `$XDG_CACHE_HOME/ai-playground/repo/`.
3. Walks you through which provision scripts to include and the username to create inside each worker.
4. Writes `$XDG_CONFIG_HOME/ai-playground/config.yaml` and seeds `$XDG_CONFIG_HOME/ai-playground/build/` with the template, runner, scripts, and `chroot/` overlay.
5. Generates a build-only ed25519 keypair under `$XDG_CACHE_HOME/ai-playground/seed/`, runs Packer, and writes the result to `$XDG_DATA_HOME/ai-playground/golden/ai-playground-base.qcow2` (~3-5 minutes).

Re-running `ai-playground build` after editing a script under `$XDG_CONFIG_HOME/ai-playground/build/provision/` rebuilds the golden image with the change. There is also a standalone `ai-playground init` that does steps 1-4 only — useful if you want to edit `build/` before starting the (~5 minute) Packer run.

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

- `--mount /host/path` — share a host directory inside the worker at `/home/<vm_user>/project` via virtio-9p
- `--memory MiB` (default 4096), `--cpus N` (default 2) — sizing
- `--no-wait` — return without waiting for the new worker's IP

### Reset

```bash
ai-playground reset                 # wipe config + cache + data dirs (after confirmation)
```

`reset` does not touch existing libvirt domains — `shutdown-worker` is for those.

### Test

```bash
make test   # bats tests/ — ~3-5 minutes including worker spawns
```

The suite verifies host prerequisites, the CLI's CRUD path, golden image content (vm user, no debian user, /home/<vm_user>/.claude overlay, docker daemon, etc.), and multi-worker pool semantics.

## Customization

After `ai-playground init` (or the first `ai-playground build`) the build inputs live under `$XDG_CONFIG_HOME/ai-playground/build/`:

- `provision/` — numbered shell scripts (`NN-name.sh`) executed in order during the Packer build. Edit, add, or delete files freely; re-run `ai-playground build` to apply. Default scripts ship with prefixes 00 (base packages), 10 (oh-my-zsh + bashrc), 20 (Claude Code CLI), 30 (Docker rootless).
- `chroot/etc/skel/` — files placed here land in each worker's home directory at user-creation time (root-owned in the image; `useradd -m` chowns them to the worker user). This is where to put dotfiles, agent config, etc.
- `template.pkr.hcl`, `run-provision.sh` — generally edit-with-care; you only need to touch them for unusual builds.

Full naming/script-writing reference is in [`.claude/rules/docs-provisioning-hooks.md`](.claude/rules/docs-provisioning-hooks.md). Build-pipeline internals (state machine, doctor checks, Packer variables) are in [`.claude/rules/docs-ai-playground-build.md`](.claude/rules/docs-ai-playground-build.md).

## Headless

Pre-place a valid `config.yaml` and skip the interactive prompts:

```bash
mkdir -p ~/.config/ai-playground
cat > ~/.config/ai-playground/config.yaml <<'EOF'
vm_user: ci
provision:
  include:
    - 00-base-packages.sh
    - 10-shell-config.sh
    - 30-docker.sh
on_conflict: overwrite
EOF
ai-playground build
```

`on_conflict: overwrite` ensures repeated headless builds always pick up upstream changes to default scripts. Drop it (or set `keep`) if you've edited `build/provision/` files locally and want them preserved.

## Local development of the CLI itself

Use a checkout of this repo as the source tree instead of the public clone:

```bash
git clone https://github.com/rodrigomideac/ai-playground ~/src/ai-playground
cd ~/src/ai-playground
make build-cli
AI_PLAYGROUND_REPO=$PWD ./cli/bin/ai-playground init
AI_PLAYGROUND_REPO=$PWD ./cli/bin/ai-playground build
```

`--repo-path /path/to/repo` is the per-invocation form of the same override; the flag wins when both are set.

## Project layout

```
packer/                 Packer template + scripts (the source for build/)
  provision/              numbered provisioning scripts run during the build
  seed/                   in-repo seed (only used when running packer by hand)
chroot/etc/skel/        files copied into each worker's home at user creation
cli/                    Go module
  cmd/ai-playground/      CLI entrypoint (init, build, reset, add-worker, ...)
  internal/buildflow/     populate + drift detection
  internal/config/        config.yaml load/save/validate
  internal/doctor/        host-environment checks
  internal/paths/         XDG path resolution + distro detection
  internal/repo/          public-repo clone/fetch + override
  internal/seed/          build-only ed25519 keypair + user-data render
  internal/worker/        Manager, Worker, NoCloud seed builder
scripts/                Repo-development helpers (lint)
tests/                  bats end-to-end test suite
.claude/rules/          Auto-loaded Claude docs rules (one per subsystem)
```

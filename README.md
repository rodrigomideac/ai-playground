# ai-playground

Have you wanted reproducible VMs to run agent workloads?

This project is a CLI tool that generates a golden image `.qcow2` file, and spins up VMs with that as base disk.

The golden image ships with Docker, oh-my-zsh, neovim, and qemu-guest-agent by default. 

## Why VMs and not containers

There is plethora of tools providing sandbox environments, but I wanted something that allowed me to:
- Run agents with their own docker stack, since several projects that I use rely on `docker-compose.yaml` to spin up local stack;
- Not worry too much if the sandboxing is secure enough.

Basically, I wanted a reproducible development environment. Some benefits of using VMs:
- Full kernel isolation from the host
- Only the project folder is shared (and only when you ask)
- VM-escape CVEs exist but are narrower than container-escape ones

This is a personal/local-first tool, use it at your own risk!

## Getting started

Install the CLI:

```bash
go install github.com/rodrigomideac/ai-playground/cli/cmd/ai-playground@latest
```

Then verify your host has the rest of the dependencies (libvirt, qemu, Packer, ...):

```bash
ai-playground doctor
```

Anything missing prints with the literal command to fix it. Once doctor is green, walk through first-time setup and produce the golden image:

```bash
ai-playground build
```

It clones the public repo, walks you through which setup scripts to include and the worker-VM username, and runs Packer (~3-5 min on first run). After that, `ai-playground add-worker` spins up a worker.

For the full host-side setup (per-distro install lines, libvirt pool perms), see [docs/INSTALLATION.md](docs/INSTALLATION.md).

## Documentation

- [docs/INSTALLATION.md](docs/INSTALLATION.md) — host prerequisites, libvirt setup, building the CLI
- [docs/USAGE.md](docs/USAGE.md) — first build, daily commands (`add-worker` / `ssh-worker` / `list-workers` / `shutdown-worker`), `reset`, `doctor`, tests
- [docs/CUSTOMIZATION.md](docs/CUSTOMIZATION.md) — adding setup scripts and per-worker files; headless / CI use
- [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) — working on the CLI against a local checkout; project layout

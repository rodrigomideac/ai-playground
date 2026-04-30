# ai-playground

Have you wanted reproducible VMs to run agent workloads?

This repo contains a CLI tool that can be used to spin up KVM/libvirt VMs, all preconfigured with tools that you decide.

The golden image ships with Docker, oh-my-zsh, neovim, and qemu-guest-agent by default. Add anything else via the [provisioning hooks](docs/CUSTOMIZATION.md#provision-scripts).

## Why VMs and not containers

There is plethora of tools providing sandbox environments, but I wanted something that allowed me to:
- Run agents with their own docker stack, since several projects that I use rely on `docker-compose.yaml` to spin up local stack;
- Not worry too much if the sandboxing is secure enough.

Basically, I wanted a reproducible development environment. Some benefits of using VMs:
- Full kernel isolation from the host
- Only the project folder is shared (and only when you ask)
- VM-escape CVEs exist but are narrower than container-escape ones

This is a personal/local-first tool, use it at your own risk!

## Documentation

- [docs/INSTALLATION.md](docs/INSTALLATION.md) — host prerequisites, libvirt setup, building the CLI
- [docs/USAGE.md](docs/USAGE.md) — first build, daily commands (`add-worker` / `ssh-worker` / `list-workers` / `shutdown-worker`), `reset`, `doctor`, tests
- [docs/CUSTOMIZATION.md](docs/CUSTOMIZATION.md) — adding setup scripts and per-worker files; headless / CI use
- [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) — working on the CLI against a local checkout; project layout

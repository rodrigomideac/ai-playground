# Usage

Day-to-day use of `ai-playground` — first build, the daily commands, reset, tests, and the diagnostic. If you haven't installed yet, start with [INSTALLATION.md](INSTALLATION.md).

## First run

```bash
ai-playground build
```

`build` is the unified entrypoint. The first time it runs, it:

1. Checks that your host is healthy (the cheap subset of `doctor`); on a fresh host you'll likely also want to run the full `ai-playground doctor` once first.
2. Clones the public repo into `$XDG_CACHE_HOME/ai-playground/repo/`.
3. Walks you through which setup scripts to include and the username to create inside each worker.
4. Writes `$XDG_CONFIG_HOME/ai-playground/config.yaml` and seeds `$XDG_CONFIG_HOME/ai-playground/build/` with the template, runner, scripts, and `chroot/` overlay.
5. Generates a build-only SSH key under `$XDG_CACHE_HOME/ai-playground/seed/`, runs Packer, and writes the result to `/var/lib/libvirt/images/ai-playground-base.qcow2` (the libvirt pool — placed there so libvirt-qemu can open it as a backing file without crossing your `$HOME`) (~3-5 minutes).

There is also a standalone `ai-playground init` that does steps 1-4 only — useful if you want to edit `build/` before starting the (~5 minute) Packer run.

### Re-running build

Re-running `ai-playground build` after editing a script under `$XDG_CONFIG_HOME/ai-playground/build/provision/` rebuilds the golden image with the change.

If you have any workers still defined when you re-run `build`, you'll be asked to stop them first — each worker's disk is a copy-on-write overlay backed by the current golden image, and rebuilding would leave that chain pointing at a different file.

## Daily commands

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

If `config.yaml` or the golden image are missing, the daily commands fast-fail with `run 'ai-playground build' first`.

## Reset

```bash
ai-playground reset                 # wipe config + cache + data dirs (after confirmation)
```

After typing `reset` to confirm, if any workers exist with the configured `--prefix` you'll be asked whether to stop them and delete their disks too — answer `n` to leave them in place and use `shutdown-worker` later.

## Diagnostics

```bash
ai-playground doctor
```

Prints every host-environment check (CPU virtualization → KVM kernel module → qemu → libvirtd → libvirt clients → Packer → ai-playground host tooling) with a precise technical description and a manual-inspection command for each. Designed so that a coding agent reading the output has enough context to debug without guessing. Exit code is non-zero if any check fails.

## Tests

```bash
make test   # bats tests/ — ~3-5 minutes including worker spawns
```

The suite verifies host prerequisites, the CLI's CRUD path, golden image content (vm user, no debian user, /home/<vm_user>/.claude overlay, docker daemon, etc.), and multi-worker pool semantics.

## Next steps

- [CUSTOMIZATION.md](CUSTOMIZATION.md) — extending the golden image with your own setup scripts
- [DEVELOPMENT.md](DEVELOPMENT.md) — running the CLI against a local checkout of this repo

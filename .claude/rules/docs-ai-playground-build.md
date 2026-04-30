---
paths:
  - "cli/cmd/ai-playground/init.go"
  - "cli/cmd/ai-playground/build.go"
  - "cli/cmd/ai-playground/reset.go"
  - "cli/internal/buildflow/**"
  - "cli/internal/config/**"
  - "cli/internal/doctor/**"
  - "cli/internal/paths/**"
  - "cli/internal/repo/**"
  - "cli/internal/seed/**"
  - "packer/template.pkr.hcl"
---

# `ai-playground` build pipeline — state machine, doctor, Packer contract

`init` and `build` are the user-facing entry points for producing the
Debian golden qcow2 image. This rule covers their state machine, the
host-environment checks they run before doing anything, the layout
they populate under `$XDG_CONFIG_HOME/ai-playground/`, and the variable
contract between the CLI and `packer/template.pkr.hcl`. Auto-loaded
when editing any of those files.

For the worker-pool side (the daily `add-worker`/`ssh-worker`/etc.
commands), see [`docs-ai-playground-cli.md`](docs-ai-playground-cli.md).

## Filesystem layout

XDG-compliant. Standard fallbacks apply when `XDG_*_HOME` is unset.

| Path | Purpose |
|---|---|
| `$XDG_CONFIG_HOME/ai-playground/config.yaml` | Declarative spec of the build (`vm_user`, `provision.include`, `on_conflict`). |
| `$XDG_CONFIG_HOME/ai-playground/build/template.pkr.hcl` | Packer template. User-editable. |
| `$XDG_CONFIG_HOME/ai-playground/build/run-provision.sh` | In-VM provision runner. Uploaded by Packer. |
| `$XDG_CONFIG_HOME/ai-playground/build/provision/` | Numbered shell scripts the runner executes during the build. |
| `$XDG_CONFIG_HOME/ai-playground/build/chroot/` | Filesystem overlay rsync'd into the image. `etc/skel/` lands in each worker's home. |
| `$XDG_CACHE_HOME/ai-playground/repo/` | Clone of the public repo — the source from which `init` populates `build/`. |
| `$XDG_CACHE_HOME/ai-playground/seed/` | Build-only ed25519 keypair (`id_ed25519` + `.pub`), `user-data.tpl`, rendered `user-data`, `meta-data`. |
| `$XDG_CACHE_HOME/ai-playground/packer/` | Packer working dir / artifact output. |
| `$XDG_DATA_HOME/ai-playground/golden/ai-playground-base.qcow2` | The built golden image. The worker manager reads from here. |

`paths.go` (in `cli/internal/paths`) is the single source of truth for
these locations. Don't hard-code XDG paths anywhere else.

## State machine

Both `init` and `build` dispatch on what's already on disk:

| `config.yaml` | `build/` populated | `init` action | `build` action |
|---|---|---|---|
| missing  | missing  | Full handholding (TTY required) | Full handholding, then build (TTY required — non-TTY exits with a pointer to headless docs) |
| present  | populated | "already initialized" exit 0 | Cheap doctor + drift + Packer |
| present  | missing  | Headless populate from repo cache | Headless populate, then Packer |
| missing  | populated | Error: `inconsistent state — run 'ai-playground reset'` | Same error |

`build` is idempotent: re-running it after editing a script under
`build/provision/` rebuilds the golden image with the change.

## Pre-build worker check

Each worker's per-VM disk is a qcow2 overlay whose backing-file path
resolves to the golden image. Once `build` overwrites that file, every
still-defined worker (running or shut off) becomes inconsistent —
qemu would read new-golden bytes through old COW chains, corrupting
reads. To keep the pool consistent, `build` enumerates workers carrying
`--prefix` after the cheap doctor pass and:

- **TTY**: prompts "Stop all of them and delete their disks before
  building? [Y/n]". `y` destroys each via `Worker.Destroy` (same path
  as `shutdown-worker`); `n` aborts the build with a clear error.
- **Non-TTY** (CI, piped stdin): refuses with a pointer to
  `ai-playground shutdown-worker`. The user is expected to clean up
  workers before invoking headless builds.

This check happens *after* the cheap doctor pass (an unhealthy host
fails fast before we touch worker state) and *before* repo
resolution / Packer setup (so the build aborts before any expensive
work). Other-prefix workers are left alone — they don't share our
golden image.

## Repo source resolution (`internal/repo`)

`init` and `build` need the public repo's `packer/` and `chroot/`
trees as the source from which `build/` gets populated.

- **Default:** clone (or `git fetch && git reset --hard origin/master`)
  into `$XDG_CACHE_HOME/ai-playground/repo/`.
- **Override:** `--repo-path /path/to/local/repo` (persistent flag) or
  `AI_PLAYGROUND_REPO=/path/to/local/repo` (env). Flag wins. When
  active: the path is read directly, **no clone, no fetch, no writes
  into the path**, and the cache directory is left untouched.

The override path is validated up-front against the expected layout
(`packer/template.pkr.hcl`, `packer/provision/`, `packer/run-provision.sh`,
`packer/seed/user-data.tpl`, `packer/seed/meta-data`, `chroot/`). A
missing path produces a specific error naming what's absent.

## Doctor checks (`internal/doctor`)

`init` runs the full set; `build` re-runs the cheap subset (marked
**C**) to catch transient regressions like rebooting into a session
without the libvirt group. Doctor never runs `sudo` or any
destructive command — failures print the punch list and exit non-zero.

The standalone `ai-playground doctor` subcommand prints a verbose
diagnostic of the same set: stack-layer headings, the Verifies field
explaining what each check actually asserts in precise libvirt/qemu
terminology, and an Inspect command line for manual verification.
That output is the right starting point when handing a broken host
to a coding agent. See [`docs-cli-language.md`](docs-cli-language.md)
for the precision-first register the doctor follows.

Full set:

- **Required commands present:** `git`, `curl`, `qemu-system-x86_64`,
  `qemu-img`, `ssh-keygen`, `packer`, `virsh`, `virt-install`. Each
  missing entry prints the per-distro install hint. (The NoCloud seed
  ISO is built in-process via `github.com/diskfs/go-diskfs`, so no
  external ISO tool is checked.)
- **`packer` is HashiCorp Packer**, not Fedora's cracklib `packer`.
  Heuristic: `packer version` first line starts with `Packer v`.
- **`/dev/kvm` exists and is r/w by current user.** **C** Tested by
  opening for read+write; the file mode + group membership both
  matter, so testing the open is more reliable than parsing mode.
- **User is in `libvirt` group.** **C**
- **CPU has `vmx` or `svm`** in `/proc/cpuinfo`.
- **libvirtd reachable:** `virsh -c qemu:///system list >/dev/null` succeeds.
- **libvirt default network active + autostart.** **C** Parsed from
  `virsh net-info default`.
- **`/var/lib/libvirt/images` is group `libvirt`, mode `g+rwxs`.** **C**
- **SSH pubkey exists** at `~/.ssh/id_ed25519.pub` or `~/.ssh/id_rsa.pub`.

When a check fails, the printed hint is the literal shell command(s)
the user must run.

## `config.yaml` contract (`internal/config`)

```yaml
vm_user: rodrigo
provision:
  include:
    - 00-base-packages.sh
    - 10-shell-config.sh
    - 30-docker.sh
on_conflict: keep
```

- **`vm_user`** *(required)* — username cloud-init creates inside each
  worker on first boot.
- **`provision.include`** *(list)* — exact filenames to copy from the
  repo source's `packer/provision/` into `build/provision/` during
  populate. Scripts the user adds directly to `build/provision/` are
  unaffected by this list.
- **`on_conflict`** *(`keep` | `overwrite`, default `keep`)* — when a
  populate-step file already exists in `build/` with different
  contents, this controls whether to keep the user's version or
  overwrite from the repo source.

`config.Save` writes the YAML; `config.Load` and `config.MaybeLoad`
read it. `MaybeLoad` returns `(nil, nil)` for a missing file —
distinguishing "not initialized yet" from "broken config".

## Drift detection (`internal/buildflow/drift.go`)

Each `build` compares `provision.include` against the repo source's
`packer/provision/` directory and prints two notices when applicable:

- **New upstream scripts** (in repo, not in `include`): the user can
  opt in by adding the filename to `provision.include`.
- **Removed upstream scripts** (in `include`, no longer in repo):
  doesn't block the build — the user's local copy in
  `build/provision/` is authoritative.

Drift checking is best-effort: if the repo source can't be resolved
(e.g. offline with no cache), `build` warns and continues.

## Packer-template variable contract

The CLI passes one variable to `packer build`:

- **`-var seed_dir=$XDG_CACHE_HOME/ai-playground/seed`** — directory
  holding `id_ed25519` (private key for the build-only SSH connection),
  `user-data` (rendered NoCloud user-data), and `meta-data`. Default
  in the template is `${path.root}/seed` so running `packer build`
  by hand from the in-repo `packer/` dir still works.

The Packer template references:
- `${var.seed_dir}/user-data` and `meta-data` for `cd_files`.
- `${var.seed_dir}/id_ed25519` for `ssh_private_key_file`.
- `${path.root}/run-provision.sh` and `${path.root}/provision` (siblings of the template).
- `${path.root}/chroot` (also a sibling — *not* `${path.root}/../chroot`
  any more).

The `chroot/` overlay is rsynced into `/` inline by the template's
shell provisioner; there is no separate `provision-chroot.sh` script.

## Build artifact path

Packer's qemu builder writes:

```
$XDG_CACHE_HOME/ai-playground/packer/packer-ai-playground-base/ai-playground-base
```

`build` then renames it to:

```
$XDG_DATA_HOME/ai-playground/golden/ai-playground-base.qcow2
```

A cross-filesystem rename falls back to copy + delete. The worker
manager reads from the second path; the daily commands fast-fail
when it's missing.

## Build-only seed (`internal/seed`)

Replaces the old `scripts/prepare-packer-seed.sh`:

- **`EnsureKeypair`** — shells out to `ssh-keygen -t ed25519 -N ''` to
  create `id_ed25519` + `.pub` under `seed_dir` if absent. Idempotent.
- **`RenderUserData`** — substitutes `__SSH_PUBKEY__` in
  `user-data.tpl` with the public key, writes `seed_dir/user-data`.

The keypair is throwaway — used only to SSH into the build VM. The
last act of the build is `userdel -rf debian`, so the matching
authorized_keys never reaches the produced qcow2.

## Headless mode

No prompts. Pre-place a valid `config.yaml` at
`$XDG_CONFIG_HOME/ai-playground/config.yaml` and run
`ai-playground build`. The state machine routes "config present /
build missing" → headless populate → Packer.

If `build` is invoked with neither `config.yaml` nor a TTY, it exits
non-zero with a pointer to headless docs.

## Out of scope (intentional)

- **Pinning the repo cache** to a tag/branch/SHA (`repo_ref` in
  `config.yaml`).
- **Build-time VM sizing knobs** (`vm_memory`, `vm_cpus`) in
  `config.yaml`.
- **A full flag/env/config precedence layer** for runtime commands.
- **A smoke-test command** that boots the freshly-built golden image.
- **Remote libvirt hosts** (`qemu+ssh://...`).
- **A `--reconfigure` flag** on `init`. Workaround: edit
  `config.yaml` directly, or `reset`.

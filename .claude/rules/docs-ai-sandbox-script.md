---
paths:
  - "ai-sandbox.sh"
  - "Vagrantfile.template"
---

# ai-sandbox — fast disposable VMs for coding agents

`ai-sandbox.sh` is the CLI that spins up isolated Debian 13 VMs to run coding agents against arbitrary host repos. It uses a one-time "warm" template VM plus VBoxManage linked clones so each sandbox starts in ~19s and multiple sandboxes can run in parallel. The warm VM is created with Vagrant; all per-session lifecycle after that is driven directly through `VBoxManage` + `ssh` + `rsync` — Vagrant is not used to manage sandbox clones.

## Architecture

```
          ┌──────────────────────────────────────┐
warmup ─▶ │  debian13 box  ──(Vagrant)──▶  warm  │
          │                                 VM    │
          │                                 +snap  │  ← "clean" snapshot, VM halted
          └──────────────┬───────────────────────┘
                         │  VBoxManage clonevm --snapshot clean --options link
                         ▼
                 ┌─────────────────┐     ┌─────────────────┐
     run ~/a ─▶  │  sandbox-a      │     │  sandbox-b      │  ◀─ run ~/b
                 │  port 2222      │     │  port 2223      │
                 │  /home/rodrigo/ │     │  /home/rodrigo/ │
                 │    project ← a  │     │    project ← b  │
                 └─────────────────┘     └─────────────────┘
```

- `warmup` is a one-time step. It creates the VM `ai-sandbox-warm` at `~/.ai-sandbox/warm/`, boots it, takes a VirtualBox snapshot named `clean`, then halts it. The VM persists across host reboots.
- Each `run` creates a linked clone **from the `clean` snapshot** (not from the live VM). Clones share the warm VM's underlying disk through VirtualBox differencing disks — disk usage per clone stays tiny (usually well under 500MB until the sandbox does real work).
- All clones inherit the warm VM's SSH public key because they inherit its disk. The host-side private key lives at `~/.ai-sandbox/warm/.vagrant/machines/default/virtualbox/private_key` and is reused for every sandbox.

## Commands

| Command | Purpose |
|---------|---------|
| `ai-sandbox.sh warmup` | Build the warm template + snapshot. One-time. |
| `ai-sandbox.sh run [REPO]` | Fork a sandbox for REPO (default: cwd). Creates clone, starts VM, rsyncs repo into `/home/rodrigo/project`. Idempotent — re-running on an existing session just restarts + re-rsyncs. |
| `ai-sandbox.sh ssh [REPO]` | Interactive SSH into the sandbox. |
| `ai-sandbox.sh stop [REPO]` | ACPI power button (graceful), falls back to `poweroff` if the guest ignores it. |
| `ai-sandbox.sh destroy [REPO]` | Power off + `unregistervm --delete` + `rm -rf` session dir. |
| `ai-sandbox.sh list` | Table: name, state, port, repo path. |
| `ai-sandbox.sh sync-back [REPO]` | Rsync `/home/rodrigo/project` from VM back to REPO on host. |

All commands take REPO as a positional argument, default cwd. REPO is resolved to an absolute path and the session is keyed by `basename(REPO)` — two repos with the same basename will collide (known limitation).

## Env-var configuration

| Env var | Default | Used by |
|---------|---------|---------|
| `AI_SANDBOX_MEMORY` | `4096` | `warmup` only — sets warm VM RAM in MB. Clones inherit. |
| `AI_SANDBOX_CPUS` | `4` | `warmup` only — sets warm VM vCPU count. Clones inherit. |

Changing these after warmup has no effect on existing clones. To resize, destroy the warm VM (`VBoxManage unregistervm ai-sandbox-warm --delete`) and re-warmup.

## State layout

```
~/.ai-sandbox/
├── warm/
│   ├── Vagrantfile                    # copy of Vagrantfile.template
│   └── .vagrant/
│       └── machines/default/virtualbox/
│           └── private_key            # SSH key shared by all sandboxes
└── sessions/
    └── <repo-basename>/               # e.g. "ai" for ~/dev/ai
        ├── vm_name                    # "sandbox-<name>"
        ├── ssh_port                   # host port forwarded to guest :22
        └── repo_path                  # absolute path to source repo on host
```

The session dir contains no Vagrantfile and no `.vagrant/` — clones are managed purely by `VBoxManage`. Destroying a session is safe: no orphan state elsewhere.

## Port assignment

`find_free_port` starts at 2222 and increments until it finds a port not bound on the host (checked via `ss -tln`). First sandbox gets 2222, second gets 2223, and so on. The port is persisted in `sessions/<name>/ssh_port`.

VirtualBox NAT rule handling: the cloned VM inherits a NAT rule named `ssh` from the warm VM (host port 2222). Before applying the new port, `run` first does `modifyvm --natpf1 delete ssh` to remove the inherited rule, then adds the new one. Failing to do so causes `"A NAT rule of this name already exists"`.

## SSH hardening

All `ssh`/`rsync` invocations pass `IdentitiesOnly=yes`. Without it, any keys the user's SSH agent has loaded get offered first and the guest's `MaxAuthTries` is exceeded before the correct key is tried. Full options:

```
-o StrictHostKeyChecking=no
-o UserKnownHostsFile=/dev/null
-o IdentitiesOnly=yes
-o LogLevel=ERROR
```

`StrictHostKeyChecking=no` + `UserKnownHostsFile=/dev/null` avoids host-key prompts when sandboxes are created/destroyed on the same ports repeatedly.

## Rsync behaviour

`rsync_to_vm` uses `--archive --delete -z --safe-links --no-owner --no-group` with these excludes hard-coded:

```
.git/  node_modules/  .vagrant/  __pycache__/  .venv/  .claude/worktrees/
```

- `--safe-links` (not `--copy-links`) silently skips broken symlinks. `.claude/worktrees/` is also excluded outright because those trees can contain many broken symlinks from deleted worktrees (the original failure during development).
- `--delete` means rsync mirrors the host repo into the VM — files removed on the host disappear from the VM on next `run`.
- `--no-owner --no-group` because the guest user UID/GID won't match the host.

`rsync_from_vm` (`sync-back`) is identical but without `--delete` — it won't delete host-side files that are absent in the VM. This is conservative on purpose; if you want mirror-semantics sync-back, it's not the current design.

## Performance

One-time cost:

| Step | Time |
|------|------|
| `warmup` (box import + boot + snapshot + halt) | ~73s |

Per-sandbox cost (measured on the author's hardware, ~NVMe PCIe 3.0, 4GB RAM, 4 vCPU):

| Phase | Time |
|-------|------|
| `VBoxManage clonevm` (linked, from snapshot) | <1s |
| NAT rule replace | <1s |
| `VBoxManage startvm` | ~1s |
| Cold boot → SSH reachable (`wait_for_ssh`) | **~16s** (dominant) |
| Rsync repo | ~2s |
| **Total `run`** | **~19s** |

The 16s is the floor set by Debian cold-boot + systemd + sshd. Below that requires either:
1. **Save state on `stop`**: hibernate the sandbox instead of poweroff; resume is ~3-5s. Doesn't help fresh forks (clones can't share saved memory state).
2. **Boot-time trimming at Packer build**: disable unneeded systemd units in the base box to cut the cold-boot path.

Neither is implemented as of this writing.

## Relationship to Vagrant

- The repo's own `Vagrantfile` (used by `make build-from-base` for development of the ai-playground box itself) is unrelated to `Vagrantfile.template`. Don't confuse them.
- `Vagrantfile.template` exists solely to bootstrap the warm VM — Vagrant handles box import, linked-clone master preparation, SSH key generation, and the initial provisioning handshake. After warmup, Vagrant is not invoked for sandbox operations.
- `vagrant snapshot save clean` inside `cmd_warmup` creates a snapshot that `VBoxManage` can reference by name. This works because Vagrant snapshots are thin wrappers over VirtualBox snapshots.

## Known limitations

- **Session-name collisions**: two repos with the same basename (e.g. `~/dev/api`, `~/work/api`) resolve to the same session dir. Not yet mitigated.
- **No auto-warmup**: `run` errors out if the warm VM is missing. User must run `warmup` manually once.
- **Single warm VM**: only one warm template, so memory/CPU are global. Resizing means destroying and recreating it.
- **No sync-back conflict detection**: `sync-back` overwrites host files with VM content (without `--delete`). It's not a merge.
- **Instrumentation echoes in `cmd_run`**: the `[Xs] Clone done` / `[Xs] SSH ready` / `[Xs] Rsync done` lines were added for benchmarking and are still present. Remove if they become noisy.
- **No teardown of the warm VM via the CLI**: has to be done manually with `VBoxManage unregistervm ai-sandbox-warm --delete`.

## Testing & debugging

- `./ai-sandbox.sh list` is the fastest way to see what's alive. `VBoxManage list runningvms` confirms at the hypervisor level.
- If SSH hangs after `VM started`, the VM is usually still booting. Use `VBoxManage showvminfo <vm> --machinereadable | grep VMState` to confirm it's actually running, then `nc -zv 127.0.0.1 <port>` to see when the port opens.
- To rebuild the warm VM from scratch: `./ai-sandbox.sh destroy` each active sandbox (sessions share the warm disk, so destroying warm while sandboxes exist corrupts them), then `VBoxManage unregistervm ai-sandbox-warm --delete`, then `rm -rf ~/.ai-sandbox/warm`, then `./ai-sandbox.sh warmup`.
- `Vagrantfile.template` reads env vars at parse time (`ENV.fetch`), so `vagrant` subcommands run against `~/.ai-sandbox/warm/` need the same env as the initial warmup — in practice only `AI_SANDBOX_MEMORY` / `AI_SANDBOX_CPUS` matter.

---
paths:
  - "packer/run-provision.sh"
  - "packer/provision/**"
---

# Packer provisioning hooks

The Packer build runs a numbered chain of shell scripts to install
packages, set up shells, and otherwise customize the golden image
before it is captured. This rule covers the hook contract and how
`packer/run-provision.sh` discovers, orders, and executes the
scripts. Auto-loaded when editing the runner or any script under
`packer/provision/`.

## Directory layout (single directory)

```
packer/
  run-provision.sh    # runner; uploaded + executed by Packer
  provision/          # numbered scripts (NN-name.sh)
    00-base-packages.sh
    10-shell-config.sh
    20-claude-code.sh
    30-docker.sh
```

There is no separate "default" vs "custom" directory and no override
mechanism. Users edit, add, or delete scripts here directly (in their
own copy under `$XDG_CONFIG_HOME/ai-playground/build/provision/` —
the in-repo `packer/provision/` is the *source* the CLI populates
that copy from).

## Naming convention

Every script must match `<NN>-<name>.sh`:

- **`NN`** — two-digit numeric prefix that controls execution order.
- **`name`** — descriptive, dash-separated.
- **`.sh`** — required extension.

Default scripts use gaps of 10 (`00`, `10`, `20`, `30`) so users can
slot custom scripts at any position without renumbering.

A `# Description: ...` header comment anywhere near the top of the
script is read by `ai-playground init`'s per-script approval prompt
(the parser scans the file and grabs the first matching line):

```bash
#!/bin/bash
set -euo pipefail

# Description: Docker engine + rootless setup
```

Keep the description to one line. Add it to every new script that
ships in `packer/provision/`.

## Execution

`run-provision.sh PROVISION_DIR` does, on each Packer run:

1. Discovers every `*.sh` in `PROVISION_DIR`.
2. Extracts each script's numeric prefix; warns and skips files
   without one.
3. Sorts by prefix and executes in order.
4. Bails on the first non-zero exit (`set -euo pipefail`).

The runner prints the execution plan up-front and announces each
script as it starts:

```
============================================
 Provision Runner
============================================

 Execution Plan (4 scripts)
  [00] /tmp/provision/00-base-packages.sh
  [10] /tmp/provision/10-shell-config.sh
  [20] /tmp/provision/20-claude-code.sh
  [30] /tmp/provision/30-docker.sh
```

## Common patterns

**Insert a step between two defaults** (e.g. at `25`, between Claude
Code and Docker): drop a `25-my-tools.sh` into the directory.
Resulting order: `00` → `10` → `20` → `25` → `30`.

**Replace a default**: edit it in place. There is no override layer,
so the script you edit is the script that runs.

**Skip a default**: delete (or comment out) the script. The runner
no longer runs scripts that aren't in the directory.

## Script writing rules

- Always start with `#!/bin/bash` and `set -euo pipefail`. The runner
  doesn't isolate failures — first non-zero exit aborts the build.
- Use `sudo` for anything needing root. Packer connects as a regular
  user (the build-time `debian` user, removed at the end of the
  build — don't bake state into `/home/debian`).
- Prefer `echo "==> Description"` so the build log is greppable.
- Don't `source ~/.bashrc` — fragile in non-interactive shells.
- Append env vars to `~/.bashrc` directly:
  `echo 'export FOO=bar' >> ~/.bashrc`.
- Anything you put under `/home/<build-user>/...` will not survive
  the build. Per-user content has to go in `chroot/etc/skel/`
  instead, which `useradd -m` copies into each clone's home with the
  right ownership at first boot.

## Debugging a failed build

The runner names the failing script in its output. Re-run the build
to see which step's first non-zero exit aborted it. Common pitfalls:

- Apt locks if the script races cloud-init's package module — the
  template waits for cloud-init to finish before the runner starts,
  but if you're calling `apt-get` from `bootcmd` or similar in
  parallel, you'll deadlock.
- `apt-get update` failing transiently — re-run the build; the
  Debian mirrors aren't 100% reliable.
- `curl ... | bash` installers that require a TTY — usually fixable
  with `< /dev/null` or by downloading + executing in two steps.

## Out of scope (intentional)

- **Per-VM configuration.** This system is build-time only. Anything
  per-VM (hostname, SSH keys, project mounts) is delivered by
  cloud-init at first boot via the NoCloud seed; see
  [`docs-ai-playground-cli.md`](docs-ai-playground-cli.md).
- **Conditionals based on host environment.** Scripts run in a fresh
  Debian VM; they don't know or care about the host that's building
  them.

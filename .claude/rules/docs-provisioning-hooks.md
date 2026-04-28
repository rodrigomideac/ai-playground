---
paths:
  - "packer/run-provision.sh"
  - "packer/default-provision/**"
  - "packer/custom-provision/**"
---

# Packer provisioning hooks

The Packer build runs a numbered hook chain to install packages, set
up shells, and otherwise customize the golden image before it's
captured. This rule covers the hook contract and how
`packer/run-provision.sh` discovers, orders, and executes the
scripts. Auto-loaded when editing the runner or any script under
`packer/{default,custom}-provision/`.

## Directory layout

```
packer/
  run-provision.sh          # runner; uploaded + executed by Packer
  default-provision/        # built-in scripts (tracked in git)
    00-base-packages.sh
    10-shell-config.sh
    20-claude-code.sh
    30-docker.sh
  custom-provision/         # user scripts (gitignored except .gitkeep)
```

`run-provision.sh` itself is uploaded into the build VM at `/tmp` by
Packer's file provisioner; both `default-provision/` and
`custom-provision/` are uploaded next to it. The shell provisioner
then runs `/tmp/run-provision.sh /tmp` inside the VM.

## Naming convention

Every script must match `<NN>-<name>.sh`:

- **`NN`**: two-digit numeric prefix that controls execution order.
- **`name`**: descriptive, dash-separated.
- **`.sh`**: required extension.

Default scripts use gaps of 10 (`00`, `10`, `20`, `30`) so users can
slot custom scripts at any position without renumbering.

## Execution order and override rule

`run-provision.sh` does this on each run:

1. Discovers every `*.sh` in `default-provision/` and
   `custom-provision/`.
2. Extracts each script's numeric prefix.
3. **If a custom script shares a prefix with a default, the custom
   one replaces the default.** Same number = custom wins.
4. Sorts the surviving scripts by prefix and executes in order.
5. Bails on the first non-zero exit (`set -euo pipefail`).

The runner prints an execution plan before running anything and logs
each override decision:

```
============================================
 Execution Plan (4 scripts)
============================================
  [00] /tmp/default-provision/00-base-packages.sh (default)
  [10] /tmp/default-provision/10-shell-config.sh (default)
  [20] /tmp/custom-provision/20-my-editor.sh (custom)
  [30] /tmp/default-provision/30-docker.sh (default)
```

```
[provision] OVERRIDE: 20 — replacing default (...) with custom (...)
```

## Common patterns

**Insert a step between two defaults** (here at `25`, between Claude
Code and Docker):

```bash
# packer/custom-provision/25-my-tools.sh
#!/bin/bash
set -euo pipefail
sudo apt-get install -y ripgrep fd-find
```

Resulting order: `00` → `10` → `20` → `25` → `30`.

**Replace a default** by reusing its prefix:

```bash
# packer/custom-provision/30-podman.sh
#!/bin/bash
set -euo pipefail
sudo apt-get install -y podman
```

Replaces `default-provision/30-docker.sh` because both have prefix
`30`.

**Skip a default** with an empty override:

```bash
# packer/custom-provision/30-skip-docker.sh
#!/bin/bash
echo "Skipping Docker installation"
```

## Script writing rules

- Always start with `#!/bin/bash` and `set -euo pipefail`. The runner
  doesn't isolate failures — first non-zero exit aborts the build.
- Use `sudo` for anything needing root. Packer connects as a regular
  user (the build-time `debian` user, which is removed at the end of
  the build — don't bake state into `/home/debian`).
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

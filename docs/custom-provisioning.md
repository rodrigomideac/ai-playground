# Custom Provisioning

## Overview

The Packer build uses a hook-based provisioning system. Scripts in `packer/default-provision/` are executed during the build, and users can override or extend them by placing scripts in `packer/custom-provision/`.

## Directory Structure

```
packer/
  run-provision.sh          # Runner script (executed by Packer)
  default-provision/        # Built-in scripts (tracked in git)
    00-base-packages.sh
    10-shell-config.sh
    20-claude-code.sh
    30-docker.sh
  custom-provision/         # User scripts (gitignored)
    .gitkeep
```

## Naming Convention

Scripts must follow this pattern: `<NN>-<name>.sh`

- **Numeric prefix** (`NN`): Two-digit zero-padded number that controls execution order
- **Name**: Descriptive name separated by a dash
- **Extension**: Must be `.sh`

Default scripts use gaps of 10 (00, 10, 20, 30) so you can insert custom scripts at any position.

## Execution Order

1. The runner discovers all `.sh` files in both `default-provision/` and `custom-provision/`
2. It extracts the numeric prefix from each filename
3. If a custom script has the **same prefix** as a default script, the custom script **replaces** it
4. All scripts are sorted by prefix and executed in order
5. If any script fails, the build stops immediately (`set -euo pipefail`)

## Precedence Rule

**Same numeric prefix = custom wins.** This lets you replace any default script without modifying tracked files.

### Examples

**Adding a tool at position 25 (between Claude Code and Docker):**

```bash
# custom-provision/25-my-tools.sh
#!/bin/bash
set -euo pipefail
sudo apt-get install -y ripgrep fd-find
```

Execution order: `00` (base) -> `10` (shell) -> `20` (claude) -> `25` (my-tools) -> `30` (docker)

**Replacing Docker with Podman:**

```bash
# custom-provision/30-podman.sh
#!/bin/bash
set -euo pipefail
sudo apt-get install -y podman
```

This replaces `default-provision/30-docker.sh` because both have prefix `30`.

**Replacing Claude Code with a different editor setup:**

```bash
# custom-provision/20-my-editor.sh
#!/bin/bash
set -euo pipefail
# Install your preferred editor/agent instead of Claude Code
curl -fsSL https://example.com/install.sh | bash
```

This replaces `default-provision/20-claude-code.sh` because both have prefix `20`.

**Skipping a default step (empty override):**

```bash
# custom-provision/30-skip-docker.sh
#!/bin/bash
echo "Skipping Docker installation"
```

## Script Writing Guidelines

- Always start with `#!/bin/bash` and `set -euo pipefail`
- Use `sudo` for commands that need root (Packer connects as a regular user)
- Use `echo "==> Description"` to log what the script is doing
- Don't use `source ~/.bashrc` — it's fragile in non-interactive shells and not needed
- Append environment variables to `~/.bashrc` with `echo 'export ...' >> ~/.bashrc`

## Debugging

The runner prints an execution plan before running any scripts:

```
============================================
 Execution Plan (4 scripts)
============================================
  [00] /tmp/default-provision/00-base-packages.sh (default)
  [10] /tmp/default-provision/10-shell-config.sh (default)
  [20] /tmp/custom-provision/20-my-editor.sh (custom)
  [30] /tmp/default-provision/30-docker.sh (default)
```

Override decisions are logged:

```
[provision] OVERRIDE: 20 — replacing default (...) with custom (...)
```

If the build fails, check the Packer output to see which script and which command failed.

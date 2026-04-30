# Customization

Extending the golden image with your own setup scripts and per-worker
files, plus how to drive the build headlessly from CI.

## Where to edit

After `ai-playground init` (or the first `ai-playground build`) the build inputs live under `$XDG_CONFIG_HOME/ai-playground/build/`:

- `provision/` — numbered shell scripts (`NN-name.sh`) executed in order during the Packer build. Edit, add, or delete files freely; re-run `ai-playground build` to apply. Default scripts ship with prefixes `00` (base packages), `10` (oh-my-zsh + bashrc), `20` (Claude Code CLI), `30` (Docker rootless).
- `chroot/etc/skel/` — files placed here land in each worker's home directory at user-creation time (root-owned in the image; `useradd -m` chowns them to the worker user). This is where to put dotfiles, agent config, etc.
- `template.pkr.hcl`, `run-provision.sh` — generally edit-with-care; you only need to touch them for unusual builds.

## Provision scripts

Naming convention: `NN-name.sh` where `NN` is a two-digit prefix that controls execution order. Defaults use gaps of 10 so you can slot custom scripts at any position without renumbering — drop `25-my-tools.sh` to insert between `20` and `30`.

A `# Description: ...` header comment near the top of the script is shown in `ai-playground init`'s per-script approval prompt:

```bash
#!/bin/bash
set -euo pipefail

# Description: My extra tooling
sudo apt-get install -y ripgrep fd-find
```

Always start scripts with `#!/bin/bash` and `set -euo pipefail`. The runner doesn't isolate failures — first non-zero exit aborts the build.

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

In headless mode the worker-conflict check (see [USAGE.md > Re-running build](USAGE.md#re-running-build)) refuses the build instead of prompting — clean up workers first with `ai-playground shutdown-worker`.

## Next steps

- [USAGE.md](USAGE.md) — daily command reference
- [DEVELOPMENT.md](DEVELOPMENT.md) — running the CLI against a local checkout

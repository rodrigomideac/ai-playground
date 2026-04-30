# Development

For working on `ai-playground` itself — running the binary against a
local checkout instead of the public repo cache, and where to find each
piece of the codebase.

## Use a local repo as the source tree

By default `ai-playground init` and `ai-playground build` clone (or refresh) `https://github.com/rodrigomideac/ai-playground` into `$XDG_CACHE_HOME/ai-playground/repo/` and read the `packer/` and `chroot/` trees from there. To work against a local checkout instead:

```bash
git clone https://github.com/rodrigomideac/ai-playground ~/src/ai-playground
cd ~/src/ai-playground
make build-cli
AI_PLAYGROUND_REPO=$PWD ./cli/bin/ai-playground init
AI_PLAYGROUND_REPO=$PWD ./cli/bin/ai-playground build
```

`--repo-path /path/to/repo` is the per-invocation form of the same override; the flag wins when both are set. When either is active the repo cache is left untouched — switching back to the default later resumes from that cache.

## Project layout

```
packer/                 Packer template + scripts (the source for build/)
  provision/              numbered provisioning scripts run during the build
  seed/                   in-repo seed (only used when running packer by hand)
chroot/etc/skel/        files copied into each worker's home at user creation
cli/                    Go module
  cmd/ai-playground/      CLI entrypoint (init, build, reset, doctor, add-worker, ...)
  internal/buildflow/     populate + drift detection
  internal/config/        config.yaml load/save/validate
  internal/doctor/        host-environment checks (precise register)
  internal/paths/         XDG path resolution + distro detection
  internal/promptio/      interactive yes/no + line prompts (TTY-aware)
  internal/repo/          public-repo clone/fetch + override
  internal/seed/          build-only SSH key + user-data render
  internal/ui/            colored Step/Done/Warn/Notice helpers
  internal/worker/        Manager, Worker, NoCloud seed builder
scripts/                Repo-development helpers (lint)
tests/                  bats end-to-end test suite
.claude/rules/          Auto-loaded Claude docs rules (one per subsystem)
```

## Auto-loaded subsystem rules

Subsystem documentation for Claude lives under `.claude/rules/docs-*.md` and auto-loads when matching files are read or edited:

- `docs-ai-playground-cli.md` — worker-pool orchestration (add/ssh/shutdown/list, virt-install traps, libvirt pool perms)
- `docs-ai-playground-build.md` — build/init/reset state machine, doctor checks, Packer-template variable contract, pre-build worker check
- `docs-cli-language.md` — two-register language convention (plain for end users, precise for the doctor)
- `docs-provisioning-hooks.md` — provision-script chain (naming, `# Description:` header, runner contract)
- `docs-test-boot.md` — libvirt-bypass smoke-boot recipe for disambiguating image bugs from libvirt bugs

These are conventions for AI-assisted edits; the source of truth for runtime behavior is the code itself.

## Next steps

- [INSTALLATION.md](INSTALLATION.md) — host-side prerequisites
- [USAGE.md](USAGE.md) — daily command reference
- [CUSTOMIZATION.md](CUSTOMIZATION.md) — extending the golden image

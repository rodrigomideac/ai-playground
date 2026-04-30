# Repo-development convenience targets. End users build the golden image
# with `ai-playground build` (after `ai-playground init`) — this Makefile
# only covers tasks that operate on the *repo*, not on the user's
# $XDG_CONFIG_HOME/ai-playground state.

check:
	bash ./scripts/lint-sh.sh
	cd cli && go vet ./...

build-cli:
	cd cli && go build -o bin/ai-playground ./cmd/ai-playground

test:
	@command -v bats >/dev/null 2>&1 || { \
	  echo "error: bats not installed." >&2; \
	  echo "  Manjaro: sudo pacman -S bats" >&2; \
	  echo "  Debian/Ubuntu: sudo apt install bats" >&2; \
	  echo "  Fedora: sudo dnf install bats" >&2; \
	  echo "  macOS: brew install bats-core" >&2; \
	  exit 1; \
	}
	@[ -x cli/bin/ai-playground ] || { echo "error: cli/bin/ai-playground missing — run 'make build-cli' first" >&2; exit 1; }
	@golden="$${XDG_DATA_HOME:-$$HOME/.local/share}/ai-playground/golden/ai-playground-base.qcow2"; \
	[ -f "$$golden" ] || { echo "error: golden image missing at $$golden — run 'ai-playground build' first" >&2; exit 1; }
	bats tests/

.PHONY: check build-cli test

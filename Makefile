check:
	bash ./scripts/lint-sh.sh
	go vet ./...

build-from-base:
	bash ./scripts/check-prerequisites.sh
	bash ./scripts/prepare-packer-seed.sh
	rm -rf build/packer-ai-playground-base
	cd packer && packer init .
	cd packer && ARTIFACT_DIR=../build packer build template.pkr.hcl

build-cli:
	go build -o bin/ai-playground ./cmd/ai-playground

test:
	@command -v bats >/dev/null 2>&1 || { \
	  echo "error: bats not installed." >&2; \
	  echo "  Manjaro: sudo pacman -S bats" >&2; \
	  echo "  Debian/Ubuntu: sudo apt install bats" >&2; \
	  echo "  Fedora: sudo dnf install bats" >&2; \
	  echo "  macOS: brew install bats-core" >&2; \
	  exit 1; \
	}
	@[ -x bin/ai-playground ] || { echo "error: bin/ai-playground missing — run 'make build-cli' first" >&2; exit 1; }
	@[ -f build/packer-ai-playground-base/ai-playground-base ] || { echo "error: golden image missing — run 'make build-from-base' first" >&2; exit 1; }
	bats tests/

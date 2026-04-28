check:
	bash ./scripts/lint-sh.sh
	go vet ./...

build-from-base:
	bash ./scripts/check-prerequisites.sh
	bash ./scripts/prepare-packer-seed.sh
	rm -rf build/packer-ai-playground-base
	cd base-iso/packer && packer init .
	cd base-iso/packer && ARTIFACT_DIR=../../build packer build template.pkr.hcl

build-cli:
	go build -o bin/ai-playground ./cmd/ai-playground

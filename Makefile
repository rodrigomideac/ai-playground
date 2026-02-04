check:
	bash ./scripts/lint-sh.sh

build-from-base:
	bash ./scripts/check-prerequisites.sh
	vagrant destroy -f
	bash ./scripts/remove-all-vms.sh
	bash ./scripts/download-iso.sh
	rm -rf build/packer-ai-playground-base
	cd base-iso/packer && packer init .
	cd base-iso/packer && ARTIFACT_DIR=../../build packer build template.pkr.hcl
	vagrant box add --name debian13 build/debian13.box --force


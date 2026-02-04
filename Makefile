check:
	./scripts/lint-sh.sh

build-from-base:
	vagrant destroy -f
	./scripts/remove-all-vms.sh
	cd base-iso/packer && packer init .
	cd base-iso/packer && ARTIFACT_DIR=../../build packer build template.pkr.hcl
	vagrant box add --name debian13 build/debian13.box --force


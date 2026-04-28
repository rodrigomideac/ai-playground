#cloud-config
# NoCloud seed used only during the Packer build. scripts/prepare-packer-seed.sh
# renders this template and substitutes the placeholder below with the build-
# time ed25519 public key before the ISO is assembled.
users:
  - name: debian
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    lock_passwd: true
    ssh_authorized_keys:
      - __SSH_PUBKEY__
ssh_pwauth: false
package_update: false

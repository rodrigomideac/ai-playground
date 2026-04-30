#cloud-config
# NoCloud seed used only during the Packer build. `ai-playground build`
# renders this template by substituting the placeholder below with the
# build-time ed25519 public key before Packer mounts it as the cidata ISO.
users:
  - name: debian
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    lock_passwd: true
    ssh_authorized_keys:
      - __SSH_PUBKEY__
ssh_pwauth: false
package_update: false

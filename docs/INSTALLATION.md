# Installation

Getting `ai-playground` working on a fresh host. Once you've finished
this page, head to [USAGE.md](USAGE.md) to build the golden image and
spin up your first worker.

## Prerequisites

You need: KVM/QEMU, libvirt, Packer, Go (to build the CLI), xorriso, git, ssh-keygen, and bats (for tests). `ai-playground doctor` will run a full host-environment check after install and name anything missing — see the [`doctor` section in USAGE.md](USAGE.md#diagnostics).

<details>
<summary><b>Manjaro / Arch</b></summary>

```bash
sudo pacman -S qemu-desktop libvirt virt-install bridge-utils \
               packer go libisoburn bats
sudo systemctl enable --now libvirtd
sudo usermod -aG kvm,libvirt "$USER"
# log out / back in for the new groups to take effect
```

</details>

<details>
<summary><b>Debian / Ubuntu</b></summary>

```bash
sudo apt install qemu-system-x86 libvirt-daemon-system virtinst \
                 bridge-utils packer golang xorriso bats
sudo systemctl enable --now libvirtd
sudo usermod -aG kvm,libvirt "$USER"
```

</details>

<details>
<summary><b>Fedora</b></summary>

```bash
sudo dnf install @virtualization libvirt virt-install bridge-utils \
                 packer golang xorriso bats
sudo systemctl enable --now libvirtd
sudo usermod -aG kvm,libvirt "$USER"
```

</details>

## One-time host fix

So the CLI can write VM disks without sudo:

```bash
sudo chgrp libvirt /var/lib/libvirt/images
sudo chmod g+rwxs  /var/lib/libvirt/images
```

(The setgid bit makes new files inherit the `libvirt` group, which plays nicely with libvirt's `dynamic_ownership`.)

## Build the CLI

```bash
make build-cli
sudo install -m 0755 cli/bin/ai-playground /usr/local/bin/   # optional: put on $PATH
```

That's it — you should now be able to run `ai-playground --help`.

## Verify the host is ready

```bash
ai-playground doctor
```

Prints a verbose KVM/qemu/libvirt stack diagnostic, grouped by layer (CPU virtualization → KVM kernel module → qemu → libvirtd → libvirt clients → Packer → host tooling). Anything that needs fixing prints the literal command(s) to run. Exit code is non-zero if any check fails.

## Next steps

- [USAGE.md](USAGE.md) — first build + daily commands
- [CUSTOMIZATION.md](CUSTOMIZATION.md) — extending the golden image with your own setup scripts
- [DEVELOPMENT.md](DEVELOPMENT.md) — local development of `ai-playground` itself

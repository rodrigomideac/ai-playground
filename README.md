# ai-playground

Builds a Vagrant box from a Debian 13 ISO file to be used as sandbox for coding agents.

# Getting Started

- Make sure you have VirtualBox, Packer, and Vagrant. 

<details>
<summary>Installing VirtualBox</summary>

- **Arch Linux:** `sudo pacman -S virtualbox virtualbox-host-modules-arch`
- **Debian/Ubuntu:** Download from [virtualbox.org/wiki/Linux_Downloads](https://www.virtualbox.org/wiki/Linux_Downloads) or:
  ```bash
  sudo apt install virtualbox
  ```
- **Fedora:** Download from [virtualbox.org/wiki/Linux_Downloads](https://www.virtualbox.org/wiki/Linux_Downloads) or:
  ```bash
  sudo dnf install VirtualBox
  ```
- **macOS:** Download from [virtualbox.org/wiki/Downloads](https://www.virtualbox.org/wiki/Downloads) or:
  ```bash
  brew install --cask virtualbox
  ```

</details>

<details>
<summary>Installing Packer</summary>

- **Arch Linux:** `sudo pacman -S packer`
- **Debian/Ubuntu:**
  ```bash
  wget -O - https://apt.releases.hashicorp.com/gpg | sudo gpg --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg
  echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/hashicorp.list
  sudo apt update && sudo apt install packer
  ```
- **Fedora:**
  ```bash
  sudo dnf install -y dnf-plugins-core
  sudo dnf config-manager --add-repo https://rpm.releases.hashicorp.com/fedora/hashicorp.repo
  sudo dnf install packer
  ```
- **macOS:**
  ```bash
  brew install packer
  ```

See [developer.hashicorp.com/packer/install](https://developer.hashicorp.com/packer/install) for more options.

</details>

<details>
<summary>Installing Vagrant</summary>

- **Arch Linux:** `sudo pacman -S vagrant`
- **Debian/Ubuntu:**
  ```bash
  wget -O - https://apt.releases.hashicorp.com/gpg | sudo gpg --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg
  echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/hashicorp.list
  sudo apt update && sudo apt install vagrant
  ```
- **Fedora:**
  ```bash
  sudo dnf install -y dnf-plugins-core
  sudo dnf config-manager --add-repo https://rpm.releases.hashicorp.com/fedora/hashicorp.repo
  sudo dnf install vagrant
  ```
- **macOS:**
  ```bash
  brew install vagrant
  ```

See [developer.hashicorp.com/vagrant/install](https://developer.hashicorp.com/vagrant/install) for more options.

</details>

- Clone this repo
- Run `make download-iso` to download the netinst Debian 13 ISO.
- Run `make build-from-base` to build the Vagrant box.

# Why
Using coding agents without supervision provides huge productivity, but it is also a huge security risk.

There are a bunch of ways for sandboxing coding agents, such as running them in Docker containers, controlling ACLs, and others.

Despite all of that, the approach used here is to use a virtual machine:
- Isolation from the host: only the project folder is shared.
- It has its own kernel.
- There are CVEs that show it is possible for a guest VM to escape and affect the host. But, in my opinion, its risk is very low considered other options.


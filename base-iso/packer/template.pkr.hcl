# Packer template for creating launcher-test Vagrant box
# Boots custom-launcher.iso and runs automated installation

packer {
  required_plugins {
    virtualbox = {
      version = ">= 1.0.0"
      source  = "github.com/hashicorp/virtualbox"
    }
    vagrant = {
      version = ">= 1.1.0"
      source  = "github.com/hashicorp/vagrant"
    }
  }
}

# Variables
variable "artifact_dir" {
  type        = string
  default     = "${env("ARTIFACT_DIR")}"
  description = "Output directory for artifacts (overridable via ARTIFACT_DIR env var)"
}

variable "boot_wait" {
  type        = string
  default     = "${env("PACKER_BOOT_WAIT")}"
  description = "Time to wait before sending boot command (overridable via PACKER_BOOT_WAIT env var)"
}

variable "iso_path" {
  type        = string
  default     = "../debian-13.3.0-amd64-netinst.iso"
  description = "Path to custom launcher ISO"
}

variable "vm_name" {
  type        = string
  default     = "ai-playground-base"
  description = "Name of the VM"
}

# Local variables (computed)
locals {
  output_dir  = var.artifact_dir
  iso_file    = var.iso_path
  boot_wait_t = var.boot_wait != "" ? var.boot_wait : "10s"
  timestamp   = formatdate("YYYYMMDD-hhmm", timestamp())
}

# Source: VirtualBox ISO builder
source "virtualbox-iso" "debian13" {
  headless             = true
  # VM settings
  vm_name              = var.vm_name
  guest_os_type        = "Debian_64"
  cpus                 = 2
  memory               = 4096
  disk_size            = 20480 # 20GB
  hard_drive_interface = "sata"
  iso_interface        = "sata"

  # ISO settings
  iso_url      = local.iso_file
  iso_checksum = "c9f09d24b7e834e6834f2ffa565b33d6f1f540d04bd25c79ad9953bc79a8ac02" 

  # Boot settings - trigger preseed installation with GRUB EFI
  # Menu: Live system (default) -> Live fail-safe -> Start Installer <- we want this
  # Editor: setparams line -> linux line -> initrd line
  boot_wait = local.boot_wait_t
  boot_command = [
    "<down><wait>",                    # Navigate to "Start Installer"
    "e<wait>",                               # Edit boot entry
    "<down><down><down><end><wait>",                     # Move to linux line and go to end
    " auto=true priority=critical url=http://{{ .HTTPIP }}:{{ .HTTPPort }}/preseed.cfg<wait>",
    "<f10><wait>"                            # Boot with F10
  ]

  # HTTP server for preseed file
  http_directory = "${path.root}"
  http_port_min  = 8100
  http_port_max  = 8200

  # SSH settings - wait for installation to complete
  ssh_username           = "rodrigo"
  ssh_password           = "rodrigo"
  ssh_timeout            = "6h"
  ssh_handshake_attempts = 100

  # Shutdown settings
  shutdown_command = "echo 'rodrigo' | sudo -S shutdown -P now"
  shutdown_timeout = "5m"

  # VirtualBox settings
  vboxmanage = [
    ["modifyvm", "{{ .Name }}", "--firmware", "efi"],
    ["modifyvm", "{{ .Name }}", "--nat-localhostreachable1", "on"],
    ["modifyvm", "{{ .Name }}", "--graphicscontroller", "vmsvga"],
    ["modifyvm", "{{ .Name }}", "--vram", "128"]
  ]

  # Output settings
  format           = "ova"
  output_directory = "${local.output_dir}/packer-${var.vm_name}"
}

# Build configuration
build {
  sources = ["source.virtualbox-iso.debian13"]

  # Provisioner: Install dependencies before boxing
  provisioner "shell" {
    script = "${path.root}/dependencies.sh"
  }

  # Provisioner: Apply chroot overlay
  provisioner "file" {
    source      = "${path.root}/../../chroot"
    destination = "/tmp"
  }

  provisioner "file" {
    source      = "${path.root}/../../scripts/provision-chroot.sh"
    destination = "/tmp/provision-chroot.sh"
  }

  provisioner "shell" {
    inline = [
      "chmod +x /tmp/provision-chroot.sh",
      "/tmp/provision-chroot.sh /tmp/chroot",
      "rm -rf /tmp/chroot /tmp/provision-chroot.sh"
    ]
  }

  # Post-processor: Convert to Vagrant box
  post-processor "vagrant" {
    output              = "${local.output_dir}/debian13.box"
    compression_level   = 6
    keep_input_artifact = false
    provider_override   = "virtualbox"
  }
}

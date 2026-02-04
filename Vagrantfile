# -*- mode: ruby -*-
# vi: set ft=ruby :

Vagrant.configure("2") do |config|
  # Use our custom launcher-test box
  config.vm.box = "debian13"

  # Don't check for box updates (local custom box)
  config.vm.box_check_update = false

  # SSH as rodrigo (custom box has no vagrant user)
  config.ssh.username = "rodrigo"
  config.ssh.password = "rodrigo"

  # VM hostname
  config.vm.hostname = "ai-playground"

  # Port forwarding for SSH
  config.vm.network "forwarded_port", guest: 22, host: 2222, id: "ssh", auto_correct: true

  # VM provider settings
  config.vm.provider "virtualbox" do |vb|
    vb.name = "ai-playground"
    vb.memory = "4096"  # 4GB RAM
    vb.cpus = 4
    vb.gui = false  # Headless (use VNC to see desktop)

    # Graphics settings for X11
    vb.customize ["modifyvm", :id, "--graphicscontroller", "vmsvga"]
    vb.customize ["modifyvm", :id, "--vram", "128"]
    vb.customize ["modifyvm", :id, "--accelerate3d", "on"]
  end

  # Provision: Apply chroot overlay
  config.vm.provision "file",
    source: "chroot/",
    destination: "/tmp/chroot"

  config.vm.provision "file",
    source: "scripts/provision-chroot.sh",
    destination: "/tmp/provision-chroot.sh"

  config.vm.provision "shell", inline: <<-SHELL
    chmod +x /tmp/provision-chroot.sh
    /tmp/provision-chroot.sh /tmp/chroot
    rm -rf /tmp/chroot /tmp/provision-chroot.sh
  SHELL
end

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

  # Provision: Display connection info after boot
  # config.vm.provision "shell", inline: <<-SHELL
  #   echo ""
  #   echo "===================================="
  #   echo "Launcher Test VM is ready!"
  #   echo "===================================="
  #   echo ""
  #   echo "Connection Info:"
  #   echo "  SSH:   ssh rodrigo@localhost -p 2222 (password: rodrigo)"
  #   echo "===================================="
  # SHELL
  #
  # # Run only on first provision
  # config.vm.provision "shell", run: "once", inline: <<-SHELL
  #   # Ensure photos directory exists
  #   mkdir -p /home/launcher/photos
  #   chown launcher:launcher /home/launcher/photos
  #
  #   # Ensure X can start on next boot
  #   systemctl enable getty@tty1
  # SHELL
end

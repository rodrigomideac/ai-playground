#!/bin/bash
set -euo pipefail

echo "==> Installing Docker"
curl -fsSL https://get.docker.com -o /tmp/get-docker.sh
sudo sh /tmp/get-docker.sh
rm -f /tmp/get-docker.sh

echo "==> Configuring rootless Docker"
sudo modprobe tun
sudo apt-get install -y uidmap
dockerd-rootless-setuptool.sh install

echo "==> Setting Docker environment"
# shellcheck disable=SC2016
echo 'export PATH=/usr/bin:$PATH' >> ~/.bashrc
# shellcheck disable=SC2016
echo 'export DOCKER_HOST=unix:///run/user/1000/docker.sock' >> ~/.bashrc

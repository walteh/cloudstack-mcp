#!/bin/sh

apk add gcc linux-headers py3-pip musl-dev python3-dev e2fsprogs e2fsprogs-extra cloud-init \
	libblockdev lsblk parted sfdisk sgdisk lvm2 device-mapper \
	doas eudev mount openssh-server-pam sudo

# sed -i '/^.*?UsePAM *=.*?$/d' /etc/ssh/sshd_config  # Erase any lines in the config that match the pattern, just to make sure. It was commented out in my install by default. I didn't run this.
echo "UsePAM yes" >>/etc/ssh/sshd_config

pip install pyserial netifaces

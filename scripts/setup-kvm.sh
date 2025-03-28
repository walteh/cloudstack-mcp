#!/usr/bin/env bash

set -e
set -x

# Based on https://rohityadav.cloud/blog/cloudstack-rpi4-kvm/
# Adapted for Mac M1/M2 with Ubuntu Asahi

# Check if running as root
if [ "$(id -u)" -ne 0 ]; then
	echo "This script must be run as root"
	exit 1
fi

# Create directories
CLOUDSTACK_DIR="/home/$SUDO_USER/cloudstack"
SECONDARY_STORAGE="$CLOUDSTACK_DIR/secondary"
PRIMARY_STORAGE="$CLOUDSTACK_DIR/primary"

mkdir -p "$CLOUDSTACK_DIR"
mkdir -p "$SECONDARY_STORAGE"
mkdir -p "$PRIMARY_STORAGE"

chown -R "$SUDO_USER:$SUDO_USER" "$CLOUDSTACK_DIR"

# Install prerequisites
apt update
apt install -y qemu-kvm libvirt-daemon-system bridge-utils nfs-kernel-server mysql-server

# Configure NFS
echo "$PRIMARY_STORAGE *(rw,async,no_root_squash)" >>/etc/exports
echo "$SECONDARY_STORAGE *(rw,async,no_root_squash)" >>/etc/exports
systemctl restart nfs-kernel-server

# Download SystemVM Template (ARM64 version)
cd "$SECONDARY_STORAGE"
mkdir -p template/tmpl/1/1

# Download latest ARM64 SystemVM Template from 4.20 release
TEMPLATE_URL="https://download.cloudstack.org/arm64/systemvmtemplate/4.20/systemvmtemplate-4.20.0.0-kvm-arm64.qcow2.bz2"
wget -O template/tmpl/1/1/template.bz2 "$TEMPLATE_URL"
bunzip2 template/tmpl/1/1/template.bz2

# Set CPU speed manually for CloudStack agent (Asahi Linux issue)
if [ -f /etc/cloudstack/agent/agent.properties ]; then
	echo "Setting CPU speed in CloudStack agent properties..."
	echo "host.cpu.speed=2400" >>/etc/cloudstack/agent/agent.properties
fi

# Install CloudStack Management Server and Agent
apt install -y software-properties-common
add-apt-repository -y ppa:cloudstack/4.20
apt update
apt install -y cloudstack-management cloudstack-agent

# Configure MySQL
mysql -e "DROP DATABASE IF EXISTS cloud"
mysql -e "DROP DATABASE IF EXISTS cloud_usage"
mysql -e "CREATE DATABASE cloud"
mysql -e "CREATE DATABASE cloud_usage"
mysql -e "GRANT ALL ON cloud.* TO 'cloud'@'localhost' IDENTIFIED BY 'cloud'"
mysql -e "GRANT ALL ON cloud_usage.* TO 'cloud'@'localhost'"
mysql -e "GRANT PROCESS ON *.* TO 'cloud'@'localhost'"
mysql -e "GRANT ALL ON cloud.* TO 'cloud'@'%' IDENTIFIED BY 'cloud'"
mysql -e "GRANT ALL ON cloud_usage.* TO 'cloud'@'%'"
mysql -e "GRANT PROCESS ON *.* TO 'cloud'@'%'"

# Configure CloudStack DB
cloudstack-setup-databases cloud:cloud@localhost --deploy-as=root

# Initialize CloudStack
cloudstack-setup-management

# Configure libvirt
systemctl stop libvirtd
sed -i 's/^#listen_tls = 0/listen_tls = 0/' /etc/libvirt/libvirtd.conf
sed -i 's/^#listen_tcp = 1/listen_tcp = 1/' /etc/libvirt/libvirtd.conf
sed -i 's/^#tcp_port = "16509"/tcp_port = "16509"/' /etc/libvirt/libvirtd.conf
sed -i 's/^#unix_sock_group = "libvirt"/unix_sock_group = "libvirt"/' /etc/libvirt/libvirtd.conf
sed -i 's/^#unix_sock_rw_perms = "0770"/unix_sock_rw_perms = "0770"/' /etc/libvirt/libvirtd.conf
sed -i 's/^#auth_tcp = "sasl"/auth_tcp = "none"/' /etc/libvirt/libvirtd.conf

# Add libvirtd to systemd
cat >/etc/systemd/system/libvirtd.service <<EOF
[Unit]
Description=Virtualization daemon
Requires=virtlogd.socket
Requires=virtlockd.socket
Before=libvirt-guests.service
After=network.target
After=dbus.service
After=apparmor.service
After=local-fs.target
Documentation=man:libvirtd(8)
Documentation=https://libvirt.org

[Service]
Type=notify
ExecStart=/usr/sbin/libvirtd --daemon --listen
ExecStartPost=/usr/bin/bash -c "/usr/sbin/iptables -I INPUT -p tcp --dport 16509 -j ACCEPT"
ExecStartPost=/usr/bin/bash -c "/usr/sbin/ip6tables -I INPUT -p tcp --dport 16509 -j ACCEPT"
Restart=on-failure
KillMode=process
EnvironmentFile=-/etc/sysconfig/libvirtd
CapabilityBoundingSet=CAP_AUDIT_CONTROL CAP_AUDIT_READ CAP_AUDIT_WRITE CAP_BLOCK_SUSPEND CAP_CHOWN CAP_CHOWN CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH CAP_FOWNER CAP_FSETID CAP_IPC_LOCK CAP_KILL CAP_LEASE CAP_LINUX_IMMUTABLE CAP_MAC_ADMIN CAP_MAC_OVERRIDE CAP_MKNOD CAP_NET_ADMIN CAP_NET_BIND_SERVICE CAP_NET_BROADCAST CAP_NET_RAW CAP_SETGID CAP_SETPCAP CAP_SETUID CAP_SYS_ADMIN CAP_SYS_BOOT CAP_SYS_CHROOT CAP_SYS_MODULE CAP_SYS_NICE CAP_SYS_PACCT CAP_SYS_PTRACE CAP_SYS_RAWIO CAP_SYS_RESOURCE CAP_SYS_TIME CAP_SYS_TTY_CONFIG CAP_SYSLOG CAP_WAKE_ALARM
LimitNOFILE=1048576
LimitNPROC=1048576
LimitCORE=infinity
TasksMax=infinity

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl start libvirtd
systemctl enable libvirtd

# Configure CloudStack Agent
sed -i 's/^# hypervisor=kvm/hypervisor=kvm/' /etc/cloudstack/agent/agent.properties
systemctl restart cloudstack-agent

echo "===================="
echo "Setup complete! You can now access CloudStack at http://localhost:8080/client"
echo "Default credentials: admin / password"
echo "===================="

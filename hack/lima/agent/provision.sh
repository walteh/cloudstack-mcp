#!/bin/bash

b=CIDATA
hosthome_mountpoint=LIMA_${b}_HOSTHOME_MOUNTPOINT
echo "hosthome_mountpoint: ${!hosthome_mountpoint}"
hosthome_mountpoint=${!hosthome_mountpoint}

exec >>"$hosthome_mountpoint/.lima/{{.Name}}/provision.log" 2>&1

cleanup() {
	cp /var/log/cloud-init-output.log "$hosthome_mountpoint/.lima/{{.Name}}/cloud-init-output.log"
}

trap cleanup EXIT INT TERM

echo "hosthome_mountpoint: $hosthome_mountpoint"
#   echo "all vars: {{.}}"
echo "AdditionalDisks: {{.AdditionalDisks}}"
echo "Arch: {{.Arch}}"
echo "CPUType: {{.CPUType}}"
echo "CPUs: {{.CPUs}}"
echo "Config: {{.Config}}"
echo "Dir: {{.Dir}}"
echo "Disk: {{.Disk}}"
echo "DriverPID: {{.DriverPID}}"
echo "Errors: {{.Errors}}"
echo "HostAgentPID: {{.HostAgentPID}}"
echo "HostArch: {{.HostArch}}"
echo "HostOS: {{.HostOS}}"
echo "Hostname: {{.Hostname}}"
echo "IdentityFile: {{.IdentityFile}}"
echo "LimaHome: {{.LimaHome}}"
echo "LimaVersion: {{.LimaVersion}}"
echo "Memory: {{.Memory}}"
echo "Message: {{.Message}}"
echo "Name: {{.Name}}"
echo "Networks: {{.Networks}}"
echo "Param: {{.Param}}"
echo "Protected: {{.Protected}}"
echo "SSHAddress: {{.SSHAddress}}"
echo "SSHConfigFile: {{.SSHConfigFile}}"
echo "SSHLocalPort: {{.SSHLocalPort}}"
echo "Status: {{.Status}}"
echo "VMType: {{.VMType}}"

export DEBIAN_FRONTEND=noninteractive

echo "=== Installing basic packages ==="
apt-get update
apt-get install -y openntpd openssh-server sudo vim htop tar

# Get IP address dynamically
IP_ADDRESS=$(ip addr show eth0 | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -1)
echo "Using IP address: $IP_ADDRESS"

# === Repository Setup ===
echo "=== Setting up CloudStack repository ==="
mkdir -p /etc/apt/keyrings
wget -O- http://packages.shapeblue.com/release.asc | gpg --dearmor | tee /etc/apt/keyrings/cloudstack.gpg >/dev/null
echo deb [signed-by=/etc/apt/keyrings/cloudstack.gpg] http://packages.shapeblue.com/cloudstack/upstream/debian/4.20 / >/etc/apt/sources.list.d/cloudstack.list
apt-get update -y

# === Storage Setup ===
echo "=== Setting up NFS storage ==="
apt-get install -y nfs-kernel-server quota

# Configure exports
echo "/export  *(rw,async,no_root_squash,no_subtree_check)" >/etc/exports
mkdir -p /export/primary /export/secondary
exportfs -a

# Configure NFS
sed -i -e 's/^RPCMOUNTDOPTS="--manage-gids"$/RPCMOUNTDOPTS="-p 892 --manage-gids"/g' /etc/default/nfs-kernel-server 2>/dev/null || true
sed -i -e 's/^STATDOPTS=$/STATDOPTS="--port 662 --outgoing-port 2020"/g' /etc/default/nfs-common 2>/dev/null || true
echo "NEED_STATD=yes" >>/etc/default/nfs-common 2>/dev/null || true
sed -i -e 's/^RPCRQUOTADOPTS=$/RPCRQUOTADOPTS="-p 875"/g' /etc/default/quota 2>/dev/null || true
service nfs-kernel-server restart

# === KVM Host Setup ===
echo "=== Setting up KVM host ==="
apt-get install -y qemu-kvm libvirt-daemon-system libvirt-clients bridge-utils

tee /etc/netplan/02-cloudstack-bridge.yaml <<<"
          network:
              version: 2
              bridges:
                cloudbr0:
                    addresses: [192.168.122.100/24]
                    interfaces: []
                    parameters:
                        stp: false
                        forward-delay: 0
          "

netplan generate
netplan apply

# Install CloudStack agent
apt-get install -y cloudstack-agent

# Configure libvirt and qemu
sed -i -e 's/\#vnc_listen.*$/vnc_listen = "0.0.0.0"/g' /etc/libvirt/qemu.conf

# For Ubuntu 22.04
echo 'LIBVIRTD_ARGS="--listen"' >>/etc/default/libvirtd

# Mask socket-based activation
systemctl mask libvirtd.socket libvirtd-ro.socket libvirtd-admin.socket libvirtd-tls.socket libvirtd-tcp.socket

# Configure legacy mode
echo 'remote_mode="legacy"' >>/etc/libvirt/libvirt.conf

# Configure libvirtd
tee /etc/libvirt/libvirtd.conf <<<'
          listen_tls=0
          listen_tcp=1
          tcp_port = "16509"
          mdns_adv = 0
          auth_tcp = "none"
          '

# Configure default network for libvirt (if needed)
tee /etc/libvirt/qemu/networks/default.xml <<<'
          <network>
            <name>default</name>
            <bridge name="virbr0"/>
            <forward/>
            <ip address="192.168.122.1" netmask="255.255.255.0">
              <dhcp>
                <range start="192.168.122.2" end="192.168.122.254"/>
              </dhcp>
            </ip>
          </network>
          '

# Restart libvirt
systemctl restart libvirtd

# Enable default network
virsh net-define /etc/libvirt/qemu/networks/default.xml
virsh net-autostart default
virsh net-start default

# Disable apparmor on libvirtd
ln -s /etc/apparmor.d/usr.sbin.libvirtd /etc/apparmor.d/disable/ 2>/dev/null || true
ln -s /etc/apparmor.d/usr.lib.libvirt.virt-aa-helper /etc/apparmor.d/disable/ 2>/dev/null || true
apparmor_parser -R /etc/apparmor.d/usr.sbin.libvirtd 2>/dev/null || true
apparmor_parser -R /etc/apparmor.d/usr.lib.libvirt.virt-aa-helper 2>/dev/null || true

# Get current IP
IP_ADDRESS=$(ip addr show eth0 | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -1)
BRIDGE_IP=$(ip addr show cloudbr0 | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -1)

# Prepare CloudStack agent configuration
mkdir -p /etc/cloudstack/agent

# Create agent properties template (needs to be filled with management server details)
tee /etc/cloudstack/agent/agent.properties.template <<<"
            # CloudStack Agent Configuration Template
            # Replace these values with your actual CloudStack management server details

            # Management Server
            host=MANAGEMENT_SERVER_IP
            port=8250

            # Zone/Pod/Cluster identification
            zone=ZONE_ID
            pod=POD_ID  
            cluster=CLUSTER_ID
            guid=$(uuidgen)

            # Local Storage
            local.storage.uuid=$(uuidgen)

            # This VM's IP is: $IP_ADDRESS
            # Bridge IP is: $BRIDGE_IP
          "

echo "=== Agent Setup Instructions ==="
echo "CloudStack agent is now set up."
echo "To connect to your management server, edit /etc/cloudstack/agent/agent.properties with your management server details."
echo "Copy the template and update it:"
echo "cp /etc/cloudstack/agent/agent.properties.template /etc/cloudstack/agent/agent.properties"
echo "Then restart the agent: systemctl restart cloudstack-agent"

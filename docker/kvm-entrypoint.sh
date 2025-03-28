#!/bin/bash
set -e

echo "Starting KVM host for CloudStack..."

# Debug - check the environment
echo "Checking environment..."
id
ls -la /var/run/
mkdir -p /var/run/libvirt

# Start libvirtd in foreground mode
echo "Starting libvirt daemon in foreground..."
/usr/sbin/libvirtd --listen &

# Wait for libvirt socket
echo "Waiting for libvirt socket..."
for i in {1..30}; do
    if [ -S /var/run/libvirt/libvirt-sock ]; then
        echo "Libvirt socket is ready"
        break
    fi
    echo "Waiting for libvirt socket... ($i/30)"
    sleep 1
done

if [ ! -S /var/run/libvirt/libvirt-sock ]; then
    echo "ERROR: Libvirt socket not found after waiting"
    # Debugging - try to find any libvirt sockets
    find / -name "libvirt-sock" 2>/dev/null || echo "No libvirt-sock found on the system"
fi

# Test virsh connectivity
echo "Testing virsh connectivity..."
virsh version || echo "Failed to get virsh version"

# Initialize storage pools
if ! virsh pool-list | grep -q "cloudstack-primary" 2>/dev/null; then
    echo "Creating primary storage pool..."
    virsh pool-define /root/primary-pool.xml || echo "Failed to define pool"
    virsh pool-build cloudstack-primary || echo "Failed to build pool"
    virsh pool-start cloudstack-primary || echo "Failed to start pool"
    virsh pool-autostart cloudstack-primary || echo "Failed to autostart pool"
fi

# Create a default network if it doesn't exist
if ! virsh net-list | grep -q "default" 2>/dev/null; then
    echo "Creating default network..."
    cat >/root/default-network.xml <<EOF
<network>
  <name>default</name>
  <forward mode='nat'/>
  <bridge name='virbr0' stp='on' delay='0'/>
  <ip address='192.168.122.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='192.168.122.2' end='192.168.122.254'/>
    </dhcp>
  </ip>
</network>
EOF
    virsh net-define /root/default-network.xml || echo "Failed to define network"
    virsh net-start default || echo "Failed to start network"
    virsh net-autostart default || echo "Failed to autostart network"
fi

# Keep the container running
echo "Starting SSH daemon..."
/usr/sbin/sshd -D

#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Setting up Libvirt for CloudStack QEMU Integration ===${NC}"

# Install libvirt using Homebrew if not installed
if ! brew list | grep -q libvirt; then
    echo -e "${YELLOW}Installing Libvirt via Homebrew...${NC}"
    brew install libvirt
else
    echo -e "${GREEN}Libvirt is already installed${NC}"
fi

# Create directories for libvirt configuration
LIBVIRT_DIR="$HOME/.cloudstack/libvirt"
QEMU_DIR="$HOME/.cloudstack/qemu"
mkdir -p "$LIBVIRT_DIR"
mkdir -p "$QEMU_DIR/images"

# Create a basic libvirt configuration file
echo -e "${YELLOW}Creating libvirt configuration...${NC}"
cat >"$LIBVIRT_DIR/libvirtd.conf" <<EOF
listen_tls = 0
listen_tcp = 1
tcp_port = "16509"
listen_addr = "0.0.0.0"
auth_tcp = "none"
unix_sock_dir = "$LIBVIRT_DIR"
unix_sock_group = "staff"
unix_sock_ro_perms = "0777"
unix_sock_rw_perms = "0770"
EOF

# Start libvirtd service
echo -e "${YELLOW}Attempting to start libvirt daemon...${NC}"
if pgrep -x "libvirtd" >/dev/null; then
    echo -e "${GREEN}Libvirt daemon is already running${NC}"
else
    echo -e "${YELLOW}Starting libvirt daemon...${NC}"
    libvirtd -f "$LIBVIRT_DIR/libvirtd.conf" -d
    echo -e "${GREEN}Libvirt daemon started${NC}"
fi

# Verify libvirt is working
echo -e "${YELLOW}Verifying libvirt connection...${NC}"
if virsh -c qemu:///system list >/dev/null 2>&1; then
    echo -e "${GREEN}Successfully connected to local libvirt${NC}"
else
    echo -e "${RED}Could not connect to local libvirt${NC}"
    echo -e "${YELLOW}You may need to run this script with sudo or configure permissions${NC}"
fi

echo -e "\n${BLUE}=== Next Steps ===${NC}"
echo -e "1. Access CloudStack UI at ${GREEN}http://localhost:8080/client${NC} (admin/password)"
echo -e "2. Try adding your QEMU host again with:"
echo -e "   ${GREEN}task cmk -- add host zoneid=... podid=... clusterid=... hypervisor=KVM username=root password=password url=qemu:///system${NC}"
echo -e "\n${YELLOW}Note: Make sure the libvirt TCP service is accessible from within the Docker container${NC}"

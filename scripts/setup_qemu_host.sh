#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Setting up QEMU host for CloudStack ===${NC}"

# Create directory for QEMU images
QEMU_DIR="$HOME/.cloudstack/qemu"
mkdir -p $QEMU_DIR
echo -e "${GREEN}Created QEMU directory at $QEMU_DIR${NC}"

# Create a network bridge for QEMU VMs
if ! ifconfig | grep -q bridge100; then
    echo -e "${YELLOW}Creating network bridge for QEMU...${NC}"
    sudo ifconfig bridge100 create
    sudo ifconfig bridge100 inet 192.168.100.1/24 up
    echo -e "${GREEN}Network bridge created: bridge100 (192.168.100.1/24)${NC}"
else
    echo -e "${GREEN}Network bridge already exists: bridge100${NC}"
fi

# Create a sample VM disk image (only if it doesn't exist)
if [ ! -f "$QEMU_DIR/cloudstack-host.qcow2" ]; then
    echo -e "${YELLOW}Creating a sample VM disk image...${NC}"
    qemu-img create -f qcow2 "$QEMU_DIR/cloudstack-host.qcow2" 20G
    echo -e "${GREEN}Created sample VM disk at $QEMU_DIR/cloudstack-host.qcow2${NC}"
else
    echo -e "${GREEN}Sample VM disk already exists at $QEMU_DIR/cloudstack-host.qcow2${NC}"
fi

# Get the Docker network information
DOCKER_IP=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' cloudstack-mcp-simulator)
DOCKER_NETWORK=$(docker network inspect -f '{{range .IPAM.Config}}{{.Subnet}}{{end}}' cloudstack-network)

echo -e "${BLUE}=== CloudStack Information ===${NC}"
echo -e "CloudStack Management Server IP: ${GREEN}$DOCKER_IP${NC}"
echo -e "Docker Network: ${GREEN}$DOCKER_NETWORK${NC}"
echo -e "QEMU Bridge Network: ${GREEN}192.168.100.0/24${NC}"

echo -e "\n${BLUE}=== Next Steps ===${NC}"
echo -e "1. Access CloudStack UI at ${GREEN}http://localhost:8080/client${NC} (admin/password)"
echo -e "2. Add a new zone with the following details:"
echo -e "   - Name: QEMU-Local"
echo -e "   - Network offering: DefaultIsolatedNetworkOfferingWithSourceNatService"
echo -e "   - Hypervisor: KVM"
echo -e "   - Host URL: qemu+tcp://localhost/system"
echo -e "   - Host Username: root"
echo -e "   - Host Password: (create a password)"
echo -e "   - Primary Storage URL: file://$QEMU_DIR/primary"
echo -e "   - Secondary Storage URL: file://$QEMU_DIR/secondary"
echo -e "\n${YELLOW}Note: You may need to manually configure networking between Docker and the QEMU bridge${NC}"

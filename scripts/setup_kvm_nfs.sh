#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Setting up NFS on KVM Host for CloudStack Primary Storage ===${NC}"

# Check if the container is running
if ! docker ps | grep -q "cloudstack-mcp-kvm"; then
    echo -e "${RED}KVM host container is not running${NC}"
    echo -e "${YELLOW}Please start the environment with 'task docker:start' first${NC}"
    exit 1
fi

# Install NFS server in the KVM container
echo -e "${YELLOW}Installing NFS server in KVM container...${NC}"
docker exec cloudstack-mcp-kvm apt-get update
docker exec cloudstack-mcp-kvm apt-get install -y nfs-kernel-server

# Create and configure primary storage directory
echo -e "${YELLOW}Configuring primary storage directory...${NC}"
docker exec cloudstack-mcp-kvm mkdir -p /var/cloudstack/primary
docker exec cloudstack-mcp-kvm chmod 777 /var/cloudstack/primary
docker exec cloudstack-mcp-kvm bash -c "echo '/var/cloudstack/primary *(rw,sync,no_subtree_check,no_root_squash)' > /etc/exports"

# Restart NFS server
echo -e "${YELLOW}Starting NFS server...${NC}"
docker exec cloudstack-mcp-kvm exportfs -a
docker exec cloudstack-mcp-kvm service nfs-kernel-server restart

# Verify NFS export
echo -e "${YELLOW}Verifying NFS export...${NC}"
docker exec cloudstack-mcp-kvm exportfs -v

echo -e "${GREEN}NFS setup complete!${NC}"

# Get the KVM container IP address
KVM_IP=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' cloudstack-mcp-kvm)
echo -e "KVM host container IP: ${GREEN}$KVM_IP${NC}"

echo -e "\n${BLUE}=== Next Steps ===${NC}"
echo -e "1. You can now create a primary storage pool with the following URL:"
echo -e "   ${GREEN}nfs://$KVM_IP:/var/cloudstack/primary${NC}"
echo -e "2. Then try adding the KVM host to CloudStack again"

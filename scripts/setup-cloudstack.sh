#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}Starting CloudStack setup...${NC}"

# Check for UTM first, as it's better for running CloudStack on M1 Macs
if command -v utm &>/dev/null; then
    echo -e "${GREEN}UTM found, setting up CloudStack using UTM...${NC}"

    # Define UTM VM settings
    VM_NAME="CloudStack Simulator"
    VM_IMAGE=".tmp/cloudstack/cloudstack-simulator.qcow2"

    # Check if VM image exists
    if [ ! -f "$VM_IMAGE" ]; then
        echo -e "${RED}CloudStack VM image not found at $VM_IMAGE${NC}"
        echo -e "${YELLOW}Please run 'task cloudstack:setup:download-image' first${NC}"
        exit 1
    fi

    # Check if VM already exists
    if utm list | grep -q "$VM_NAME"; then
        echo -e "${YELLOW}VM '$VM_NAME' already exists in UTM${NC}"
        echo -e "${GREEN}Starting VM...${NC}"
        utm start "$VM_NAME"
    else
        echo -e "${GREEN}Creating new CloudStack VM in UTM...${NC}"
        # Create a UTM VM from the qcow2 image
        # Note: These are command-line approximations, actual implementation might require UTM GUI
        utm create --name "$VM_NAME" \
            --cpu 2 \
            --memory 4096 \
            --disk "$VM_IMAGE" \
            --network-mode shared \
            --start
    fi

    echo -e "${GREEN}CloudStack VM should be starting in UTM...${NC}"
    echo -e "${YELLOW}CloudStack management interface should be available at http://localhost:8080/client in a few minutes${NC}"

# Fall back to Docker if UTM is not available
elif command -v docker &>/dev/null; then
    echo -e "${GREEN}Docker found, setting up CloudStack using Docker...${NC}"

    # Create a docker network for CloudStack
    if ! docker network inspect cloudstack &>/dev/null; then
        echo -e "${GREEN}Creating Docker network for CloudStack...${NC}"
        docker network create cloudstack
    fi

    # Check if the CloudStack simulator container is already running
    if docker ps -a --filter "name=cloudstack-simulator" | grep -q cloudstack-simulator; then
        echo -e "${YELLOW}CloudStack simulator container already exists${NC}"

        # Check if it's running
        if docker ps --filter "name=cloudstack-simulator" | grep -q cloudstack-simulator; then
            echo -e "${GREEN}CloudStack simulator is already running${NC}"
        else
            echo -e "${GREEN}Starting CloudStack simulator container...${NC}"
            docker start cloudstack-simulator
        fi
    else
        echo -e "${GREEN}Creating and starting CloudStack simulator container...${NC}"
        docker run -d \
            --name cloudstack-simulator \
            --network cloudstack \
            -p 8080:8080 \
            -p 8443:8443 \
            -p 8250:8250 \
            apache/cloudstack-simulator:4.20.0.0

        echo -e "${GREEN}CloudStack simulator container started${NC}"
        echo -e "${YELLOW}It may take a few minutes for CloudStack to fully initialize...${NC}"
    fi

    echo -e "${GREEN}CloudStack management interface should be available at http://localhost:8080/client in a few minutes${NC}"
    echo -e "${YELLOW}Default credentials: username: admin, password: password${NC}"

else
    echo -e "${RED}Neither UTM nor Docker found. Please install one of them to continue.${NC}"
    exit 1
fi

echo -e "${GREEN}CloudStack setup completed.${NC}"
echo -e "${GREEN}To check if CloudStack is ready, try accessing the API at http://localhost:8080/client${NC}"

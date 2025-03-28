#!/usr/bin/env bash

set -e

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== CloudStack KVM Setup Verification Tool ===${NC}"

# Check if running on ARM64
ARCH=$(uname -m)
if [[ "$ARCH" != "aarch64" && "$ARCH" != "arm64" ]]; then
    echo -e "${RED}This script is intended for ARM64 (Apple Silicon) machines.${NC}"
    echo -e "${RED}Detected architecture: $ARCH${NC}"
    exit 1
fi

# Check if running as root
if [ "$(id -u)" -ne 0 ]; then
    echo -e "${RED}This script must be run as root (sudo)${NC}"
    exit 1
fi

# Check if libvirt is installed
if ! command -v libvirtd >/dev/null 2>&1; then
    echo -e "${RED}libvirtd is not installed.${NC}"
    echo -e "${YELLOW}Please run 'task kvm:setup' or install libvirt manually.${NC}"
    exit 1
fi

# Check libvirt service status
if systemctl is-active libvirtd >/dev/null 2>&1; then
    echo -e "${GREEN}✓ libvirtd is running${NC}"
else
    echo -e "${RED}✗ libvirtd is not running${NC}"
    echo -e "${YELLOW}Try starting it with 'task kvm:start'${NC}"
    exit 1
fi

# Check if virsh can connect
if virsh list --all >/dev/null 2>&1; then
    echo -e "${GREEN}✓ virsh can connect to libvirt daemon${NC}"
else
    echo -e "${RED}✗ virsh cannot connect to libvirt daemon${NC}"
    echo -e "${YELLOW}Check libvirt configuration${NC}"
    exit 1
fi

# Check if CloudStack agent is installed
if [ -f /etc/cloudstack/agent/agent.properties ]; then
    echo -e "${GREEN}✓ CloudStack agent is installed${NC}"

    # Check agent configuration
    if grep -q "^hypervisor=kvm" /etc/cloudstack/agent/agent.properties; then
        echo -e "${GREEN}✓ CloudStack agent is configured for KVM${NC}"
    else
        echo -e "${RED}✗ CloudStack agent is not configured for KVM${NC}"
        echo -e "${YELLOW}Please check /etc/cloudstack/agent/agent.properties${NC}"
    fi

    # Check CPU speed configuration
    if grep -q "^host.cpu.speed" /etc/cloudstack/agent/agent.properties; then
        CPU_SPEED=$(grep "^host.cpu.speed" /etc/cloudstack/agent/agent.properties | cut -d= -f2)
        echo -e "${GREEN}✓ CPU speed is set to $CPU_SPEED MHz${NC}"
    else
        echo -e "${YELLOW}⚠ CPU speed is not set manually${NC}"
        echo -e "${YELLOW}This may cause issues with Asahi Linux${NC}"
    fi

    # Check agent service
    if systemctl is-active cloudstack-agent >/dev/null 2>&1; then
        echo -e "${GREEN}✓ CloudStack agent service is running${NC}"
    else
        echo -e "${RED}✗ CloudStack agent service is not running${NC}"
        echo -e "${YELLOW}Try starting it with 'systemctl start cloudstack-agent'${NC}"
    fi
else
    echo -e "${YELLOW}⚠ CloudStack agent is not installed${NC}"
    echo -e "${YELLOW}This is normal if you ran setup-only${NC}"
fi

# Check NFS exports
if command -v showmount >/dev/null 2>&1; then
    if showmount -e localhost | grep -q "cloudstack"; then
        echo -e "${GREEN}✓ NFS exports are configured for CloudStack${NC}"
    else
        echo -e "${RED}✗ NFS exports are not configured for CloudStack${NC}"
        echo -e "${YELLOW}Check /etc/exports and restart nfs-kernel-server${NC}"
    fi
else
    echo -e "${YELLOW}⚠ showmount is not installed, cannot verify NFS exports${NC}"
fi

# Check if system VM template exists
HOMEDIR=$(eval echo ~$SUDO_USER)
TEMPLATE_PATH="$HOMEDIR/cloudstack/secondary/template/tmpl/1/1/template"
if [ -f "$TEMPLATE_PATH" ]; then
    TEMPLATE_SIZE=$(du -h "$TEMPLATE_PATH" | cut -f1)
    echo -e "${GREEN}✓ SystemVM template exists (Size: $TEMPLATE_SIZE)${NC}"
else
    echo -e "${RED}✗ SystemVM template is missing${NC}"
    echo -e "${YELLOW}Expected at: $TEMPLATE_PATH${NC}"
fi

echo -e "${BLUE}=== Verification Complete ===${NC}"

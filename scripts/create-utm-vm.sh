#!/usr/bin/env bash

set -e

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== CloudStack UTM VM Creation Script ===${NC}"

# Check if running on macOS
if [[ "$(uname)" != "Darwin" ]]; then
	echo -e "${RED}This script must be run on macOS.${NC}"
	exit 1
fi

# Check if UTM is installed
if ! [ -d "/Applications/UTM.app" ]; then
	echo -e "${YELLOW}UTM not found. Please install UTM first from https://mac.getutm.app/${NC}"
	echo -e "${YELLOW}You can also use 'brew install --cask utm'${NC}"
	exit 1
fi

# Default values
VM_NAME="CloudStack-Ubuntu"
CPU_COUNT=4
RAM_SIZE=8192 # 8GB
DISK_SIZE=64  # 64GB
ISO_URL="https://cdimage.ubuntu.com/releases/22.04/release/ubuntu-22.04.3-live-server-arm64.iso"
ISO_FILENAME="ubuntu-22.04.3-live-server-arm64.iso"
DOWNLOAD_DIR="$HOME/Downloads"

# Parse command line arguments
while [[ "$#" -gt 0 ]]; do
	case $1 in
	--name)
		VM_NAME="$2"
		shift
		;;
	--cpu)
		CPU_COUNT="$2"
		shift
		;;
	--ram)
		RAM_SIZE="$2"
		shift
		;;
	--disk)
		DISK_SIZE="$2"
		shift
		;;
	--iso)
		ISO_URL="$2"
		shift
		;;
	*)
		echo "Unknown parameter: $1"
		exit 1
		;;
	esac
	shift
done

# Check if ISO exists, download if not
ISO_PATH="$DOWNLOAD_DIR/$ISO_FILENAME"
if [ ! -f "$ISO_PATH" ]; then
	echo -e "${YELLOW}Downloading Ubuntu Server ISO...${NC}"
	curl -L "$ISO_URL" -o "$ISO_PATH"
	echo -e "${GREEN}Download complete.${NC}"
else
	echo -e "${GREEN}Ubuntu Server ISO already exists at $ISO_PATH${NC}"
fi

# Create UTM VM using the utm CLI tool
echo -e "${BLUE}Creating UTM virtual machine with:${NC}"
echo -e "  Name: ${GREEN}$VM_NAME${NC}"
echo -e "  CPU Cores: ${GREEN}$CPU_COUNT${NC}"
echo -e "  RAM: ${GREEN}$RAM_SIZE MB${NC}"
echo -e "  Disk: ${GREEN}$DISK_SIZE GB${NC}"
echo -e "  ISO: ${GREEN}$ISO_FILENAME${NC}"

# Check if utm command-line tool is available
if ! command -v utm &>/dev/null; then
	echo -e "${YELLOW}UTM command-line tool not found.${NC}"
	echo -e "${YELLOW}Opening UTM application. Please create the VM manually.${NC}"

	echo -e "${BLUE}Instructions for manual VM creation:${NC}"
	echo -e "1. Create a new VM with these specs:"
	echo -e "   - Name: $VM_NAME"
	echo -e "   - CPU: $CPU_COUNT cores"
	echo -e "   - RAM: $RAM_SIZE MB"
	echo -e "   - Disk: $DISK_SIZE GB"
	echo -e "   - ISO: $ISO_PATH"
	echo -e "2. Choose 'Linux' as OS type"
	echo -e "3. Enable hardware virtualization (important for CloudStack!)"
	echo -e "4. Boot the VM and proceed with Ubuntu installation"
	echo -e "5. After installation, run our CloudStack setup script"

	open -a UTM
else
	# Create the VM using utm command
	utm create --name "$VM_NAME" \
		--cpu "$CPU_COUNT" \
		--memory "$RAM_SIZE" \
		--disk "$DISK_SIZE" \
		--operating-system linux \
		--arch arm64 \
		--boot "$ISO_PATH"

	echo -e "${GREEN}VM created successfully!${NC}"

	# Start the VM
	utm start "$VM_NAME"
	echo -e "${GREEN}VM started. Proceed with Ubuntu installation.${NC}"
fi

echo -e "${BLUE}=== Next Steps ===${NC}"
echo -e "1. Complete the Ubuntu Server installation"
echo -e "2. Note the IP address of your Ubuntu VM (run 'ip addr' in the VM)"
echo -e "3. Update our project's configuration with the VM IP address"
echo -e "4. Run 'task vm:setup' to install CloudStack on the VM"

echo -e "${YELLOW}Important: Enable nested virtualization in Ubuntu VM${NC}"
echo -e "After installation, run these commands in the VM:"
echo -e "  sudo apt update"
echo -e "  sudo apt install qemu-kvm libvirt-daemon-system"
echo -e "  sudo modprobe kvm_arm_virt"

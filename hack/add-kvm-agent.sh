#!/bin/bash
set -e -o pipefail

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# CloudStack management server details
MANAGEMENT_IP="192.168.1.100" # This will be detected from Docker network
MANAGEMENT_PORT="8250"

# Zone/Pod/Cluster IDs from CloudStack
ZONE_ID="9d10c8a8-d9c3-4977-ba29-9edb845db394"
POD_ID="ca30bdc7-8420-4e99-94aa-6f1f3ff58049"
CLUSTER_ID="bf0569c7-355b-4fc0-9726-79f19837d2c4"

tmpdir=$(mktemp -d)

# Storage UUIDs
GUID=$(uuidgen)
LOCAL_STORAGE_UUID=$(uuidgen)

log_info() {
	echo -e "${BLUE}[INFO] $1${NC}"
}

log_success() {
	echo -e "${GREEN}[SUCCESS] $1${NC}"
}

log_warning() {
	echo -e "${YELLOW}[WARNING] $1${NC}"
}

log_error() {
	echo -e "${RED}[ERROR] $1${NC}"
}

echo -e "${BLUE}=== CloudStack KVM Agent Setup ===${NC}"

# Check if CloudStack containers are running
if ! docker ps | ggrep -q cloudstack-mcp-simulator; then
	log_error "CloudStack simulator is not running."
	log_warning "Please start CloudStack with 'task docker:start' first."
	exit 1
fi

# Get Management Server IP (the Docker host IP from the container's perspective)
log_info "Getting management server IP address..."
DOCKER_HOST_IP=$(docker exec cloudstack-mcp-simulator bash -c "ip route | grep default | awk '{print \$3}'")
if [ -n "$DOCKER_HOST_IP" ]; then
	MANAGEMENT_IP=$DOCKER_HOST_IP
	log_success "Found management server IP: ${MANAGEMENT_IP}"
else
	log_warning "Could not determine management server IP automatically. Using default: ${MANAGEMENT_IP}"
fi

# Check if Lima VM "agent" exists
log_info "Checking for Lima agent VM..."
if ! limactl list | ggrep -q "agent"; then
	log_error "Lima agent VM does not exist."
	log_warning "Please create it first with 'limactl start --name=agent hack/cloudstack-agent.lima.yaml'"
	exit 1
fi

# Check if agent is running
log_info "Checking if CloudStack agent is already running..."
AGENT_STATUS=$(limactl shell agent sudo systemctl is-active cloudstack-agent 2>/dev/null || echo "inactive")

# Check if agent.properties already exists and is configured
log_info "Checking if agent is already configured..."
AGENT_CONFIGURED=false
if limactl shell agent sudo test -f /etc/cloudstack/agent/agent.properties 2>/dev/null; then
	CURRENT_HOST=$(limactl shell agent sudo grep -oP "(?<=^host=).*" /etc/cloudstack/agent/agent.properties 2>/dev/null || echo "")
	CURRENT_ZONE=$(limactl shell agent sudo grep -oP "(?<=^zone=).*" /etc/cloudstack/agent/agent.properties 2>/dev/null || echo "")

	if [[ -n "$CURRENT_HOST" && -n "$CURRENT_ZONE" ]]; then
		if [[ "$CURRENT_HOST" == "$MANAGEMENT_IP" && "$CURRENT_ZONE" == "$ZONE_ID" ]]; then
			log_warning "Agent is already configured with the same management server and zone."
			AGENT_CONFIGURED=true

			# Get existing GUIDs to maintain them
			GUID=$(limactl shell agent sudo grep -oP "(?<=^guid=).*" /etc/cloudstack/agent/agent.properties 2>/dev/null || echo "$GUID")
			LOCAL_STORAGE_UUID=$(limactl shell agent sudo grep -oP "(?<=^local.storage.uuid=).*" /etc/cloudstack/agent/agent.properties 2>/dev/null || echo "$LOCAL_STORAGE_UUID")
		else
			log_warning "Agent is configured but with different parameters. Will reconfigure."
		fi
	fi
fi

if [ "$AGENT_CONFIGURED" = false ]; then
	# Setup the CloudStack agent configuration
	log_info "Configuring CloudStack agent on Lima VM..."
	cat >"$tmpdir/agent.properties" <<<"
# CloudStack Agent Configuration
# Generated by add-kvm-agent.sh

# Management Server
host=${MANAGEMENT_IP}
port=${MANAGEMENT_PORT}

# Zone/Pod/Cluster identification
zone=${ZONE_ID}
pod=${POD_ID}  
cluster=${CLUSTER_ID}
guid=${GUID}

# Local Storage
local.storage.uuid=${LOCAL_STORAGE_UUID}
"

	log_info "Copying agent configuration to Lima VM..."
	limactl shell agent sudo mkdir -p /etc/cloudstack/agent
	limactl cp "$tmpdir/agent.properties" agent:/tmp/agent.properties
	limactl shell agent sudo cp /tmp/agent.properties /etc/cloudstack/agent/agent.properties

	# Get the VM's IP address
	log_info "Getting VM network information..."
	VM_IP=$(limactl shell agent ip addr show eth0 | ggrep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -1)
	BRIDGE_IP=$(limactl shell agent ip addr show cloudbr0 | ggrep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -1 || echo "Not configured")

	log_success "Agent VM IP: ${VM_IP}"
	log_success "Bridge IP: ${BRIDGE_IP}"

	# Check if network info is already in the config
	if ! limactl shell agent sudo grep -q "priv.host.ip" /etc/cloudstack/agent/agent.properties; then
		# Add this information to agent.properties
		log_info "Updating configuration with network information..."
		limactl shell agent "sudo sh -c 'echo -e \"\n# Network Configuration\npriv.host.ip=${VM_IP}\nbridge.ip=${BRIDGE_IP}\" >> /etc/cloudstack/agent/agent.properties'"
	fi
else
	# Just get the VM's IP address for display
	VM_IP=$(limactl shell agent ip addr show eth0 | ggrep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -1)
	BRIDGE_IP=$(limactl shell agent ip addr show cloudbr0 | ggrep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -1 || echo "Not configured")
fi

# Restart CloudStack agent only if needed
if [ "$AGENT_STATUS" != "active" ] || [ "$AGENT_CONFIGURED" = false ]; then
	log_info "Restarting CloudStack agent service..."
	limactl shell agent sudo systemctl restart cloudstack-agent
else
	log_info "CloudStack agent is already running with correct configuration."
fi

# # Check service status
# log_info "Checking CloudStack agent service status..."
# limactl shell --tty=true agent sudo systemctl status cloudstack-agent

# # Add option to skip monitoring
# if [ "$1" != "--no-monitor" ]; then
# 	# Monitor logs
# 	log_info "Showing CloudStack agent logs (press Ctrl+C to exit)..."
# 	log_info "To skip monitoring logs in the future, run with --no-monitor flag"
# 	limactl shell agent sudo journalctl -u cloudstack-agent -f
# else
# 	log_info "Skipping log monitoring as requested."
# fi

echo -e "${GREEN}=== Agent Setup Complete ===${NC}"
echo -e "${YELLOW}To check agent status in CloudStack UI:${NC}"
echo -e "  1. Open http://localhost:8080/client"
echo -e "  2. Navigate to Infrastructure > Hosts"
echo -e "  3. Check if your KVM host has been added successfully"
echo -e "\n${YELLOW}Agent properties:${NC}"
echo -e "  Zone ID: ${ZONE_ID}"
echo -e "  Pod ID: ${POD_ID}"
echo -e "  Cluster ID: ${CLUSTER_ID}"
echo -e "  GUID: ${GUID}"
echo -e "  Local Storage UUID: ${LOCAL_STORAGE_UUID}"
echo -e "  VM IP: ${VM_IP}"
echo -e "  Bridge IP: ${BRIDGE_IP}"

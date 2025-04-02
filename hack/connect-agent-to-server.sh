#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# CloudStack management server details
MANAGEMENT_HOST="127.0.0.1" # its forwarded from the Lima VM to the Docker host
MANAGEMENT_PORT="8080"

# Check Lima environment
if ! command -v limactl &>/dev/null; then
	echo -e "${RED}[ERROR] Lima is not installed. Please install it first.${NC}"
	exit 1
fi

# Check Docker environment
if ! command -v docker &>/dev/null; then
	echo -e "${RED}[ERROR] Docker is not installed. Please install it first.${NC}"
	exit 1
fi

echo -e "${BLUE}=== CloudStack Agent Connection Setup ===${NC}"

# Check if CloudStack containers are running
if ! docker ps | grep -q cloudstack-mcp-simulator; then
	echo -e "${RED}[ERROR] CloudStack simulator is not running.${NC}"
	echo -e "${YELLOW}[INFO] Please start CloudStack with 'task docker:start' first.${NC}"
	exit 1
fi

# Check if Lima VM "agent" exists
echo -e "${BLUE}[INFO] Checking for Lima agent VM...${NC}"
if ! limactl list | grep -q "agent"; then
	echo -e "${RED}[ERROR] Lima agent VM does not exist.${NC}"
	echo -e "${YELLOW}[INFO] Please create it first with 'limactl start --name=agent hack/lima/agent/agent.yaml'${NC}"
	exit 1
fi

# Get IP addresses for both environments
echo -e "${BLUE}[INFO] Getting network information...${NC}"

# # Get the Docker container gateway IP from the container's perspective
# DOCKER_HOST_IP=$(docker exec cloudstack-mcp-simulator bash -c "ip route | grep default | awk '{print \$3}'")
# if [ -z "$DOCKER_HOST_IP" ]; then
# 	echo -e "${RED}[ERROR] Could not determine Docker host IP from container.${NC}"
# 	exit 1
# fi
# echo -e "${GREEN}[SUCCESS] Docker host IP (from container perspective): ${DOCKER_HOST_IP}${NC}"

# # Get the Lima VM IP address
# LIMA_VM_IP=$(limactl shell agent ip addr show eth0 | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -1)
# if [ -z "$LIMA_VM_IP" ]; then
# 	echo -e "${RED}[ERROR] Could not determine Lima VM IP address.${NC}"
# 	exit 1
# fi
# echo -e "${GREEN}[SUCCESS] Lima VM IP: ${LIMA_VM_IP}${NC}"

# # Get the Lima VM bridge IP address (for CloudStack networking)
# LIMA_BRIDGE_IP=$(limactl shell agent ip addr show cloudbr0 | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -1 || echo "Not configured")
# echo -e "${BLUE}[INFO] Lima bridge IP: ${LIMA_BRIDGE_IP}${NC}"

# Test connectivity from Docker to Lima (docker container should be able to hit the port 8250)
echo -e "${BLUE}[INFO] Testing network connectivity from Docker to Lima VM...${NC}"
if ! docker exec cloudstack-mcp-simulator ping -c 1 -W 2 ${MANAGEMENT_HOST} &>/dev/null; then
	echo -e "${YELLOW}[WARNING] Docker container cannot reach Lima VM at ${MANAGEMENT_HOST}.${NC}"
	echo -e "${YELLOW}[WARNING] This may cause connection issues between management server and agent.${NC}"
else
	echo -e "${GREEN}[SUCCESS] Docker container can reach Lima VM.${NC}"
fi

# Test connectivity from Lima to Docker
echo -e "${BLUE}[INFO] Testing network connectivity from Lima VM to Docker host...${NC}"
if ! limactl shell agent ping -c 1 -W 2 "${MANAGEMENT_HOST}" &>/dev/null; then
	echo -e "${YELLOW}[WARNING] Lima VM cannot reach Docker host at ${DOCKER_HOST_IP}.${NC}"
	echo -e "${YELLOW}[WARNING] This may cause connection issues between agent and management server.${NC}"
else
	echo -e "${GREEN}[SUCCESS] Lima VM can reach Docker host.${NC}"
fi

# Update agent.properties file
echo -e "${BLUE}[INFO] Updating CloudStack agent configuration...${NC}"

# Get Zone/Pod/Cluster IDs from CloudStack
echo -e "${BLUE}[INFO] Getting infrastructure identifiers from CloudStack...${NC}"
ZONE_ID=$(docker exec cloudstack-mcp-cmk cmk list zones name=QAZone | ggrep -oP '(?<="id": ")[^"]*' | head -1)

# check if zone with name QAZone already exists
if [[ -z "$ZONE_ID" ]]; then
	# 💩 Missing required parameters:  dns1, internaldns1, networktype
	echo -e "${YELLOW}[WARNING] Zone with name QAZone does not exist. Creating new zone...${NC}"
	zone_uuid=$(uuidgen)
	docker exec cloudstack-mcp-cmk cmk create zone name=QAZone dns1=10.10.10.1 internaldns1=10.10.10.1 networktype=Advanced uuid="${zone_uuid}"
	ZONE_ID=$zone_uuid
	echo -e "${GREEN}[SUCCESS] Created new zone with ID: ${ZONE_ID}${NC}"
	sleep 5
else
	echo -e "${GREEN}[SUCCESS] Zone with name QAZone already exists (ID: ${ZONE_ID}). Using existing zone.${NC}"
fi

# check if pod with name TestPod already exists
POD_ID=$(docker exec cloudstack-mcp-cmk cmk list pods zoneid="${ZONE_ID}" name=QAPod | ggrep -oP '(?<="id": ")[^"]*' | head -1)
if [[ -z "$POD_ID" ]]; then
	pod_uuid=$(uuidgen)
	echo -e "${BLUE}[INFO] Creating new pod with ID: ${pod_uuid}${NC}"
	docker exec cloudstack-mcp-cmk cmk create pod name=QAPod zoneid="${ZONE_ID}" uuid="${pod_uuid}" startip=10.10.10.1 endip=10.10.10.254 netmask=255.255.255.0 gateway=10.10.10.255
	POD_ID=$pod_uuid
	echo -e "${GREEN}[SUCCESS] Created new pod with ID: ${POD_ID}${NC}"
	sleep 5
else
	echo -e "${GREEN}[SUCCESS] Pod with name QAPod already exists (ID: ${POD_ID}). Using existing pod.${NC}"
fi

# check if cluster with name TestCluster already exists
CLUSTER_ID=$(docker exec cloudstack-mcp-cmk cmk list clusters zoneid="${ZONE_ID}" name=QACluster | ggrep -oP '(?<="id": ")[^"]*' | head -1)
if [[ -z "$CLUSTER_ID" ]]; then
	cluster_uuid=$(uuidgen)
	echo -e "${BLUE}[INFO] Creating new cluster with ID: ${cluster_uuid}${NC}"
	docker exec cloudstack-mcp-cmk cmk add cluster name=QACluster clustertype=ExternalManaged hypervisor=KVM zoneid="${ZONE_ID}" uuid="${cluster_uuid}" podid="${POD_ID}" clustername=QACluster
	CLUSTER_ID=$cluster_uuid
	echo -e "${GREEN}[SUCCESS] Created new cluster with ID: ${CLUSTER_ID}${NC}"
	sleep 10
else
	echo -e "${GREEN}[SUCCESS] Cluster with name QACluster already exists (ID: ${CLUSTER_ID}). Using existing cluster.${NC}"
	# exit 33
fi

# Generate UUIDs for agent
GUID=$(uuidgen)
LOCAL_STORAGE_UUID=$(uuidgen)

# Create a temporary agent.properties file
TMPDIR=$(mktemp -d)
cat >"${TMPDIR}/agent.properties" <<<"
# CloudStack Agent Configuration
# Generated by connect-agent-to-server.sh

# Management Server
host=${MANAGEMENT_HOST}
port=${MANAGEMENT_PORT}

# Zone/Pod/Cluster identification
zone=${ZONE_ID}
pod=${POD_ID}  
cluster=${CLUSTER_ID}
guid=${GUID}

# Local Storage
local.storage.uuid=${LOCAL_STORAGE_UUID}

# Network Configuration
priv.host.ip=${LIMA_VM_IP}
bridge.ip=${LIMA_BRIDGE_IP}
"

# Copy the configuration to the Lima VM
echo -e "${BLUE}[INFO] Copying agent configuration to Lima VM...${NC}"
limactl shell agent sudo mkdir -p /etc/cloudstack/agent
limactl cp "${TMPDIR}/agent.properties" agent:/tmp/agent.properties
limactl shell agent sudo cp /tmp/agent.properties /etc/cloudstack/agent/agent.properties

# Restart the CloudStack agent
echo -e "${BLUE}[INFO] Restarting CloudStack agent service...${NC}"
limactl shell agent sudo systemctl restart cloudstack-agent

# Check agent status
echo -e "${BLUE}[INFO] Checking CloudStack agent service status...${NC}"
AGENT_STATUS=$(limactl shell agent sudo systemctl is-active cloudstack-agent)
if [ "$AGENT_STATUS" == "active" ]; then
	echo -e "${GREEN}[SUCCESS] CloudStack agent service is running.${NC}"
else
	echo -e "${RED}[ERROR] CloudStack agent service is not running.${NC}"
	echo -e "${BLUE}[INFO] Checking agent logs for errors:${NC}"
	limactl shell agent sudo journalctl -u cloudstack-agent -n 10
fi

# Display summary
echo -e "\n${GREEN}=== Configuration Summary ===${NC}"
echo -e "Management Server IP: ${DOCKER_HOST_IP}"
echo -e "Management Server Port: ${MANAGEMENT_PORT}"
echo -e "Agent VM IP: ${LIMA_VM_IP}"
echo -e "Agent Bridge IP: ${LIMA_BRIDGE_IP}"
echo -e "Zone ID: ${ZONE_ID}"
echo -e "Pod ID: ${POD_ID}"
echo -e "Cluster ID: ${CLUSTER_ID}"
echo -e "Agent GUID: ${GUID}"
echo -e "Local Storage UUID: ${LOCAL_STORAGE_UUID}"

echo -e "\n${BLUE}=== Next Steps ===${NC}"
echo -e "1. Check if the agent appears in CloudStack UI:"
echo -e "   - Open http://localhost:8080/client"
echo -e "   - Navigate to Infrastructure > Hosts"
echo -e "   - The agent should appear in the list (may take a minute or two)"
echo -e "2. If the agent does not appear, check the logs:"
echo -e "   - Run: limactl shell agent sudo journalctl -u cloudstack-agent -f"
echo -e "   - Look for connection issues or errors"

# Clean up
rm -rf "${TMPDIR}"

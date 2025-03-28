#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Adding KVM Host to CloudStack ===${NC}"

# Check if the containers are running
if ! docker ps | grep -q "cloudstack-mcp-simulator"; then
	echo -e "${RED}CloudStack simulator is not running${NC}"
	echo -e "${YELLOW}Please start the environment with 'task docker:start' first${NC}"
	exit 1
fi

if ! docker ps | grep -q "cloudstack-mcp-kvm"; then
	echo -e "${RED}KVM host container is not running${NC}"
	echo -e "${YELLOW}Please start the environment with 'task docker:start' first${NC}"
	exit 1
fi

# Get the KVM container IP address
KVM_IP=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' cloudstack-mcp-kvm)
echo -e "KVM host container IP: ${GREEN}$KVM_IP${NC}"

# Check if we can connect to libvirt from the container
echo -e "${YELLOW}Testing libvirt connectivity from host...${NC}"
if ! docker exec cloudstack-mcp-kvm virsh -c qemu:///system list >/dev/null 2>&1; then
	echo -e "${RED}Cannot connect to libvirt inside KVM container${NC}"
	echo -e "${YELLOW}Checking libvirt service...${NC}"
	docker exec cloudstack-mcp-kvm ps aux | grep libvirt
	exit 1
fi

# Check TCP connection for libvirt
echo -e "${YELLOW}Testing TCP connectivity to libvirt...${NC}"
if ! docker exec cloudstack-mcp-kvm virsh -c qemu+tcp://$KVM_IP/system list >/dev/null 2>&1; then
	echo -e "${RED}Cannot connect to libvirt via TCP inside KVM container${NC}"
	echo -e "${YELLOW}Checking libvirt configuration...${NC}"
	docker exec cloudstack-mcp-kvm cat /etc/libvirt/libvirtd.conf | grep -E "listen|auth"
	exit 1
fi

# Make sure libvirt-clients is installed in the CloudStack container
echo -e "${YELLOW}Installing libvirt-clients in CloudStack container...${NC}"
if ! docker exec cloudstack-mcp-simulator which virsh >/dev/null 2>&1; then
	echo -e "${YELLOW}Installing libvirt-clients...${NC}"
	docker exec cloudstack-mcp-simulator apt-get update >/dev/null
	docker exec cloudstack-mcp-simulator apt-get install -y libvirt-clients >/dev/null
fi

# Test connection from CloudStack container to KVM host
echo -e "${YELLOW}Testing connection from CloudStack to KVM host...${NC}"
if ! docker exec cloudstack-mcp-simulator virsh -c qemu+tcp://$KVM_IP/system list >/dev/null 2>&1; then
	echo -e "${RED}CloudStack container cannot connect to KVM libvirt${NC}"
	echo -e "${YELLOW}Checking network connectivity...${NC}"
	docker exec cloudstack-mcp-simulator apt-get install -y iputils-ping >/dev/null 2>&1 || true
	docker exec cloudstack-mcp-simulator ping -c 3 $KVM_IP
	exit 1
fi

echo -e "${GREEN}Verified libvirt connectivity from CloudStack to KVM host${NC}"

# Check if we have a zone
echo -e "${YELLOW}Checking CloudStack infrastructure...${NC}"
ZONE_OUTPUT=$(bash ./scripts/cmk.sh list zones)
if [[ -z "$ZONE_OUTPUT" ]]; then
	echo -e "${YELLOW}Creating zone...${NC}"
	ZONE_RESULT=$(bash ./scripts/cmk.sh create zone name=TestZone networktype=Advanced dns1=8.8.8.8 internaldns1=8.8.8.8 securitygroupenabled=false localstorageenabled=true)
	ZONE_ID=$(echo "$ZONE_RESULT" | grep -o '"id": "[^"]*' | grep -o '[a-f0-9\-]*$')
	echo -e "Zone ID: ${GREEN}$ZONE_ID${NC}"
else
	ZONE_ID=$(echo "$ZONE_OUTPUT" | grep -o '"id": "[^"]*' | grep -o '[a-f0-9\-]*$')
	echo -e "Existing Zone ID: ${GREEN}$ZONE_ID${NC}"
fi

# Check for a physical network
PHYS_NET_OUTPUT=$(bash ./scripts/cmk.sh list physicalnetworks zoneid=$ZONE_ID)
if [[ -z "$PHYS_NET_OUTPUT" ]]; then
	echo -e "${YELLOW}Creating physical network...${NC}"
	PHYS_NET_RESULT=$(bash ./scripts/cmk.sh create physicalnetwork zoneid=$ZONE_ID name=TestPhysicalNetwork isolationmethods=VLAN)
	PHYS_NET_ID=$(echo "$PHYS_NET_RESULT" | grep -o '"id": "[^"]*' | grep -o '[a-f0-9\-]*$')
	echo -e "Physical Network ID: ${GREEN}$PHYS_NET_ID${NC}"

	echo -e "${YELLOW}Adding traffic types...${NC}"
	bash ./scripts/cmk.sh add traffictype physicalnetworkid=$PHYS_NET_ID traffictype=Management >/dev/null
	bash ./scripts/cmk.sh add traffictype physicalnetworkid=$PHYS_NET_ID traffictype=Guest >/dev/null
	bash ./scripts/cmk.sh add traffictype physicalnetworkid=$PHYS_NET_ID traffictype=Storage >/dev/null
	bash ./scripts/cmk.sh add traffictype physicalnetworkid=$PHYS_NET_ID traffictype=Public >/dev/null

	echo -e "${YELLOW}Enabling physical network...${NC}"
	bash ./scripts/cmk.sh update physicalnetwork id=$PHYS_NET_ID state=Enabled >/dev/null
else
	PHYS_NET_ID=$(echo "$PHYS_NET_OUTPUT" | grep -o '"id": "[^"]*' | grep -o '[a-f0-9\-]*$')
	echo -e "Existing Physical Network ID: ${GREEN}$PHYS_NET_ID${NC}"
fi

# Ensure zone is enabled
echo -e "${YELLOW}Enabling zone...${NC}"
bash ./scripts/cmk.sh update zone id=$ZONE_ID allocationstate=Enabled >/dev/null 2>&1 || echo -e "${YELLOW}Zone may already be enabled${NC}"

# Check for pod
POD_OUTPUT=$(bash ./scripts/cmk.sh list pods zoneid=$ZONE_ID)
if [[ -z "$POD_OUTPUT" ]]; then
	echo -e "${YELLOW}Creating pod...${NC}"
	POD_RESULT=$(bash ./scripts/cmk.sh create pod name=TestPod zoneid=$ZONE_ID gateway=192.168.1.1 netmask=255.255.255.0 startip=192.168.1.100 endip=192.168.1.200)
	POD_ID=$(echo "$POD_RESULT" | grep -o '"id": "[^"]*' | grep -o '[a-f0-9\-]*$')
	echo -e "Pod ID: ${GREEN}$POD_ID${NC}"
else
	POD_ID=$(echo "$POD_OUTPUT" | grep -o '"id": "[^"]*' | grep -o '[a-f0-9\-]*$')
	echo -e "Existing Pod ID: ${GREEN}$POD_ID${NC}"
fi

# Check for cluster
CLUSTER_OUTPUT=$(bash ./scripts/cmk.sh list clusters zoneid=$ZONE_ID podid=$POD_ID)
if [[ -z "$CLUSTER_OUTPUT" ]]; then
	echo -e "${YELLOW}Creating cluster...${NC}"
	CLUSTER_RESULT=$(bash ./scripts/cmk.sh add cluster zoneid=$ZONE_ID podid=$POD_ID clustername=TestCluster hypervisor=KVM clustertype=CloudManaged)
	CLUSTER_ID=$(echo "$CLUSTER_RESULT" | grep -o '"id": "[^"]*' | grep -o '[a-f0-9\-]*$')
	echo -e "Cluster ID: ${GREEN}$CLUSTER_ID${NC}"
else
	CLUSTER_ID=$(echo "$CLUSTER_OUTPUT" | grep -o '"id": "[^"]*' | grep -o '[a-f0-9\-]*$' | head -1)
	echo -e "Existing Cluster ID: ${GREEN}$CLUSTER_ID${NC}"
fi

# Add the KVM host to CloudStack - try different URL formats
echo -e "${YELLOW}Adding KVM host to CloudStack (attempt 1)...${NC}"
RESULT1=$(bash ./scripts/cmk.sh add host zoneid=$ZONE_ID podid=$POD_ID clusterid=$CLUSTER_ID hypervisor=KVM username=root password=password url=qemu+tcp://$KVM_IP/system hosttags=kvm 2>&1 || true)

if echo "$RESULT1" | grep -q "Error"; then
	echo -e "${YELLOW}First attempt failed, trying alternative URL format...${NC}"
	echo -e "${YELLOW}Adding KVM host to CloudStack (attempt 2)...${NC}"
	RESULT2=$(bash ./scripts/cmk.sh add host zoneid=$ZONE_ID podid=$POD_ID clusterid=$CLUSTER_ID hypervisor=KVM username=root password=password url=qemu://$KVM_IP/system hosttags=kvm 2>&1 || true)

	if echo "$RESULT2" | grep -q "Error"; then
		echo -e "${YELLOW}Second attempt failed, trying with port 16509...${NC}"
		echo -e "${YELLOW}Adding KVM host to CloudStack (attempt 3)...${NC}"
		RESULT3=$(bash ./scripts/cmk.sh add host zoneid=$ZONE_ID podid=$POD_ID clusterid=$CLUSTER_ID hypervisor=KVM username=root password=password url=qemu+tcp://$KVM_IP:16509/system hosttags=kvm 2>&1 || true)

		if echo "$RESULT3" | grep -q "Error"; then
			echo -e "${RED}All attempts to add KVM host failed${NC}"
			echo -e "${YELLOW}Creating primary storage might help, attempting...${NC}"

			# Try creating primary storage
			STORAGE_RESULT=$(bash ./scripts/cmk.sh create storagepool zoneid=$ZONE_ID podid=$POD_ID clusterid=$CLUSTER_ID name=TestPrimaryStorage url=nfs://$KVM_IP:/var/cloudstack/primary scope=cluster hypervisor=KVM 2>&1 || true)

			if echo "$STORAGE_RESULT" | grep -q "Error"; then
				echo -e "${RED}Failed to create primary storage${NC}"
				echo -e "${YELLOW}Final attempt with local storage...${NC}"
				RESULT4=$(bash ./scripts/cmk.sh add host zoneid=$ZONE_ID podid=$POD_ID clusterid=$CLUSTER_ID hypervisor=KVM username=root password=password url=qemu+tcp://$KVM_IP/system hosttags=kvm 2>&1 || true)

				if echo "$RESULT4" | grep -q "Error"; then
					echo -e "${RED}All attempts failed.${NC}"
					echo -e "${YELLOW}Detailed error:${NC}"
					echo "$RESULT4"
					exit 1
				else
					echo -e "${GREEN}Successfully added KVM host to CloudStack!${NC}"
					echo "$RESULT4"
				fi
			else
				echo -e "${GREEN}Successfully created primary storage${NC}"
				echo "$STORAGE_RESULT"

				echo -e "${YELLOW}Final attempt after creating storage...${NC}"
				RESULT4=$(bash ./scripts/cmk.sh add host zoneid=$ZONE_ID podid=$POD_ID clusterid=$CLUSTER_ID hypervisor=KVM username=root password=password url=qemu+tcp://$KVM_IP/system hosttags=kvm 2>&1 || true)

				if echo "$RESULT4" | grep -q "Error"; then
					echo -e "${RED}Failed after creating storage${NC}"
					echo -e "${YELLOW}Detailed error:${NC}"
					echo "$RESULT4"
					exit 1
				else
					echo -e "${GREEN}Successfully added KVM host to CloudStack!${NC}"
					echo "$RESULT4"
				fi
			fi
		else
			echo -e "${GREEN}Successfully added KVM host to CloudStack!${NC}"
			echo "$RESULT3"
		fi
	else
		echo -e "${GREEN}Successfully added KVM host to CloudStack!${NC}"
		echo "$RESULT2"
	fi
else
	echo -e "${GREEN}Successfully added KVM host to CloudStack!${NC}"
	echo "$RESULT1"
fi

echo -e "\n${BLUE}=== Next Steps ===${NC}"
echo -e "1. Access CloudStack UI at ${GREEN}http://localhost:8080/client${NC} (admin/password)"
echo -e "2. Check your Host in Infrastructure > Hosts"
echo -e "3. Create VMs using the KVM hypervisor"

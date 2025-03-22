#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}Checking for CloudStack socket connection issues...${NC}"

# Check if CloudStack container is running
if ! docker ps | grep -q cloudstack-simulator; then
	echo -e "${RED}CloudStack simulator is not running.${NC}"
	echo -e "${YELLOW}Please start CloudStack with 'docker-compose up -d' or 'task cloudstack:setup' first.${NC}"
	exit 1
fi

CONTAINER_ID=$(docker ps -q -f name=cloudstack-simulator)

if [ -z "$CONTAINER_ID" ]; then
	echo -e "${RED}Failed to find CloudStack simulator container.${NC}"
	exit 1
fi

echo -e "${YELLOW}Checking CloudStack logs for socket errors...${NC}"

# Look for socket errors in the CloudStack logs
SOCKET_ERRORS=$(docker exec $CONTAINER_ID bash -c "grep -r 'Unable to create socket' /var/log/cloudstack || echo 'No socket errors found'")

if [[ $SOCKET_ERRORS == *"No socket errors found"* ]]; then
	echo -e "${GREEN}No socket errors found in the logs.${NC}"
else
	echo -e "${YELLOW}Socket errors found in the logs:${NC}"
	echo -e "$SOCKET_ERRORS"
fi

echo -e "${YELLOW}Checking for configuration issues with TcpSocketManager in CloudStack...${NC}"

# Check server.properties for socket configuration
SERVER_PROPS=$(docker exec $CONTAINER_ID bash -c "cat /etc/cloudstack/management/server.properties 2>/dev/null || echo 'File not found'")

if [[ $SERVER_PROPS == *"File not found"* ]]; then
	echo -e "${YELLOW}Could not find server.properties file.${NC}"
else
	echo -e "${GREEN}Found server.properties file.${NC}"

	# Check if there are any configuration values for socket binding
	SOCKET_CONFIG=$(docker exec $CONTAINER_ID bash -c "grep -i 'socket\|port\|bind\|tcp' /etc/cloudstack/management/server.properties || echo 'No socket configuration found'")

	if [[ $SOCKET_CONFIG == *"No socket configuration found"* ]]; then
		echo -e "${YELLOW}No specific socket binding configuration found.${NC}"
	else
		echo -e "${YELLOW}Socket configuration found:${NC}"
		echo -e "$SOCKET_CONFIG"
	fi
fi

echo -e "${YELLOW}Checking current listening ports in the container...${NC}"

# Check netstat to see what's listening on port 4560
NETSTAT_OUTPUT=$(docker exec $CONTAINER_ID bash -c "netstat -tuln | grep LISTEN || echo 'No listening ports found'")

if [[ $NETSTAT_OUTPUT == *"No listening ports found"* ]]; then
	echo -e "${YELLOW}No listening ports found in the container.${NC}"
else
	echo -e "${GREEN}Current listening ports:${NC}"
	echo -e "$NETSTAT_OUTPUT"

	if [[ $NETSTAT_OUTPUT == *"4560"* ]]; then
		echo -e "${GREEN}Port 4560 is already in use by an application in the container.${NC}"
	else
		echo -e "${YELLOW}Port 4560 is not currently bound by any application.${NC}"
	fi
fi

echo -e "${YELLOW}Attempting to fix the socket issue...${NC}"

# Check if we're on Mac
if [[ $(uname) == "Darwin" ]]; then
	echo -e "${YELLOW}Running on macOS. Some socket binding issues are common with Docker on Mac.${NC}"
	echo -e "${YELLOW}Checking if port 4560 is already in use on the host...${NC}"

	if lsof -i :4560 >/dev/null; then
		echo -e "${RED}Port 4560 is already in use on your Mac. This might be causing conflicts.${NC}"
		echo -e "${YELLOW}Consider stopping the application using this port or changing the CloudStack configuration.${NC}"
	else
		echo -e "${GREEN}Port 4560 is not in use on your Mac.${NC}"
	fi
fi

# Restart the management server service inside the container
echo -e "${YELLOW}Restarting CloudStack management server...${NC}"
docker exec $CONTAINER_ID bash -c "service cloudstack-management restart || systemctl restart cloudstack-management || echo 'Failed to restart service'"

echo -e "${GREEN}CloudStack management server restart attempted.${NC}"
echo -e "${YELLOW}Wait a few minutes and check if the issue is resolved.${NC}"

echo -e "${YELLOW}Possible solutions if the issue persists:${NC}"
echo -e "1. Modify the CloudStack configuration to bind to a different address:"
echo -e "   e.g., 0.0.0.0 instead of localhost or a specific IP"
echo -e "2. Modify the Docker Compose file to use a different host port mapping for port 4560"
echo -e "3. Check if any firewall rules are blocking the connection"
echo -e "4. Sometimes this error can be safely ignored if CloudStack is still functioning correctly"

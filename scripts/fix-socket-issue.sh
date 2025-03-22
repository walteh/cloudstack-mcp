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

echo -e "${YELLOW}Checking port 4560 inside the container...${NC}"

# Check if port 4560 is in use inside the container
PORT_CHECK=$(docker exec $CONTAINER_ID bash -c "netstat -tuln | grep 4560 || echo 'Port available'")

if [[ $PORT_CHECK == *"Port available"* ]]; then
	echo -e "${GREEN}Port 4560 is available inside the container.${NC}"
else
	echo -e "${YELLOW}Port 4560 is in use inside the container. Details:${NC}"
	echo -e "$PORT_CHECK"
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

echo -e "${YELLOW}Attempting to fix the socket issue...${NC}"

# Restart the management server service inside the container
# This is a generic approach - the actual service name may vary depending on the CloudStack image
echo -e "${YELLOW}Restarting CloudStack management server...${NC}"
docker exec $CONTAINER_ID bash -c "service cloudstack-management restart || systemctl restart cloudstack-management || echo 'Failed to restart service'"

echo -e "${GREEN}CloudStack management server restart attempted.${NC}"
echo -e "${YELLOW}Wait a few minutes and check if the issue is resolved.${NC}"
echo -e "${YELLOW}If the issue persists, you might need to modify the CloudStack configuration to use a different port or address binding.${NC}"

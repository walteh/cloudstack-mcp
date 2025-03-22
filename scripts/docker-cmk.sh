#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Docker CloudMonkey Wrapper ===${NC}"

# Pull the latest CloudMonkey Docker image if it's not already available
DOCKER_CMK_IMAGE="apache/cloudstack-cloudmonkey:latest"
if ! docker image inspect "$DOCKER_CMK_IMAGE" &>/dev/null; then
    echo -e "${YELLOW}Pulling CloudMonkey Docker image...${NC}"
    docker pull "$DOCKER_CMK_IMAGE"
fi

# Create a Docker volume for CMK configuration if it doesn't exist
DOCKER_CMK_VOLUME="cloudmonkey-config"
if ! docker volume inspect "$DOCKER_CMK_VOLUME" &>/dev/null; then
    echo -e "${YELLOW}Creating Docker volume for CloudMonkey configuration...${NC}"
    docker volume create "$DOCKER_CMK_VOLUME"
fi

# Build the Docker command
DOCKER_CMK_CMD="docker run --rm -it"
DOCKER_CMK_CMD+=" --network=host"                   # Use host network to access localhost CloudStack
DOCKER_CMK_CMD+=" -v $DOCKER_CMK_VOLUME:/root/.cmk" # Mount configuration volume

# Setup profile on first run if we have no arguments
if [ "$#" -eq 0 ]; then
    echo -e "${YELLOW}Setting up CloudMonkey profile for localhost...${NC}"

    # First, initialize config with profile and then set configurations
    $DOCKER_CMK_CMD $DOCKER_CMK_IMAGE set profile localhost
    $DOCKER_CMK_CMD $DOCKER_CMK_IMAGE set url http://localhost:8080/client/api
    $DOCKER_CMK_CMD $DOCKER_CMK_IMAGE set username admin
    $DOCKER_CMK_CMD $DOCKER_CMK_IMAGE set password password
    $DOCKER_CMK_CMD $DOCKER_CMK_IMAGE set domain ROOT
    $DOCKER_CMK_CMD $DOCKER_CMK_IMAGE set output json

    # Try to sync the API definitions
    echo -e "${YELLOW}Syncing CloudMonkey API cache...${NC}"
    SYNC_OUTPUT=$($DOCKER_CMK_CMD $DOCKER_CMK_IMAGE sync 2>&1 || echo "Sync failed")

    if [[ "$SYNC_OUTPUT" == *"successfully synchronized"* ]]; then
        echo -e "${GREEN}Successfully synced CloudMonkey with CloudStack API!${NC}"
    else
        echo -e "${RED}Sync failed: $SYNC_OUTPUT${NC}"
        echo -e "${YELLOW}This may happen if CloudStack is not fully initialized yet.${NC}"
        echo -e "${YELLOW}Try running this script with 'sync' argument once CloudStack is ready.${NC}"
    fi

    # Try logging in
    echo -e "${YELLOW}Logging in to CloudStack...${NC}"
    LOGIN_OUTPUT=$($DOCKER_CMK_CMD $DOCKER_CMK_IMAGE login -u admin -p password 2>&1 || echo "Login failed")

    if [[ "$LOGIN_OUTPUT" == *"successfully logged in"* ]]; then
        echo -e "${GREEN}Successfully logged in to CloudStack!${NC}"
    else
        echo -e "${RED}Login failed: $LOGIN_OUTPUT${NC}"
        echo -e "${YELLOW}You may need to wait for CloudStack to fully initialize.${NC}"
    fi

    # Print usage info
    echo -e "\n${BLUE}=== Docker CloudMonkey Usage ===${NC}"
    echo -e "${YELLOW}To run CloudMonkey commands:${NC}"
    echo -e "  ./scripts/docker-cmk.sh [command] [arguments]"
    echo -e "\n${YELLOW}Examples:${NC}"
    echo -e "  ./scripts/docker-cmk.sh sync                       # Sync API cache"
    echo -e "  ./scripts/docker-cmk.sh list zones                 # List zones"
    echo -e "  ./scripts/docker-cmk.sh list serviceofferings      # List service offerings"
    echo -e "  ./scripts/docker-cmk.sh help                       # Show CloudMonkey help"

else
    # If we have arguments, just run the command
    $DOCKER_CMK_CMD $DOCKER_CMK_IMAGE "$@"
fi

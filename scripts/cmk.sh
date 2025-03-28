#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# echo -e "${BLUE}=== CloudMonkey Container Client ===${NC}"

# Check if CloudStack containers are running
if ! docker ps | grep -q cloudstack-mcp-simulator; then
    echo -e "${RED}CloudStack simulator is not running.${NC}"
    echo -e "${YELLOW}Please start CloudStack with 'task docker:start' first.${NC}"
    exit 1
fi

# Check if CloudMonkey container is running
if ! docker ps | grep -q cloudstack-mcp-cmk; then
    echo -e "${RED}CloudMonkey container is not running.${NC}"
    echo -e "${YELLOW}Please start the environment with 'task docker:start' first.${NC}"
    exit 1
fi

# Function to execute commands in the CloudMonkey container
run_cmk() {
    docker exec -it cloudstack-mcp-cmk cmk "$@"
}

# If no arguments are provided, just show usage
if [ "$#" -eq 0 ]; then
    echo -e "${YELLOW}Usage: cmk.sh [command] [arguments]${NC}"
    echo -e "\n${YELLOW}Examples:${NC}"
    echo -e "  ./scripts/cmk.sh sync                       # Sync API cache"
    echo -e "  ./scripts/cmk.sh list zones                 # List zones"
    echo -e "  ./scripts/cmk.sh list serviceofferings      # List service offerings"
    echo -e "  ./scripts/cmk.sh help                       # Show CloudMonkey help"
    echo -e "  ./scripts/cmk.sh api listApis username=admin password=password    # Direct API call with credentials"

    # Display the current profile configuration
    echo -e "\n${YELLOW}Current CloudMonkey profile configuration:${NC}"
    run_cmk set
else
    # Run the command with all arguments
    run_cmk "$@"
fi

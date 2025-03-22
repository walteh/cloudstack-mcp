#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== CloudStack Examples using CloudMonkey ===${NC}"

# Check for cmk (CloudMonkey)
if ! command -v cmk &>/dev/null; then
	echo -e "${RED}CloudMonkey (cmk) not found. Installing it...${NC}"
	go install github.com/apache/cloudstack-cloudmonkey@latest

	if [ -f "$(which cloudstack-cloudmonkey)" ] && [ ! -f "$(dirname $(which cloudstack-cloudmonkey))/cmk" ]; then
		cp "$(which cloudstack-cloudmonkey)" "$(dirname $(which cloudstack-cloudmonkey))/cmk"
		echo -e "${GREEN}CloudMonkey installed and 'cmk' alias created.${NC}"
	else
		echo -e "${RED}Failed to install CloudMonkey. Please install it manually:${NC}"
		echo -e "${YELLOW}go install github.com/apache/cloudstack-cloudmonkey@latest${NC}"
		exit 1
	fi
fi

# Check if CloudStack is running
if ! curl -s --connect-timeout 5 http://localhost:8080/client &>/dev/null; then
	echo -e "${RED}CloudStack management server is not accessible.${NC}"
	echo -e "${YELLOW}Please start CloudStack with 'task docker:start' first.${NC}"
	exit 1
fi

# First, make sure the cache is working correctly
echo -e "${YELLOW}Checking CloudMonkey cache status...${NC}"
if [[ "$(cmk list zones 2>&1)" == *"Failed to read API cache"* ]]; then
	echo -e "${RED}CloudMonkey cache needs to be fixed before running examples.${NC}"
	echo -e "${YELLOW}Running 'task cloudstack:fix-cmk-cache' to fix it...${NC}"
	# Call the fix script
	if [ -f "./scripts/fix-cmk-cache.sh" ]; then
		bash ./scripts/fix-cmk-cache.sh
	else
		echo -e "${RED}Could not find fix-cmk-cache.sh script. Please run:${NC}"
		echo -e "${YELLOW}task cloudstack:fix-cmk-cache${NC}"
		exit 1
	fi
fi

# Set up CloudMonkey profile for localhost
echo -e "${YELLOW}Setting up CloudMonkey profile for localhost...${NC}"
CMK_CONFIG_DIR="$HOME/.cmk"
mkdir -p "$CMK_CONFIG_DIR"

# First try authenticating with cached credentials
cmk set profile localhost
cmk set url http://localhost:8080/client/api
cmk set username admin
cmk set password password
cmk set domain ROOT
cmk set output json

# Try authenticating
echo -e "${YELLOW}Authenticating with CloudStack...${NC}"
LOGIN_RESULT=$(cmk login -u admin -p password 2>&1) || true

if [[ "$LOGIN_RESULT" == *"successfully logged in"* ]]; then
	echo -e "${GREEN}Successfully logged in to CloudStack.${NC}"
elif [[ "$LOGIN_RESULT" == *"Failed to read API cache"* ]]; then
	echo -e "${RED}API cache issue detected. Please run 'task cloudstack:fix-cmk-cache' first.${NC}"
	exit 1
else
	echo -e "${RED}Failed to log in to CloudStack: $LOGIN_RESULT${NC}"
	echo -e "${YELLOW}This may indicate that CloudStack is not fully initialized yet.${NC}"
	echo -e "${YELLOW}Try accessing the web UI at http://localhost:8080/client first.${NC}"
	echo -e "${YELLOW}Default credentials: admin/password${NC}"

	# Try a direct API call to check if the server is responding
	echo -e "${YELLOW}Trying a direct API call...${NC}"
	DIRECT_RESULT=$(curl -s "http://localhost:8080/client/api?command=list&response=json&listApis=true&username=admin&password=password")

	if [[ "$DIRECT_RESULT" == *"apiList"* ]]; then
		echo -e "${GREEN}API server is responding. CloudMonkey configuration may be incorrect.${NC}"
		echo -e "${YELLOW}Please run 'task cloudstack:fix-cmk-cache' to fix CloudMonkey.${NC}"
	else
		echo -e "${RED}API server is not responding correctly. CloudStack may not be fully initialized.${NC}"
		echo -e "${YELLOW}Please check CloudStack logs and wait for it to complete initialization.${NC}"
	fi

	exit 1
fi

echo -e "${BLUE}=== Examples of CloudMonkey Commands ===${NC}"

# List available zones
echo -e "${YELLOW}Listing zones...${NC}"
cmk list zones

# List service offerings
echo -e "\n${YELLOW}Listing service offerings...${NC}"
cmk list serviceofferings

# List templates
echo -e "\n${YELLOW}Listing templates...${NC}"
cmk list templates templatefilter=featured

# List virtual machines (if any)
echo -e "\n${YELLOW}Listing virtual machines...${NC}"
cmk list virtualmachines

# Displaying your CloudStack configuration
echo -e "\n${YELLOW}Your CloudStack configuration:${NC}"
cmk set

# List users
echo -e "\n${YELLOW}Listing users...${NC}"
cmk list users

# Show API key for admin user
echo -e "\n${YELLOW}Checking API keys for admin user...${NC}"
ADMIN_USER=$(cmk list users name=admin)
API_KEY=$(echo "$ADMIN_USER" | grep -o '"apikey": *"[^"]*"' | cut -d'"' -f4)

if [ -z "$API_KEY" ] || [ "$API_KEY" == "null" ]; then
	echo -e "${RED}Admin user doesn't have API keys.${NC}"
	echo -e "${YELLOW}You can generate API keys with: cmk register -u admin -p password${NC}"
else
	echo -e "${GREEN}API key exists for admin user.${NC}"
	echo -e "${YELLOW}You can view it by examining the output of: cmk list users name=admin${NC}"
fi

# Show help for common commands
echo -e "\n${BLUE}=== CloudMonkey Help ===${NC}"
echo -e "${YELLOW}To see all available commands:${NC} cmk help"
echo -e "${YELLOW}To see details about a specific command:${NC} cmk help <command>"
echo -e "${YELLOW}For example:${NC} cmk help deployVirtualMachine"

echo -e "\n${BLUE}=== Troubleshooting Tips ===${NC}"
echo -e "${YELLOW}If you encounter API cache errors:${NC} task cloudstack:fix-cmk-cache"
echo -e "${YELLOW}To get API credentials:${NC} task cloudstack:get-credentials"
echo -e "${YELLOW}To check CloudStack status:${NC} task cloudstack:status"
echo -e "${YELLOW}To login to the UI:${NC} http://localhost:8080/client (admin/password)"

echo -e "\n${GREEN}Examples completed.${NC}"

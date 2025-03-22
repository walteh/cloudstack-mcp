#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Retrieving CloudStack API Credentials using Docker CloudMonkey ===${NC}"

# Check if CloudStack is running
if ! docker ps | grep -q cloudstack-simulator; then
	echo -e "${RED}CloudStack simulator is not running.${NC}"
	echo -e "${YELLOW}Please start CloudStack with 'task docker:start' first.${NC}"
	exit 1
fi

# Check if CloudMonkey container is running
if ! docker ps | grep -q cloudstack-cloudmonkey; then
	echo -e "${RED}CloudMonkey container is not running.${NC}"
	echo -e "${YELLOW}Please start the environment with 'task docker:start' first.${NC}"
	exit 1
fi

# Function to execute commands in the CloudMonkey container
run_cmk() {
	docker exec -i cloudstack-cloudmonkey cmk -d "$@"
}

echo -e "${YELLOW}Waiting for CloudStack to be fully initialized...${NC}"
MAX_ATTEMPTS=30
attempt=0
while ! curl -s --connect-timeout 5 http://localhost:8080/client >/dev/null; do
	attempt=$((attempt + 1))
	if [ $attempt -gt $MAX_ATTEMPTS ]; then
		echo -e "${RED}CloudStack did not become available after $MAX_ATTEMPTS attempts.${NC}"
		echo -e "${YELLOW}Please check the CloudStack container logs for errors.${NC}"
		exit 1
	fi
	echo -e "${YELLOW}CloudStack is not ready yet, waiting... (attempt $attempt/$MAX_ATTEMPTS)${NC}"
	sleep 10
done

echo -e "${GREEN}CloudStack is running. Setting up CloudMonkey profile...${NC}"

# Ensure profile is set up correctly
echo -e "${YELLOW}Configuring CloudMonkey profile...${NC}"
run_cmk set profile localcloud
run_cmk set url http://cloudstack:8080/client/api
run_cmk set username admin
run_cmk set password password
run_cmk set domain ""
run_cmk set output json

# Sync API definitions
echo -e "${YELLOW}Syncing CloudMonkey API cache...${NC}"
SYNC_OUTPUT=$(run_cmk sync 2>&1) || true
if [[ "$SYNC_OUTPUT" == *"successfully synchronized"* ]]; then
	echo -e "${GREEN}Successfully synced CloudMonkey API cache.${NC}"
elif [[ "$SYNC_OUTPUT" == *"failed to authenticate"* ]]; then
	echo -e "${RED}Authentication error during sync: $SYNC_OUTPUT${NC}"
	# If sync fails with authentication error, try direct API call
	echo -e "${YELLOW}Trying direct API call...${NC}"
	DIRECT_OUTPUT=$(run_cmk api listApis username=admin password=password 2>&1) || true
	if [[ "$DIRECT_OUTPUT" == *"count"* ]]; then
		echo -e "${GREEN}Successfully accessed API directly.${NC}"
	else
		echo -e "${RED}Direct API call failed: $DIRECT_OUTPUT${NC}"
		echo -e "${YELLOW}CloudStack might not be fully initialized yet.${NC}"
	fi
else
	echo -e "${YELLOW}Sync warning: $SYNC_OUTPUT${NC}"
	echo -e "${YELLOW}Continuing anyway...${NC}"
fi

# Try to log in
echo -e "${YELLOW}Logging in to CloudStack...${NC}"
LOGIN_OUTPUT=$(run_cmk login username=admin password=password 2>&1) || true
if [[ "$LOGIN_OUTPUT" == *"successfully logged in"* ]]; then
	echo -e "${GREEN}Successfully logged in to CloudStack.${NC}"
else
	echo -e "${YELLOW}Login warning: $LOGIN_OUTPUT${NC}"
	echo -e "${YELLOW}Continuing anyway...${NC}"
fi

# Check if admin user already has keys
echo -e "${YELLOW}Checking if admin user already has API keys...${NC}"
USERS_JSON=$(run_cmk list users name=admin 2>/dev/null || run_cmk api listUsers username=admin password=password name=admin 2>/dev/null || echo "{}")

# Extract API key and secret key from the JSON response
API_KEY=$(echo "$USERS_JSON" | grep -o '"apikey": *"[^"]*"' | cut -d'"' -f4)
SECRET_KEY=$(echo "$USERS_JSON" | grep -o '"secretkey": *"[^"]*"' | cut -d'"' -f4)

if [ -z "$API_KEY" ] || [ "$API_KEY" == "null" ]; then
	echo -e "${YELLOW}Admin user doesn't have API keys. Generating keys...${NC}"
	REGISTER_OUTPUT=$(run_cmk register -u admin -p password 2>&1) || true

	if [[ "$REGISTER_OUTPUT" == *"apikey"* ]]; then
		# Extract the newly generated keys
		API_KEY=$(echo "$REGISTER_OUTPUT" | grep -o '"apikey": *"[^"]*"' | cut -d'"' -f4)
		SECRET_KEY=$(echo "$REGISTER_OUTPUT" | grep -o '"secretkey": *"[^"]*"' | cut -d'"' -f4)
		echo -e "${GREEN}Successfully registered API keys.${NC}"
	else
		echo -e "${RED}Failed to register API keys: $REGISTER_OUTPUT${NC}"
		echo -e "${YELLOW}Trying alternative method...${NC}"

		# Try with direct API call
		USER_ID=$(echo "$USERS_JSON" | grep -o '"id": *"[^"]*"' | head -1 | cut -d'"' -f4 || echo "")
		if [ -z "$USER_ID" ]; then
			echo -e "${RED}Failed to get user ID.${NC}"
			exit 1
		fi

		GENERATE_OUTPUT=$(run_cmk api generateUserKeys username=admin password=password id=$USER_ID 2>&1) || true

		if [[ "$GENERATE_OUTPUT" == *"apikey"* ]]; then
			# Extract the keys from the result
			API_KEY=$(echo "$GENERATE_OUTPUT" | grep -o '"apikey": *"[^"]*"' | cut -d'"' -f4)
			SECRET_KEY=$(echo "$GENERATE_OUTPUT" | grep -o '"secretkey": *"[^"]*"' | cut -d'"' -f4)
			echo -e "${GREEN}Successfully generated API keys.${NC}"
		else
			echo -e "${RED}All methods to generate API keys failed: $GENERATE_OUTPUT${NC}"
			exit 1
		fi
	fi
fi

if [ -z "$API_KEY" ] || [ -z "$SECRET_KEY" ]; then
	echo -e "${RED}Failed to retrieve CloudStack API credentials.${NC}"
	exit 1
fi

echo -e "${GREEN}Successfully retrieved CloudStack API credentials.${NC}"
echo -e "${YELLOW}API Key: ${API_KEY}${NC}"
echo -e "${YELLOW}Secret Key: ${SECRET_KEY}${NC}"

# Create .env file with the credentials
cat >.env <<EOF
CLOUDSTACK_API_URL=http://localhost:8080/client/api
CLOUDSTACK_API_KEY=${API_KEY}
CLOUDSTACK_SECRET_KEY=${SECRET_KEY}
EOF

echo -e "${GREEN}Credentials saved to .env file.${NC}"
echo -e "${YELLOW}You can now use 'source .env' to load these credentials into your environment.${NC}"

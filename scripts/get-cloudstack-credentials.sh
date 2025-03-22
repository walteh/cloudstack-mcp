#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}Retrieving CloudStack API credentials...${NC}"

# Check if CloudStack is running
if ! docker ps | grep -q cloudstack-simulator; then
	echo -e "${RED}CloudStack simulator is not running.${NC}"
	echo -e "${YELLOW}Please start CloudStack with 'docker-compose up -d' or 'task docker:start' first.${NC}"
	exit 1
fi

# Check for cmk (CloudMonkey)
if ! command -v cmk &>/dev/null; then
	echo -e "${RED}CloudMonkey (cmk) not found. Please install it with:${NC}"
	echo -e "${YELLOW}go install github.com/apache/cloudstack-cloudmonkey@latest && mv \$(which cloudstack-cloudmonkey) \$(dirname \$(which cloudstack-cloudmonkey))/cmk${NC}"
	exit 1
fi

# Wait for CloudStack to be ready
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

# Set up CloudMonkey profile for localhost
cmk set profile localhost
cmk set url http://localhost:8080/client/api
cmk set username admin
cmk set password password
cmk set domain ROOT
cmk set output json

# Try to login first before syncing
echo -e "${YELLOW}Attempting to login to CloudStack...${NC}"
LOGIN_OUTPUT=$(cmk login -u admin -p password 2>&1) || true
if [[ "$LOGIN_OUTPUT" == *"successfully logged in"* ]]; then
	echo -e "${GREEN}Successfully logged in to CloudStack.${NC}"
else
	echo -e "${YELLOW}Login attempt result: $LOGIN_OUTPUT${NC}"
	echo -e "${YELLOW}Will continue anyway and try to sync...${NC}"
fi

echo -e "${YELLOW}Syncing CloudMonkey API cache with CloudStack...${NC}"
# Sync CloudMonkey's API definitions with the server
# This fixes the "Failed to read API cache, please run 'sync'" error
SYNC_OUTPUT=$(cmk sync 2>&1) || true
if [[ "$SYNC_OUTPUT" == *"successfully synchronized"* ]]; then
	echo -e "${GREEN}Successfully synced CloudMonkey with CloudStack API!${NC}"
elif [[ "$SYNC_OUTPUT" == *"failed to authenticate"* ]]; then
	echo -e "${RED}Authentication error during sync: $SYNC_OUTPUT${NC}"
	echo -e "${YELLOW}Trying alternative approach...${NC}"

	# Try using the CloudMonkey API call directly with credentials
	echo -e "${YELLOW}Attempting direct API call to sync...${NC}"
	DIRECT_SYNC=$(cmk api listApis username=admin password=password 2>&1) || true

	if [[ "$DIRECT_SYNC" == *"count"* ]]; then
		echo -e "${GREEN}Successfully retrieved API list directly!${NC}"
		echo -e "${YELLOW}This should have implicitly synced the API cache.${NC}"
	else
		echo -e "${RED}Direct API call failed: $DIRECT_SYNC${NC}"
		echo -e "${YELLOW}Will try manual approach...${NC}"

		# Try manual cache creation approach
		CMK_CACHE_DIR="$HOME/.cmk"
		mkdir -p "$CMK_CACHE_DIR/cache/localhost"

		# Get API list directly via curl and save it
		API_LIST=$(curl -s -X POST "http://localhost:8080/client/api?command=listApis&response=json&username=admin&password=password")

		if [[ "$API_LIST" == *"apiList"* ]]; then
			echo "$API_LIST" >"$CMK_CACHE_DIR/cache/localhost/apis.json"
			echo -e "${GREEN}Manually created API cache file.${NC}"
		else
			echo -e "${RED}Failed to retrieve API list directly via curl.${NC}"
			echo -e "${YELLOW}You may need to log in to the web UI first at http://localhost:8080/client${NC}"
			echo -e "${YELLOW}Default credentials: admin/password${NC}"
		fi
	fi
else
	echo -e "${YELLOW}Initial sync failed: $SYNC_OUTPUT${NC}"
	echo -e "${YELLOW}Waiting for CloudStack to fully initialize and trying again...${NC}"
	sleep 20 # Give it some more time to initialize

	SYNC_OUTPUT=$(cmk sync 2>&1) || true
	if [[ "$SYNC_OUTPUT" == *"successfully synchronized"* ]]; then
		echo -e "${GREEN}Successfully synced CloudMonkey with CloudStack API on second attempt!${NC}"
	else
		echo -e "${RED}Failed to sync CloudMonkey with CloudStack API.${NC}"
		echo -e "${RED}Error: $SYNC_OUTPUT${NC}"
		echo -e "${YELLOW}Continuing anyway, some commands might not work properly.${NC}"
	fi
fi

echo -e "${GREEN}CloudMonkey profile set up. Retrieving API credentials...${NC}"

# Login to get the session key
SESSION_RESPONSE=$(cmk login -u admin -p password 2>/dev/null || echo "Login failed")

if [[ $SESSION_RESPONSE == *"Login failed"* ]]; then
	echo -e "${RED}Failed to log in to CloudStack with admin/password.${NC}"
	echo -e "${YELLOW}You may need to log in to the web UI first at http://localhost:8080/client${NC}"
	exit 1
fi

# Use admin account to register API keys
echo -e "${GREEN}Getting API keys for admin user...${NC}"

# Try a direct API call with explicit credentials to verify API access
echo -e "${YELLOW}Verifying API access...${NC}"
API_TEST=$(cmk api listAccounts username=admin password=password name=admin 2>/dev/null || echo "API access failed")
if [[ $API_TEST == *"API access failed"* ]]; then
	echo -e "${RED}Failed to access CloudStack API with direct credentials.${NC}"
	# Try a regular API call
	API_TEST=$(cmk list accounts name=admin 2>/dev/null || echo "API access failed")
	if [[ $API_TEST == *"API access failed"* ]]; then
		echo -e "${RED}Failed to access CloudStack API. CloudStack might not be fully initialized yet.${NC}"
		echo -e "${YELLOW}Please wait a few more minutes and try again.${NC}"
		exit 1
	fi
fi

# Check if admin user already has keys
USERS_JSON=$(cmk list users name=admin 2>/dev/null || cmk api listUsers username=admin password=password name=admin 2>/dev/null || echo "{}")

# Extract API key and secret key from the JSON response
API_KEY=$(echo "$USERS_JSON" | grep -o '"apikey": *"[^"]*"' | cut -d'"' -f4)
SECRET_KEY=$(echo "$USERS_JSON" | grep -o '"secretkey": *"[^"]*"' | cut -d'"' -f4)

if [ -z "$API_KEY" ] || [ "$API_KEY" == "null" ]; then
	echo -e "${YELLOW}Admin user doesn't have API keys. Generating keys...${NC}"
	REGISTER_RESPONSE=$(cmk register -u admin -p password 2>/dev/null || echo "Registration failed")

	if [[ $REGISTER_RESPONSE == *"Registration failed"* ]]; then
		echo -e "${RED}Failed to register API keys.${NC}"
		echo -e "${YELLOW}Trying alternative method...${NC}"

		# Try with direct API call to create user keys with explicit credentials
		USER_ID=$(echo "$USERS_JSON" | grep -o '"id": *"[^"]*"' | head -1 | cut -d'"' -f4 || echo "")
		if [ -z "$USER_ID" ]; then
			echo -e "${RED}Failed to get user ID.${NC}"
			exit 1
		fi

		RESULT=$(cmk api generateUserKeys username=admin password=password id=$USER_ID 2>/dev/null || echo "Key generation failed")

		if [[ $RESULT == *"Key generation failed"* ]]; then
			echo -e "${RED}All methods to generate API keys failed.${NC}"
			exit 1
		fi

		# Extract the keys from the result
		API_KEY=$(echo "$RESULT" | grep -o '"apikey": *"[^"]*"' | cut -d'"' -f4)
		SECRET_KEY=$(echo "$RESULT" | grep -o '"secretkey": *"[^"]*"' | cut -d'"' -f4)
	else
		# Extract the newly generated keys
		API_KEY=$(echo "$REGISTER_RESPONSE" | grep -o '"apikey": *"[^"]*"' | cut -d'"' -f4)
		SECRET_KEY=$(echo "$REGISTER_RESPONSE" | grep -o '"secretkey": *"[^"]*"' | cut -d'"' -f4)
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

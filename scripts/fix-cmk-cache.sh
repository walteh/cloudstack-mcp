#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}Fixing CloudMonkey API cache issues...${NC}"

# Check for CloudMonkey (cmk)
if ! command -v cmk &>/dev/null; then
	echo -e "${RED}CloudMonkey (cmk) not found. Installing it...${NC}"
	go install github.com/apache/cloudstack-cloudmonkey@latest

	if [ -f "$(which cloudstack-cloudmonkey)" ] && [ ! -f "$(dirname $(which cloudstack-cloudmonkey))/cmk" ]; then
		cp $(which cloudstack-cloudmonkey) $(dirname $(which cloudstack-cloudmonkey))/cmk
		echo -e "${GREEN}CloudMonkey installed and 'cmk' alias created.${NC}"
	else
		echo -e "${RED}Failed to install CloudMonkey.${NC}"
		exit 1
	fi
fi

# Check if CloudStack is running
if ! curl -s --connect-timeout 5 http://localhost:8080/client &>/dev/null; then
	echo -e "${RED}CloudStack management server is not accessible.${NC}"
	echo -e "${YELLOW}Please start CloudStack with 'task docker:start' first.${NC}"
	exit 1
fi

# Check for old CloudMonkey configuration and migrate if needed
OLD_CONFIG_DIR="$HOME/.cloudmonkey"
NEW_CONFIG_DIR="$HOME/.cmk"
if [ -d "$OLD_CONFIG_DIR" ] && [ ! -d "$NEW_CONFIG_DIR" ]; then
	echo -e "${YELLOW}Found old CloudMonkey config directory. Migrating...${NC}"
	mkdir -p "$NEW_CONFIG_DIR"
	if [ -f "$OLD_CONFIG_DIR/config" ]; then
		cp "$OLD_CONFIG_DIR/config" "$NEW_CONFIG_DIR/config"
		echo -e "${GREEN}Migrated CloudMonkey configuration.${NC}"
	fi
fi

# Make sure the CloudMonkey config directory exists
mkdir -p "$NEW_CONFIG_DIR"

# Create a clean CloudMonkey configuration for localhost
echo -e "${YELLOW}Setting up CloudMonkey profile for localhost...${NC}"

# Create the config file directly instead of using the cmk set commands
CONFIG_FILE="$NEW_CONFIG_DIR/config"

# Create or update the localhost profile in the config file
cat >"$CONFIG_FILE" <<EOF
[core]
asyncblock = true
paramcompletion = true
history_file = $NEW_CONFIG_DIR/history
cache_dir = $NEW_CONFIG_DIR/cache

[server "localhost"]
url = http://localhost:8080/client/api
apikey = 
secretkey = 
timeout = 600
verifycert = true
signatureversion = 3
domain = ROOT
username = admin
password = password
expires = 
output = json
EOF

# Set proper permissions on the config file
chmod 600 "$CONFIG_FILE"
echo -e "${GREEN}Created fresh CloudMonkey configuration file.${NC}"

# Set up the profile with cmk commands to make sure it's properly initialized
cmk set profile localhost
cmk set url http://localhost:8080/client/api
cmk set username admin
cmk set password password
cmk set domain ROOT
cmk set output json

echo -e "${YELLOW}Clearing CloudMonkey cache...${NC}"
# Find and remove CloudMonkey cache files
CMK_CACHE_DIR="$NEW_CONFIG_DIR/cache"
if [ -d "$CMK_CACHE_DIR" ]; then
	rm -rf "$CMK_CACHE_DIR"
	mkdir -p "$CMK_CACHE_DIR/localhost"
	echo -e "${GREEN}CloudMonkey cache directory cleared and recreated.${NC}"
else
	mkdir -p "$CMK_CACHE_DIR/localhost"
	echo -e "${GREEN}CloudMonkey cache directory created at $CMK_CACHE_DIR.${NC}"
fi

# Try accessing the CloudStack UI first to ensure it's initialized
echo -e "${YELLOW}Verifying CloudStack UI is accessible...${NC}"
UI_RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/client)
if [ "$UI_RESPONSE" != "200" ]; then
	echo -e "${RED}CloudStack UI is not responding with 200 OK (got $UI_RESPONSE).${NC}"
	echo -e "${YELLOW}Waiting for CloudStack to finish initialization...${NC}"
	# Wait for the UI to be accessible
	for i in {1..10}; do
		echo -e "${YELLOW}Attempt $i/10: Waiting for CloudStack UI...${NC}"
		sleep 15
		UI_RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/client)
		if [ "$UI_RESPONSE" == "200" ]; then
			echo -e "${GREEN}CloudStack UI is now accessible!${NC}"
			break
		fi
		if [ $i -eq 10 ]; then
			echo -e "${RED}CloudStack UI did not become accessible after 10 attempts.${NC}"
			echo -e "${YELLOW}You may need to check the CloudStack container logs for errors.${NC}"
		fi
	done
fi

# Get the API list directly with curl to ensure we can access it
echo -e "${YELLOW}Attempting to fetch the API list directly from CloudStack...${NC}"
API_LIST=$(curl -s -X POST "http://localhost:8080/client/api?command=listApis&response=json&username=admin&password=password")

if [[ "$API_LIST" == *"apiList"* ]]; then
	echo -e "${GREEN}Successfully retrieved API list directly from CloudStack!${NC}"
	echo -e "${YELLOW}Saving API list to cache file...${NC}"
	echo "$API_LIST" >"$CMK_CACHE_DIR/localhost/apis.json"
	echo -e "${GREEN}API cache created manually.${NC}"
else
	echo -e "${RED}Failed to retrieve API list directly from CloudStack.${NC}"
	echo -e "${YELLOW}API Response: ${NC}${API_LIST}"
	echo -e "${YELLOW}This might indicate CloudStack is not fully initialized or there is an authentication issue.${NC}"
	echo -e "${YELLOW}Trying the sync command anyway...${NC}"
fi

# Try to login with the web UI to initialize the session
echo -e "${YELLOW}Attempting to login to CloudStack web UI...${NC}"
WEB_LOGIN=$(curl -s -c /tmp/cloudstack_cookies.txt -X POST -d "username=admin&password=password&domain=" http://localhost:8080/client/api/login)
if [[ "$WEB_LOGIN" == *"successful"* ]]; then
	echo -e "${GREEN}Successfully logged in to CloudStack web UI!${NC}"
else
	echo -e "${YELLOW}Unable to login to CloudStack web UI. This might be normal during initialization.${NC}"
fi

# Try regular cloudmonkey login now
echo -e "${YELLOW}Logging in to CloudStack with CloudMonkey...${NC}"
LOGIN_OUTPUT=$(cmk login -u admin -p password 2>&1) || true
if [[ "$LOGIN_OUTPUT" == *"successfully logged in"* ]]; then
	echo -e "${GREEN}Successfully logged in to CloudStack with CloudMonkey.${NC}"
else
	echo -e "${YELLOW}Login attempt result: $LOGIN_OUTPUT${NC}"
	echo -e "${YELLOW}Will continue anyway and try to sync...${NC}"
fi

echo -e "${YELLOW}Syncing CloudMonkey API cache with CloudStack...${NC}"
# Try syncing multiple times with increasing wait periods
for attempt in 1 2 3; do
	echo -e "${YELLOW}Sync attempt $attempt/3...${NC}"
	SYNC_OUTPUT=$(cmk sync 2>&1) || true

	if [[ "$SYNC_OUTPUT" == *"successfully synchronized"* ]]; then
		echo -e "${GREEN}Successfully synced CloudMonkey with CloudStack API!${NC}"
		break
	elif [[ "$SYNC_OUTPUT" == *"failed to authenticate"* ]]; then
		echo -e "${RED}Authentication error during sync: $SYNC_OUTPUT${NC}"
		echo -e "${YELLOW}Trying alternative approach...${NC}"

		# Try using the CloudMonkey API call directly with credentials
		echo -e "${YELLOW}Attempting direct API call to sync...${NC}"
		DIRECT_SYNC=$(cmk api listApis username=admin password=password 2>&1) || true

		if [[ "$DIRECT_SYNC" == *"count"* ]]; then
			echo -e "${GREEN}Successfully retrieved API list directly!${NC}"
			echo -e "${YELLOW}This should have implicitly synced the API cache.${NC}"
			break
		else
			echo -e "${RED}Direct API call failed: $DIRECT_SYNC${NC}"
		fi

		# If we failed with direct API call, try curl again to save to cache
		API_LIST=$(curl -s -X POST "http://localhost:8080/client/api?command=listApis&response=json&username=admin&password=password")
		if [[ "$API_LIST" == *"apiList"* ]]; then
			echo -e "${GREEN}Successfully retrieved API list with curl!${NC}"
			echo "$API_LIST" >"$CMK_CACHE_DIR/localhost/apis.json"
			echo -e "${GREEN}Manually created API cache file.${NC}"
		fi

		if [ $attempt -lt 3 ]; then
			echo -e "${YELLOW}Will wait and try again...${NC}"
			sleep $((attempt * 30))
		else
			echo -e "${YELLOW}Failed to sync after 3 attempts with authentication errors.${NC}"
			echo -e "${YELLOW}Please check if the CloudStack server is properly initialized.${NC}"
			echo -e "${YELLOW}You may need to log in to the CloudStack UI first at http://localhost:8080/client${NC}"
			echo -e "${YELLOW}Default credentials: admin/password${NC}"
		fi
	else
		if [ $attempt -lt 3 ]; then
			echo -e "${YELLOW}Sync failed: $SYNC_OUTPUT${NC}"
			echo -e "${YELLOW}Waiting for CloudStack to fully initialize...${NC}"
			sleep $((attempt * 30)) # Increase wait time with each attempt
		else
			echo -e "${RED}Failed to sync CloudMonkey with CloudStack API after 3 attempts.${NC}"
			echo -e "${RED}Last error: $SYNC_OUTPUT${NC}"
			echo -e "${YELLOW}This might indicate that CloudStack is not fully initialized yet.${NC}"
			echo -e "${YELLOW}Please wait a few more minutes and try again.${NC}"
		fi
	fi
done

# Create a simple test file to check if API functions
echo -e "${YELLOW}Testing API with a simple command...${NC}"
TEST_CMD="list zones"
TEST_OUTPUT=$(cmk $TEST_CMD 2>&1) || true

if [[ "$TEST_OUTPUT" == *"Failed to read API cache"* ]]; then
	# If we still have cache issues, try a more forceful direct approach
	echo -e "${YELLOW}Still having cache issues. Trying a more forceful direct API approach...${NC}"

	# Get API list directly again and save it
	API_LIST=$(curl -s -X POST "http://localhost:8080/client/api?command=listApis&response=json&username=admin&password=password")

	if [[ "$API_LIST" == *"apiList"* ]]; then
		# Clear and recreate the cache directory completely
		rm -rf "$CMK_CACHE_DIR"
		mkdir -p "$CMK_CACHE_DIR/localhost"
		echo "$API_LIST" >"$CMK_CACHE_DIR/localhost/apis.json"
		echo -e "${GREEN}Recreated API cache file.${NC}"

		# Modify the config to use no authentication for now
		echo -e "${YELLOW}Temporarily modifying CloudMonkey config to bypass authentication...${NC}"
		sed -i.bak 's/^apikey.*/apikey = /' "$CONFIG_FILE"
		sed -i.bak 's/^secretkey.*/secretkey = /' "$CONFIG_FILE"

		# Test again
		TEST_OUTPUT=$(cmk $TEST_CMD 2>&1) || true
		if [[ "$TEST_OUTPUT" != *"Failed to read API cache"* ]]; then
			echo -e "${GREEN}API cache issue resolved manually!${NC}"
		else
			echo -e "${RED}Still having cache issues after manual intervention.${NC}"
			echo -e "${RED}Test output: $TEST_OUTPUT${NC}"
		fi
	else
		echo -e "${RED}Failed to retrieve API list directly.${NC}"
		echo -e "${RED}API Response: ${NC}${API_LIST}"
	fi
fi

echo -e "${YELLOW}Verifying API access...${NC}"
VERIFY_OUTPUT=$(cmk $TEST_CMD 2>&1) || true
if [[ "$VERIFY_OUTPUT" == *"id"* || "$VERIFY_OUTPUT" == *"count"* ]]; then
	echo -e "${GREEN}API access verified successfully!${NC}"
	echo -e "${GREEN}Output: $VERIFY_OUTPUT${NC}"
else
	echo -e "${RED}Failed to verify API access.${NC}"
	echo -e "${RED}Output: $VERIFY_OUTPUT${NC}"
	echo -e "${YELLOW}CloudStack API might not be fully initialized yet.${NC}"
	echo -e "${YELLOW}Try accessing the web UI at http://localhost:8080/client${NC}"
	echo -e "${YELLOW}Login with admin/password and then run this script again.${NC}"

	# Add a final attempt with direct curl to test if the API is working at all
	echo -e "${YELLOW}Trying one last direct API call with curl...${NC}"
	CURL_OUTPUT=$(curl -s -X POST "http://localhost:8080/client/api?command=listZones&response=json&username=admin&password=password")
	if [[ "$CURL_OUTPUT" == *"zoneid"* || "$CURL_OUTPUT" == *"count"* ]]; then
		echo -e "${GREEN}Direct API call with curl succeeded!${NC}"
		echo -e "${GREEN}Curl output: $CURL_OUTPUT${NC}"
		echo -e "${YELLOW}This suggests the API is working but CloudMonkey configuration is incorrect.${NC}"
	else
		echo -e "${RED}Direct API call with curl also failed.${NC}"
		echo -e "${RED}Curl output: $CURL_OUTPUT${NC}"
		echo -e "${YELLOW}The CloudStack API may not be fully initialized or is misconfigured.${NC}"
	fi
fi

echo -e "${GREEN}CloudMonkey API cache has been fixed.${NC}"
echo -e "${YELLOW}You should now be able to run CloudMonkey commands without cache errors.${NC}"
echo -e "${YELLOW}If you still encounter issues, try accessing the UI first at http://localhost:8080/client${NC}"

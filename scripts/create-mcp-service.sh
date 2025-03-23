#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Creating MCP Service Account ===${NC}"

# Check if CloudMonkey container is running
if ! docker ps | grep -q cloudstack-cloudmonkey; then
    echo -e "${RED}CloudMonkey container is not running.${NC}"
    echo -e "${YELLOW}Please start the environment with 'task docker:start' first.${NC}"
    exit 1
fi

# Function to execute commands in the CloudMonkey container
run_cmk() {
    docker exec -i cloudstack-cloudmonkey cmk "$@"
}

# First, try login with admin
echo -e "${YELLOW}Logging in as admin...${NC}"
LOGIN_OUTPUT=$(run_cmk login -u admin -p password 2>&1) || true
if [[ ! "$LOGIN_OUTPUT" == *"successfully logged in"* ]]; then
    echo -e "${RED}Admin login failed: $LOGIN_OUTPUT${NC}"
    echo -e "${YELLOW}Trying to continue anyway...${NC}"
fi

# Check if 'mcp-service' user already exists
echo -e "${YELLOW}Checking if MCP service account already exists...${NC}"
USERS_JSON=$(run_cmk list users username=mcp-service 2>/dev/null || echo "{}")
if echo "$USERS_JSON" | grep -q "mcp-service"; then
    echo -e "${GREEN}MCP service account already exists.${NC}"

    # Check if it has API keys
    API_KEY=$(echo "$USERS_JSON" | grep -o '"apikey": *"[^"]*"' | cut -d'"' -f4)
    if [ -z "$API_KEY" ] || [ "$API_KEY" = "null" ]; then
        echo -e "${YELLOW}MCP service account has no API keys. Generating...${NC}"
        USER_ID=$(echo "$USERS_JSON" | grep -o '"id": *"[^"]*"' | head -1 | cut -d'"' -f4)

        if [ -n "$USER_ID" ]; then
            # Generate keys for service account
            echo -e "${YELLOW}Generating API keys for user ID: $USER_ID...${NC}"
            REGISTER_OUTPUT=$(run_cmk api generateUserKeys id=$USER_ID 2>/dev/null || echo "{}")

            # Extract keys
            API_KEY=$(echo "$REGISTER_OUTPUT" | grep -o '"apikey": *"[^"]*"' | cut -d'"' -f4)
            SECRET_KEY=$(echo "$REGISTER_OUTPUT" | grep -o '"secretkey": *"[^"]*"' | cut -d'"' -f4)

            if [ -n "$API_KEY" ] && [ -n "$SECRET_KEY" ]; then
                echo -e "${GREEN}Successfully generated API keys for MCP service account.${NC}"
                # Write the keys to an environment file
                mkdir -p ./mcp-credentials
                echo "CLOUDSTACK_API_KEY=$API_KEY" >./mcp-credentials/mcp-credentials.env
                echo "CLOUDSTACK_SECRET_KEY=$SECRET_KEY" >>./mcp-credentials/mcp-credentials.env
                echo -e "${GREEN}MCP service account credentials saved to ./mcp-credentials/mcp-credentials.env${NC}"
            else
                echo -e "${RED}Failed to extract API keys: $REGISTER_OUTPUT${NC}"
            fi
        else
            echo -e "${RED}Could not find user ID for MCP service account.${NC}"
        fi
    else
        echo -e "${GREEN}MCP service account already has API keys.${NC}"
        SECRET_KEY=$(echo "$USERS_JSON" | grep -o '"secretkey": *"[^"]*"' | cut -d'"' -f4)

        # Write the keys to an environment file
        mkdir -p ./mcp-credentials
        echo "CLOUDSTACK_API_KEY=$API_KEY" >./mcp-credentials/mcp-credentials.env
        echo "CLOUDSTACK_SECRET_KEY=$SECRET_KEY" >>./mcp-credentials/mcp-credentials.env
        echo -e "${GREEN}MCP service account credentials saved to ./mcp-credentials/mcp-credentials.env${NC}"
    fi
else
    echo -e "${YELLOW}Creating MCP service account...${NC}"
    # Create user
    CREATE_OUTPUT=$(run_cmk api createUser username=mcp-service password=mcp-service-password \
        email=mcp-service@example.com firstname=MCP lastname=Service \
        account=admin domainid=1 2>/dev/null || echo "{}")

    if echo "$CREATE_OUTPUT" | grep -q "username"; then
        echo -e "${GREEN}Successfully created MCP service account.${NC}"

        # Wait a bit for user creation to propagate
        sleep 3

        # Now get the user ID
        USERS_JSON=$(run_cmk list users username=mcp-service 2>/dev/null || echo "{}")
        USER_ID=$(echo "$USERS_JSON" | grep -o '"id": *"[^"]*"' | head -1 | cut -d'"' -f4)

        if [ -n "$USER_ID" ]; then
            # Generate keys for service account
            echo -e "${YELLOW}Generating API keys for user ID: $USER_ID...${NC}"
            REGISTER_OUTPUT=$(run_cmk api generateUserKeys id=$USER_ID 2>/dev/null || echo "{}")

            # Extract keys
            API_KEY=$(echo "$REGISTER_OUTPUT" | grep -o '"apikey": *"[^"]*"' | cut -d'"' -f4)
            SECRET_KEY=$(echo "$REGISTER_OUTPUT" | grep -o '"secretkey": *"[^"]*"' | cut -d'"' -f4)

            if [ -n "$API_KEY" ] && [ -n "$SECRET_KEY" ]; then
                echo -e "${GREEN}Successfully generated API keys for MCP service account.${NC}"
                # Write the keys to an environment file
                mkdir -p ./mcp-credentials
                echo "CLOUDSTACK_API_KEY=$API_KEY" >./mcp-credentials/mcp-credentials.env
                echo "CLOUDSTACK_SECRET_KEY=$SECRET_KEY" >>./mcp-credentials/mcp-credentials.env
                echo -e "${GREEN}MCP service account credentials saved to ./mcp-credentials/mcp-credentials.env${NC}"
            else
                echo -e "${RED}Failed to extract API keys: $REGISTER_OUTPUT${NC}"
            fi
        else
            echo -e "${RED}Could not find user ID for newly created MCP service account.${NC}"
        fi
    else
        echo -e "${RED}Failed to create MCP service account: $CREATE_OUTPUT${NC}"
    fi
fi

echo -e "${BLUE}=== MCP Service Account Setup Complete ===${NC}"
echo -e "${YELLOW}You may need to restart the MCP server for it to use the new credentials:${NC}"
echo -e "${YELLOW}task docker:restart${NC}"

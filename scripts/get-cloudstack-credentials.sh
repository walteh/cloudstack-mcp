#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}Retrieving CloudStack API credentials...${NC}"

# Check if CloudStack is running in Docker
if ! docker ps | grep -q cloudstack-simulator; then
    echo -e "${RED}CloudStack simulator is not running.${NC}"
    echo -e "${YELLOW}Please start CloudStack with 'docker-compose up -d' or 'task cloudstack:setup' first.${NC}"
    exit 1
fi

# Wait for CloudStack to be ready
echo -e "${YELLOW}Waiting for CloudStack to be fully initialized...${NC}"
while ! curl -s http://localhost:8080/client >/dev/null; do
    echo -e "${YELLOW}CloudStack is not ready yet, waiting...${NC}"
    sleep 10
done

echo -e "${GREEN}CloudStack is running. Retrieving API credentials...${NC}"

# Use docker exec to get the admin API key and secret key from the CloudStack container
# This example assumes the CloudStack simulator container has the cloudmonkey tool installed
CONTAINER_ID=$(docker ps -q -f name=cloudstack-simulator)

if [ -z "$CONTAINER_ID" ]; then
    echo -e "${RED}Failed to find CloudStack simulator container.${NC}"
    exit 1
fi

# Create a user account if it doesn't exist
echo -e "${GREEN}Creating API access...${NC}"

# Execute command to get API credentials
# This command will vary depending on the CloudStack version and configuration
# For the simulator, we can use default admin credentials
RESULT=$(docker exec $CONTAINER_ID bash -c "cloudmonkey list users | grep -A2 'apikey' || cloudmonkey create account name=admin email=admin@example.com firstname=Admin lastname=Admin username=admin password=password")

# Get API key and secret key
API_KEY=$(docker exec $CONTAINER_ID cloudmonkey list users | grep -A1 'apikey' | tail -n 1 | awk '{print $3}')
SECRET_KEY=$(docker exec $CONTAINER_ID cloudmonkey list users | grep -A2 'apikey' | tail -n 1 | awk '{print $3}')

if [ -z "$API_KEY" ] || [ -z "$SECRET_KEY" ]; then
    echo -e "${RED}Failed to retrieve CloudStack API credentials.${NC}"
    exit 1
fi

echo -e "${GREEN}Successfully retrieved CloudStack API credentials.${NC}"
echo -e "${YELLOW}API Key: ${API_KEY}${NC}"
echo -e "${YELLOW}Secret Key: ${SECRET_KEY}${NC}"

# Create .env file with the credentials
cat >.env <<EOF
CLOUDSTACK_API_KEY=${API_KEY}
CLOUDSTACK_SECRET_KEY=${SECRET_KEY}
EOF

echo -e "${GREEN}Credentials saved to .env file.${NC}"
echo -e "${YELLOW}You can now use 'source .env' to load these credentials into your environment.${NC}"

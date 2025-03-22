#!/bin/bash
set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}Preparing volumes for CloudStack...${NC}"

# Create directory for CloudStack data
CLOUDSTACK_DATA_DIR=".tmp/cloudstack-data"
mkdir -p "$CLOUDSTACK_DATA_DIR"

# Check if the directory was created successfully
if [ -d "$CLOUDSTACK_DATA_DIR" ]; then
    echo -e "${GREEN}Successfully created directory for CloudStack data: $CLOUDSTACK_DATA_DIR${NC}"
else
    echo -e "${RED}Failed to create directory for CloudStack data${NC}"
    exit 1
fi

echo -e "${YELLOW}Volumes are ready. You can now start CloudStack with 'task docker:start'.${NC}"

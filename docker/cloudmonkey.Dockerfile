FROM apache/cloudstack-cloudmonkey:6.4.0

# Create config directory
RUN mkdir -p /root/.cmk

# COPY <<EOT /root/.cmk/config
# [core]
# asyncblock   = false
# timeout      = 0
# output       = json
# verifycert   = false
# profile      = localcloud
# autocomplete = true

# [server "localcloud"]
# url = http://cloudstack:8080/client/api
# apikey = 
# secretkey = 
# timeout = 600
# verifycert = true
# signatureversion = 3
# domain =
# username = admin
# password = password
# expires = 
# output = json
# EOT

# # Set proper permissions
# RUN chmod 600 /root/.cmk/config

# Create cache directory
RUN mkdir -p /root/.cmk/profiles/localcloud

COPY <<EOT /usr/local/bin/docker-entrypoint.sh
#! /bin/sh

echo "running entrypoint"

# we could have this commented in, but it must be the default because it works without it
# cmk set profile localcloud
# cmk set url http://cloudstack:8080/client/api
# cmk set username admin
# cmk set password password
# cmk set domain ""
# cmk set output json

# just keep trying to sync 
while ! cmk -d sync; do
	echo "Sync failed - will retry later"
	sleep 5
done

# Create service account for MCP server if it doesn't exist
# echo "Setting up MCP service account..."

# # First, try login with admin
# cmk login -u admin -p password || echo "Admin login failed, retrying..."

# # Check if 'mcp-service' user already exists
# USERS_JSON=$(cmk list users username=mcp-service 2>/dev/null || echo "{}")
# if echo "$USERS_JSON" | grep -q "mcp-service"; then
#     echo "MCP service account already exists"
# else
#     echo "Creating MCP service account..."
#     # Create user
#     cmk api createUser username=mcp-service password=mcp-service-password \
#         email=mcp-service@example.com firstname=MCP lastname=Service \
#         account=admin domainid=1 || echo "Failed to create MCP service user"
    
#     # Wait a bit for user creation to propagate
#     sleep 3
# fi

# # Generate API keys for MCP service account if needed
# USERS_JSON=$(cmk list users username=mcp-service 2>/dev/null || echo "{}")
# API_KEY=$(echo "$USERS_JSON" | grep -o '"apikey": *"[^"]*"' | cut -d'"' -f4)
# if [ -z "$API_KEY" ] || [ "$API_KEY" = "null" ]; then
#     echo "Generating API keys for MCP service account..."
#     USER_ID=$(echo "$USERS_JSON" | grep -o '"id": *"[^"]*"' | head -1 | cut -d'"' -f4)
    
#     if [ -n "$USER_ID" ]; then
#         # Generate keys for service account
#         REGISTER_OUTPUT=$(cmk api generateUserKeys id=$USER_ID 2>/dev/null || echo "Failed to generate API keys")
        
#         # Extract keys
#         API_KEY=$(echo "$REGISTER_OUTPUT" | grep -o '"apikey": *"[^"]*"' | cut -d'"' -f4)
#         SECRET_KEY=$(echo "$REGISTER_OUTPUT" | grep -o '"secretkey": *"[^"]*"' | cut -d'"' -f4)
        
#         if [ -n "$API_KEY" ] && [ -n "$SECRET_KEY" ]; then
#             echo "Successfully generated API keys for MCP service account"
#             # Write the keys to an environment file that can be sourced by MCP
#             echo "CLOUDSTACK_API_KEY=$API_KEY" > /tmp/mcp-credentials.env
#             echo "CLOUDSTACK_SECRET_KEY=$SECRET_KEY" >> /tmp/mcp-credentials.env
#             echo "MCP service account credentials saved"
#         else
#             echo "Failed to extract API keys"
#         fi
#     else
#         echo "Could not find user ID for MCP service account"
#     fi
# else
#     echo "MCP service account already has API keys"
#     SECRET_KEY=$(echo "$USERS_JSON" | grep -o '"secretkey": *"[^"]*"' | cut -d'"' -f4)
    
#     # Write the keys to an environment file
#     echo "CLOUDSTACK_API_KEY=$API_KEY" > /tmp/mcp-credentials.env
#     echo "CLOUDSTACK_SECRET_KEY=$SECRET_KEY" >> /tmp/mcp-credentials.env
#     echo "MCP service account credentials saved"
# fi

# never exit
tail -f /dev/null

EOT

RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# # Create entrypoint script that handles initialization
# RUN echo '#!/bin/sh\n\
# 	\n\
# 	# Handle special init case\n\
# 	if [ "$1" = "init" ]; then\n\
# 	# Attempt to sync the API\n\
# 	echo "Syncing CloudMonkey API cache..."\n\
# 	cmk sync || echo "Sync failed - will retry later"\n\
# 	\n\
# 	# Try to login\n\
# 	echo "Logging in to CloudStack..."\n\
# 	cmk login -u admin -p password || echo "Login failed - will retry later"\n\
# 	\n\
# 	# Try a simple API command to verify access\n\
# 	echo "Testing API access..."\n\
# 	cmk list zones || echo "API access test failed"\n\
# 	\n\
# 	echo "Initialization completed"\n\
# 	exit 0\n\
# 	fi\n\
# 	\n\
# 	# For normal operation, just execute the provided command\n\
# 	exec cmk "$@"' > /usr/local/bin/docker-entrypoint.sh

# # Make entrypoint executable
# RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# # Set the entrypoint
ENTRYPOINT ["/bin/sh", "-c", "/usr/local/bin/docker-entrypoint.sh"]

# # Default command if none provided
# CMD ["help"] 
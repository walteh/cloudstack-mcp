FROM apache/cloudstack-cloudmonkey:6.4.0

# Create config directory
RUN mkdir -p /root/.cmk

COPY <<EOT /root/.cmk/config
[core]
asyncblock   = false
timeout      = 0
output       = json
verifycert   = false
profile      = localcloud
autocomplete = true

[server "localcloud"]
url = http://cloudstack:8080/client/api
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
EOT

# Set proper permissions
RUN chmod 600 /root/.cmk/config

# Create cache directory
RUN mkdir -p /root/.cmk/profiles/localcloud

COPY <<EOT /usr/local/bin/docker-entrypoint.sh
#! /bin/sh

echo "running entrypoint"

# just keep trying to sync 
while ! cmk -d sync; do
	echo "Sync failed - will retry later"
	sleep 5
done

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
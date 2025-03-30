#!/bin/bash
set -e

# Create directory for scripts if it doesn't exist
mkdir -p scripts

this_script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# Create the VM setup script
cat >scripts/setup-cloudstack-vm.sh <<'EOF'
#!/bin/bash
set -e

# Launch VM
echo "Launching Alpine VM for CloudStack agent..."
alpine launch -n cloudstack-vm -a x86_64 -c 4 -m 4096 -d 20G -s 2222 --mount $this_script_dir

echo "Waiting for VM to boot..."
sleep 5



# Install cloud-init and run the configuration
echo "Installing cloud-init and applying configuration..."
alpine exec cloudstack-vm << 'COMMANDS'
apk update
apk add cloud-init

# Apply cloud-init configuration manually
cloud-init -f /mnt/cloudstack-agent.cloud-init.yaml init
cloud-init -f /mnt/cloudstack-agent.cloud-init.yaml modules
EOF

chmod +x scripts/setup-cloudstack-vm.sh

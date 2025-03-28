# CloudStack with QEMU/KVM Integration

This document describes how to use a real QEMU/KVM hypervisor with CloudStack in this project, instead of using just the simulator.

## Overview

While the CloudStack simulator is useful for testing and development, you may want to create actual virtual machines that run on real hypervisors. This integration adds a containerized KVM host that runs alongside the CloudStack management server, allowing you to:

1. Deploy real VMs with QEMU/KVM
2. Test advanced networking features
3. Get a more production-like experience for testing

## Architecture

The integrated system has the following components:

-   **CloudStack Management Server**: Running in a Docker container, manages the cloud infrastructure
-   **KVM Host**: A Docker container with KVM/QEMU and libvirt that serves as a hypervisor
-   **MCP Server**: Bridges AI agents with CloudStack
-   **CloudMonkey**: CLI tool for interacting with CloudStack API

## Prerequisites

-   Docker and Docker Compose
-   At least 8GB of RAM available for containers
-   20GB of disk space

## Quick Start

### 1. Start the Environment

```bash
# Start all containers including CloudStack, MCP, and KVM
task docker:start
```

This command will start CloudStack, CloudMonkey, MCP server, and the KVM host container.

### 2. Add the KVM Host to CloudStack

Once all containers are up and running (this may take a few minutes for CloudStack to initialize), add the KVM host with:

```bash
# Add the KVM host to CloudStack
task kvm:add-host
```

This script will:

-   Get the IP address of the KVM container
-   Find the appropriate Zone, Pod, and Cluster IDs
-   Add the KVM host to CloudStack

### 3. Verify the KVM Host in CloudStack

Access the CloudStack UI at http://localhost:8080/client (login: admin/password) and navigate to Infrastructure > Hosts. You should see your KVM host listed.

## Creating VMs on KVM

You can create VMs on the KVM host using either:

1. **CloudStack UI**: Navigate to Compute > Instances > Add Instance
2. **CloudMonkey CLI**:

```bash
# Create a VM on the KVM host
task cmk -- deploy virtualmachine serviceofferingid=... templateid=... zoneid=...
```

## Behind the Scenes

The KVM host container runs:

-   QEMU/KVM for virtualization
-   libvirt for VM management
-   SSH for CloudStack to connect
-   Network bridges for VM connectivity

The connection between CloudStack and the KVM host uses the libvirt protocol over TCP.

## Troubleshooting

### KVM Host Not Connecting

If the KVM host fails to connect to CloudStack:

1. Check container status:

```bash
docker ps | grep cloudstack-mcp-kvm
```

2. Check libvirt status inside the container:

```bash
docker exec cloudstack-mcp-kvm systemctl status libvirtd
```

3. Verify network connectivity:

```bash
docker exec cloudstack-mcp-simulator ping -c 3 cloudstack-mcp-kvm
```

### Libvirt Socket Issues

If you're having issues with the libvirt socket:

1. Verify the libvirt socket exists on your host:

```bash
ls -la ~/.cache/libvirt/
```

2. Check if the container can access the host's libvirt directory:

```bash
docker exec cloudstack-mcp-kvm ls -la /var/run/libvirt/
```

3. Test libvirt connection in the container:

```bash
docker exec cloudstack-mcp-kvm virsh -c qemu:///system list
```

4. Check permissions on the host's socket files:

```bash
ls -la ~/.cache/libvirt/libvirt-sock
```

5. If using host's libvirt, make sure it's running:

```bash
brew services list | grep libvirt
```

6. Restart host's libvirt if needed:

```bash
brew services restart libvirt
```

7. If all else fails, use the internal libvirt daemon by removing the volume mount

### VM Creation Failing

If VM creation fails:

1. Check libvirt logs:

```bash
docker exec cloudstack-mcp-kvm cat /var/log/libvirt/libvirtd.log
```

2. Verify storage pools:

```bash
docker exec cloudstack-mcp-kvm virsh pool-list --all
```

3. Check CloudStack agent logs:

```bash
docker exec cloudstack-mcp-simulator cat /var/log/cloudstack/agent/agent.log
```

## Limitations

-   The KVM host runs in a Docker container, which has some performance limitations
-   Not all CloudStack networking features may work perfectly in this containerized environment
-   The setup is designed for testing and development, not production use

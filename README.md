# CloudStack MCP

A Model Context Protocol (MCP) server for Apache CloudStack, allowing AI agents to interact with CloudStack through a standardized interface.

## Overview

This project implements an MCP server that connects to Apache CloudStack's API, providing a bridge between AI assistants (like Claude) and CloudStack infrastructure. It enables AI agents to:

-   List templates and service offerings
-   Deploy and manage virtual machines
-   Check VM status and operations
-   And more...

## Prerequisites

-   Go 1.18 or higher
-   Docker and Docker Compose (for containerized setup)
-   UTM (optional, for running CloudStack on Apple Silicon)
-   Task (for running the task commands)

### CloudMonkey Integration

This project includes a containerized version of CloudMonkey, the official CLI client for CloudStack. With our setup, you don't need to install CloudMonkey locally - it runs in its own container with proper configuration.

To use the containerized CloudMonkey:

```bash
# Start the environment with Docker Compose
task docker:start

# Run CloudMonkey commands through our wrapper script
task cmk -- list zones
task cmk -- list serviceofferings
task cmk -- help
```

The `task cmk` command accepts any valid CloudMonkey command and parameters.

## Quick Start

### Using Docker Compose

The easiest way to get started is with Docker Compose, which sets up both CloudStack and the MCP server:

```bash
# Clone the repository
git clone https://github.com/walteh/cloudstack-mcp.git
cd cloudstack-mcp

# Start the CloudStack and MCP containers
docker-compose up -d
```

The CloudStack management interface will be available at http://localhost:8080/client (default credentials: admin/password) after a few minutes of initialization.

The MCP server will be accessible at http://localhost:8251.

### Using UTM (Recommended for M1 Macs)

For Apple Silicon Macs, running CloudStack in UTM provides better performance:

```bash
# Install task runner (if not already installed)
brew install go-task

# Set up the CloudStack environment in UTM
task cloudstack:setup

# Start the MCP server locally
task run:server
```

## Configuration

The MCP server can be configured through environment variables or command-line flags:

-   `CLOUDSTACK_API_URL` - CloudStack API URL (default: http://localhost:8080/client/api)
-   `CLOUDSTACK_API_KEY` - CloudStack API key (required for authenticated operations)
-   `CLOUDSTACK_SECRET_KEY` - CloudStack secret key (required for authenticated operations)
-   `CLOUDSTACK_TIMEOUT` - API timeout in seconds (default: 60)
-   `MCP_ADDR` - Address for the MCP server to listen on (default: :8250)

## MCP Tools

The MCP server implements the following tools:

-   `listTemplates` - List available templates in CloudStack
-   `deployVM` - Deploy a new virtual machine
-   `getVMStatus` - Get the status of a virtual machine

## Development

```bash
# Set up the development environment
task setup-env

# Run the server locally
task run:server

# Build the server
task build
```

## Troubleshooting

### Socket Connection Issue

If you encounter a socket connection error like this:

```
2025-03-22 07:01:42 2025-03-22 12:01:42,132 main ERROR TcpSocketManager (TCP:localhost:4560) caught exception and will continue: java.io.IOException: Unable to create socket for localhost at port 4560
```

You can fix it by running:

```bash
task cloudstack:fix-socket
```

This script will check for port conflicts and restart the CloudStack management server to resolve socket binding issues.

### CloudMonkey API Cache Issues

If CloudMonkey commands fail with an error message like:

```
Failed to read API cache, please run 'sync'
```

Or if you see any API cache-related errors, you can fix them by running:

```bash
task cmk -- sync
```

This command will sync the API cache in the CloudMonkey container.

### Environment Status Check

To check the overall status of your CloudStack environment, run:

```bash
task cloudstack:status
```

This will provide a comprehensive overview of:

-   Container status for CloudStack and MCP
-   CloudStack management service status
-   Web UI accessibility
-   Java processes
-   Listening ports
-   CloudMonkey API cache status
-   API credential availability

This is particularly helpful for troubleshooting when you encounter issues with any part of the system.

## CloudStack Interaction with CloudMonkey

Once CloudStack is running, you can interact with it directly using CloudMonkey through our containerized approach:

```bash
# List available commands
task cmk -- help

# List zones
task cmk -- list zones

# List templates
task cmk -- list templates
```

CloudMonkey is a powerful tool for directly testing CloudStack API functionality and can help you understand how to use the API in your MCP server.

### API Credentials

To get the CloudStack API credentials:

```bash
task cloudstack:get-credentials
```

This will use the containerized CloudMonkey to generate API credentials for the admin user and save them to a `.env` file.

## Using Docker for CloudMonkey

Our approach uses a containerized version of CloudMonkey to avoid common issues with CloudMonkey configuration and cache management:

1. CloudMonkey runs in its own Docker container with proper configuration
2. All cache and configuration issues are handled within the container
3. The container is automatically started with the rest of the environment
4. Commands are executed through a simple wrapper script (`task cmk`)
5. No need to install or configure CloudMonkey locally

This eliminates common issues like API cache errors, configuration problems, and authentication failures that can occur with local CloudMonkey installations.

## License

Apache License 2.0

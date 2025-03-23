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

We use Docker to run CloudMonkey in a containerized environment, which offers several benefits:

-   CloudMonkey runs in its own container, avoiding configuration and cache management issues
-   Cache and configuration problems are handled within the container
-   The container starts automatically with the environment
-   Commands are executed through a wrapper script (`task cmk`), eliminating the need for local installation

You can interact with CloudMonkey using the following command:

```bash
task cmk -- list zones
```

This executes the `list zones` command in the CloudMonkey container.

## Authentication Options

The MCP server supports multiple authentication methods for connecting to CloudStack:

### 1. Direct API Key Authentication

You can provide CloudStack API key and Secret key directly:

```bash
# Using environment variables
export CLOUDSTACK_API_URL=http://cloudstack:8080/client/api
export CLOUDSTACK_API_KEY=your-api-key
export CLOUDSTACK_SECRET_KEY=your-secret-key

# Or pass as command line arguments
./mcp-server --api-url=http://cloudstack:8080/client/api --api-key=your-api-key --secret-key=your-secret-key
```

### 2. Username/Password Authentication

The MCP server can automatically obtain API keys using username and password credentials:

```bash
# Using environment variables
export CLOUDSTACK_API_URL=http://cloudstack:8080/client/api
export CLOUDSTACK_USERNAME=mcp-service
export CLOUDSTACK_PASSWORD=mcp-service-password

# Or pass as command line arguments
./mcp-server --api-url=http://cloudstack:8080/client/api --username=mcp-service --password=mcp-service-password
```

When using this method, the MCP server will:

1. Check if the specified user exists in CloudStack
2. Create the user if it doesn't exist
3. Generate API keys for the user if they don't already have them
4. Use these API keys for CloudStack communication

This simplifies the setup process and avoids the need for pre-generated API keys.

## MCP Service Account

For proper separation of concerns, the MCP server uses a dedicated service account to interact with CloudStack instead of using the admin credentials. This service account is automatically created during the initialization of the CloudMonkey container.

### Service Account Setup

The service account setup happens automatically when you start the environment with:

```bash
task docker:start
```

This creates a user named `mcp-service` with appropriate API credentials, which are then made available to the MCP server container.

### Manual Service Account Management

You can also manually check or create the service account with these commands:

```bash
# Check the status of the MCP service account
task mcp:service-account:check

# Create or regenerate the MCP service account
task mcp:service-account:create
```

After manually creating or updating the service account, you may need to restart the MCP server for it to use the new credentials:

```bash
task docker:restart
```

## License

Apache License 2.0

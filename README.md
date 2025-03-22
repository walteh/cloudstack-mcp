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

### API Credentials

If you need to manually get the CloudStack API credentials:

```bash
task cloudstack:get-credentials
```

This will extract API credentials from the running CloudStack container and save them to a `.env` file.

## License

Apache License 2.0

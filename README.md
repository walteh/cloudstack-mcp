# CloudStack MCP

A Model Context Protocol (MCP) server for Apache CloudStack, allowing AI agents to interact with CloudStack infrastructure.

> [!IMPORTANT]
> This project is still under development and not all features are available. Treat it as a proof of concept.

## Running the server (macOS)

```bash
# Install the dependencies
brew install go-task go docker

# Install the CLI
git clone https://github.com/walteh/cloudstack-mcp.git
cd cloudstack-mcp

# Start the environment (will take a while)
task docker:start
```

## Generic MCP Usage

### for `sse` servers (recommended, if possible)

-   works with `Cursor`

```json
{
	"mcpServers": {
		"cloudstack": {
			"url": "http://localhost:8250/sse"
		}
	}
}
```

### for `stdio` servers

-   works with `Cursor`, and `Claude Desktop`

```json
{
	"mcpServers": {
		"cloudstack": {
			"command": "docker",
			"args": ["compose", "-f", "<<LOCAL_PATH_TO_THIS_REPO>>/docker-compose.yaml", "run", "mcp-stdio-server"]
		}
	}
}
```

## Usage with Cursor (macOS)

1. make sure server is running and you are in the root of the project (see above)

2. set up dependencies

```bash
brew install --cask cursor
```

3. open the project in Cursor

```bash
cursor cloudstack-mcp.code-workspace
```

4. open cursor settings from the menu bar `Cursor -> Settings... -> Cursor Settings` (or press `Shift-Cmd-J`)

5. go to the `MCP` tab, you should see the `cloudstack` server already added (because of [`./.cursor/mcp.json`](./.cursor/mcp.json))

6. click on the buttons to refresh and enable the `cloudstack` server - you should see a green dot and a list of `list` cloudstack api commands

7. open a new composer by making sure the composer pane is open (toggle with `Cmd-Option-B`) and start a new conversation (press `Cmd-N`)

8. make sure the composer is set to `Agent`

9. type `can you list my @cloudstack vpcs?`

10. press `Enter` and watch the magic happen!

```
# list zones
list zones
```

## Usage with Claude Desktop (macOS)

> [!CAUTION]
> There is a 99% chance this will not work, but you can use it as a starting point to get the MCP working with Claude Desktop if you like.

1. make sure server is running and you are in the root of the project (see above)

2. set up dependencies

```bash
brew install --cask claude-desktop
```

3. run this to setup the server in Claude Desktop

```bash
# injects the server into claudes config, doesn't overwrite anything (unless you have another mcp called 'cloudstack')
task mcp:setup:claude-desktop
```

4. restart claude desktop

## How It Works

This MCP implementation allows AI assistants to manage CloudStack resources by:

-   Translating MCP protocol requests into CloudStack API calls
-   Providing common operations like VM deployment and management
-   Handling authentication and API interaction automatically

## CloudMonkey Usage

CloudMonkey (the CloudStack CLI) is containerized in this setup:

```bash
# Basic commands
task cmk -- list zones
task cmk -- list serviceofferings

# Fix API cache issues if they occur
task cmk -- sync

# Get API credentials
task cloudstack:get-credentials
```

## License

Apache License 2.0

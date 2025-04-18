package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
	"github.com/walteh/cloudstack-mcp/pkg/cloudstack"
	errors "gitlab.com/tozd/go/errors"
)

// Server represents an MCP server for CloudStack
type Server struct {
	username  string
	password  string
	apiURL    string
	mcpServer *server.MCPServer
}

// NewServer creates a new MCP server
func NewServer(ctx context.Context, username, password, apiURL string) (*Server, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Msg("Creating CloudStack MCP server")

	s := &Server{
		username: username,
		password: password,
		apiURL:   apiURL,
		mcpServer: server.NewMCPServer(
			"CloudStackMCP",
			"1.0.0",
			server.WithToolCapabilities(false),
			server.WithResourceCapabilities(false, false),
			server.WithInstructions("CloudStack MCP server provides tools to interact with CloudStack"),
		),
	}

	// Register the dynamic tools based on CloudStack API
	if err := s.registerDynamicTools(ctx); err != nil {
		return nil, errors.Errorf("registering dynamic tools: %w", err)
	}

	// Register default tools as fallback
	// s.registerDefaultTools(ctx)

	logger.Info().Msg("MCP server created")
	return s, nil
}

// registerDynamicTools registers tools dynamically based on CloudStack API
func (s *Server) registerDynamicTools(ctx context.Context) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Msg("Registering dynamic tools based on CloudStack API")

	tools, err := s.CreateToolForEachApi(ctx)
	if err != nil {
		return errors.Errorf("creating tools for APIs: %w", err)
	}

	logger.Info().Int("count", len(tools)).Msg("Registering CloudStack API tools")

	for _, tool := range tools {
		s.mcpServer.AddTool(*tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return s.handleDynamicTool(ctx, req, tool.Name)
		})
		logger.Debug().Str("tool", tool.Name).Msg("Registered dynamic tool")
	}

	return nil
}

// handleDynamicTool is a generic handler for dynamically created tools
func (s *Server) handleDynamicTool(ctx context.Context, req mcp.CallToolRequest, toolID string) (*mcp.CallToolResult, error) {
	logger := zerolog.Ctx(ctx).With().Str("tool", toolID).Logger()
	logger = logger.With().Interface("parameters", req.Params).Logger()
	logger.Debug().Msg("Executing dynamic tool")

	// Extract the API name from the tool ID
	// Tool IDs are formatted as cs_<apiName>
	// if !strings.HasPrefix(toolID, "cs_") {
	// 	return nil, errors.Errorf("invalid tool ID format: %s", toolID)
	// }
	apiName := strings.TrimPrefix(toolID, "cs_")

	logger.Debug().Str("apiName", apiName).Msg("Executing CloudStack API")

	// Convert mcp.Params to a map of strings for the CloudStack API
	params := make(map[string]string)
	for key, value := range req.Params.Arguments {
		if value == nil {
			continue
		}

		// Convert value to string based on type
		switch v := value.(type) {
		case string:
			params[key] = v
		case float64:
			params[key] = fmt.Sprintf("%v", v)
		case bool:
			params[key] = fmt.Sprintf("%t", v)
		case []interface{}:
			// Convert array to comma-separated string

			var strValues []string
			for _, item := range v {
				strValues = append(strValues, fmt.Sprintf("%v", item))
			}
			params[key] = strings.Join(strValues, ",")
		case map[string]interface{}:
			// Marshal map to JSON string
			jsonBytes, err := json.Marshal(v)
			if err != nil {
				logger.Warn().Err(err).Str("param", key).Msg("Failed to marshal object parameter")
				continue
			}
			params[key] = string(jsonBytes)
		default:
			// Default conversion for other types
			params[key] = fmt.Sprintf("%v", v)
		}
	}

	// Execute the API call
	logger.Debug().Interface("params", params).Msg("Calling CloudStack API")

	// Call the dynamic API
	result, err := cloudstack.DoRawCloudStackRequest(ctx, s.apiURL, apiName, s.username, s.password, params)
	if err != nil {
		logger.Error().Err(err).Msg("CloudStack API call failed")
		return nil, errors.Errorf("error executing CloudStack API: %w", err)
	}

	logger.Debug().Msg("Dynamic tool executed successfully")

	marsh, err := json.Marshal(result)
	if err != nil {
		return nil, errors.Errorf("error marshalling result: %w", err)
	}

	return mcp.NewToolResultText(string(marsh)), nil
}

// Start starts the MCP server
func (s *Server) Server() *server.MCPServer {
	return s.mcpServer
}

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/rs/zerolog"
	"github.com/walteh/cloudstack-mcp/pkg/cloudstack"
	errors "gitlab.com/tozd/go/errors"
)

// Server represents an MCP server for CloudStack
type Server struct {
	client    *cloudstack.Client
	tools     map[string]ToolHandler
	toolsMu   sync.RWMutex
	serverMux *http.ServeMux
}

// ToolHandler is the function signature for handling tool requests
type ToolHandler func(context.Context, map[string]interface{}) (interface{}, error)

// NewServer creates a new MCP server
func NewServer(ctx context.Context, client *cloudstack.Client) *Server {
	logger := zerolog.Ctx(ctx)

	s := &Server{
		client:    client,
		tools:     make(map[string]ToolHandler),
		serverMux: http.NewServeMux(),
	}

	// Register routes
	s.serverMux.HandleFunc("/v1/tools", s.handleTools)
	s.serverMux.HandleFunc("/v1/status", s.handleStatus)

	// Register default tools
	s.registerDefaultTools(ctx)

	logger.Info().Msg("MCP server created")

	return s
}

// registerDefaultTools registers the default set of tools
func (s *Server) registerDefaultTools(ctx context.Context) {
	logger := zerolog.Ctx(ctx)

	s.RegisterTool(ctx, "listTemplates", s.handleListTemplates)
	s.RegisterTool(ctx, "deployVM", s.handleDeployVM)
	s.RegisterTool(ctx, "getVMStatus", s.handleGetVMStatus)

	logger.Info().Msg("Default tools registered")
}

// RegisterTool registers a new tool handler
func (s *Server) RegisterTool(ctx context.Context, name string, handler ToolHandler) {
	logger := zerolog.Ctx(ctx)

	s.toolsMu.Lock()
	defer s.toolsMu.Unlock()
	s.tools[name] = handler

	logger.Info().Str("tool", name).Msg("Tool registered")
}

// Start starts the MCP server
func (s *Server) Start(ctx context.Context, addr string) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("address", addr).Msg("Starting MCP server")

	return http.ListenAndServe(addr, s.loggerMiddleware(s.serverMux, *logger))
}

// loggerMiddleware adds logging to HTTP requests
func (s *Server) loggerMiddleware(next http.Handler, logger zerolog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create a request-specific logger
		reqLogger := logger.With().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Str("remote_addr", r.RemoteAddr).
			Logger()

		// Add logger to context
		ctx := r.Context()
		ctx = reqLogger.WithContext(ctx)
		r = r.WithContext(ctx)

		reqLogger.Debug().Msg("Request received")

		// Call the next handler
		next.ServeHTTP(w, r)

		reqLogger.Debug().Msg("Request completed")
	})
}

// handleTools handles the /v1/tools endpoint
func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := zerolog.Ctx(ctx)

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		logger.Warn().Str("method", r.Method).Msg("Method not allowed")
		return
	}

	var req struct {
		Name       string                 `json:"name"`
		Parameters map[string]interface{} `json:"parameters"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errMsg := fmt.Sprintf("Failed to decode request: %v", err)
		http.Error(w, errMsg, http.StatusBadRequest)
		logger.Error().Err(err).Msg("Failed to decode request")
		return
	}

	// Create a new logger with the tool name and update the context
	toolLogger := logger.With().Str("tool", req.Name).Logger()
	ctx = toolLogger.WithContext(ctx)
	toolLogger.Debug().Interface("parameters", req.Parameters).Msg("Tool request")

	s.toolsMu.RLock()
	handler, ok := s.tools[req.Name]
	s.toolsMu.RUnlock()

	if !ok {
		errMsg := fmt.Sprintf("Unknown tool: %s", req.Name)
		http.Error(w, errMsg, http.StatusBadRequest)
		toolLogger.Warn().Msg("Unknown tool requested")
		return
	}

	result, err := handler(ctx, req.Parameters)
	if err != nil {
		errMsg := fmt.Sprintf("Tool execution failed: %v", err)
		http.Error(w, errMsg, http.StatusInternalServerError)
		toolLogger.Error().Err(err).Msg("Tool execution failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"result": result,
	}); err != nil {
		toolLogger.Error().Err(err).Msg("Failed to encode response")
	}

	toolLogger.Debug().Msg("Tool executed successfully")
}

// handleStatus handles the /v1/status endpoint
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := zerolog.Ctx(ctx)

	logger.Debug().Msg("Status request received")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"name":   "cloudstack-mcp",
	})

	logger.Debug().Msg("Status request completed")
}

// handleListTemplates handles the listTemplates tool
func (s *Server) handleListTemplates(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	logger := zerolog.Ctx(ctx)
	logger.Debug().Msg("Executing listTemplates")

	templates, err := s.client.ListTemplates()
	if err != nil {
		return nil, errors.Errorf("listing templates: %w", err)
	}

	logger.Debug().Int("count", len(templates)).Msg("Templates retrieved")
	return templates, nil
}

// handleDeployVM handles the deployVM tool
func (s *Server) handleDeployVM(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	logger := zerolog.Ctx(ctx)
	logger.Debug().Interface("params", params).Msg("Executing deployVM")

	name, ok := params["name"].(string)
	if !ok {
		return nil, errors.New("name parameter is required")
	}

	templateID, ok := params["templateId"].(string)
	if !ok {
		return nil, errors.New("templateId parameter is required")
	}

	serviceOfferingID, ok := params["serviceOfferingId"].(string)
	if !ok {
		return nil, errors.New("serviceOfferingId parameter is required")
	}

	zoneID, ok := params["zoneId"].(string)
	if !ok {
		// Try to get default zone
		var err error
		logger.Debug().Msg("zoneId not provided, getting default zone")
		zoneID, err = s.client.GetDefaultZone()
		if err != nil {
			return nil, errors.Errorf("getting default zone: %w", err)
		}
		logger.Debug().Str("zoneId", zoneID).Msg("Using default zone")
	}

	logger.Info().
		Str("name", name).
		Str("templateId", templateID).
		Str("serviceOfferingId", serviceOfferingID).
		Str("zoneId", zoneID).
		Msg("Deploying VM")

	vmID, err := s.client.DeployVM(name, templateID, serviceOfferingID, zoneID)
	if err != nil {
		return nil, errors.Errorf("deploying VM: %w", err)
	}

	logger.Info().Str("vmId", vmID).Msg("VM deployment initiated")
	return map[string]string{
		"id":     vmID,
		"status": "deploying",
	}, nil
}

// handleGetVMStatus handles the getVMStatus tool
func (s *Server) handleGetVMStatus(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	logger := zerolog.Ctx(ctx)

	id, ok := params["id"].(string)
	if !ok {
		return nil, errors.New("id parameter is required")
	}

	logger.Debug().Str("vmId", id).Msg("Getting VM status")
	status, err := s.client.GetVMStatus(id)
	if err != nil {
		return nil, errors.Errorf("getting VM status: %w", err)
	}

	logger.Debug().Str("vmId", id).Str("status", status).Msg("Retrieved VM status")
	return map[string]string{
		"id":     id,
		"status": status,
	}, nil
}

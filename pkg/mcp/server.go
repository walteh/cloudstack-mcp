package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/walteh/cloudstack-mcp/pkg/cloudstack"
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
func NewServer(client *cloudstack.Client) *Server {
	s := &Server{
		client:    client,
		tools:     make(map[string]ToolHandler),
		serverMux: http.NewServeMux(),
	}

	// Register routes
	s.serverMux.HandleFunc("/v1/tools", s.handleTools)
	s.serverMux.HandleFunc("/v1/status", s.handleStatus)

	// Register default tools
	s.registerDefaultTools()

	return s
}

// registerDefaultTools registers the default set of tools
func (s *Server) registerDefaultTools() {
	s.RegisterTool("listTemplates", s.handleListTemplates)
	s.RegisterTool("deployVM", s.handleDeployVM)
	s.RegisterTool("getVMStatus", s.handleGetVMStatus)
}

// RegisterTool registers a new tool handler
func (s *Server) RegisterTool(name string, handler ToolHandler) {
	s.toolsMu.Lock()
	defer s.toolsMu.Unlock()
	s.tools[name] = handler
}

// Start starts the MCP server
func (s *Server) Start(addr string) error {
	log.Printf("Starting MCP server on %s", addr)
	return http.ListenAndServe(addr, s.serverMux)
}

// handleTools handles the /v1/tools endpoint
func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name       string                 `json:"name"`
		Parameters map[string]interface{} `json:"parameters"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Failed to decode request: %v", err), http.StatusBadRequest)
		return
	}

	s.toolsMu.RLock()
	handler, ok := s.tools[req.Name]
	s.toolsMu.RUnlock()

	if !ok {
		http.Error(w, fmt.Sprintf("Unknown tool: %s", req.Name), http.StatusBadRequest)
		return
	}

	result, err := handler(r.Context(), req.Parameters)
	if err != nil {
		http.Error(w, fmt.Sprintf("Tool execution failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"result": result,
	}); err != nil {
		log.Printf("Failed to encode response: %v", err)
	}
}

// handleStatus handles the /v1/status endpoint
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"name":   "cloudstack-mcp",
	})
}

// handleListTemplates handles the listTemplates tool
func (s *Server) handleListTemplates(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	templates, err := s.client.ListTemplates()
	if err != nil {
		return nil, err
	}
	return templates, nil
}

// handleDeployVM handles the deployVM tool
func (s *Server) handleDeployVM(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	name, ok := params["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name parameter is required")
	}

	templateID, ok := params["templateId"].(string)
	if !ok {
		return nil, fmt.Errorf("templateId parameter is required")
	}

	serviceOfferingID, ok := params["serviceOfferingId"].(string)
	if !ok {
		return nil, fmt.Errorf("serviceOfferingId parameter is required")
	}

	zoneID, ok := params["zoneId"].(string)
	if !ok {
		// Try to get default zone
		var err error
		zoneID, err = s.client.GetDefaultZone()
		if err != nil {
			return nil, fmt.Errorf("zoneId parameter is required and no default zone available: %v", err)
		}
	}

	vmID, err := s.client.DeployVM(name, templateID, serviceOfferingID, zoneID)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"id":     vmID,
		"status": "deploying",
	}, nil
}

// handleGetVMStatus handles the getVMStatus tool
func (s *Server) handleGetVMStatus(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	id, ok := params["id"].(string)
	if !ok {
		return nil, fmt.Errorf("id parameter is required")
	}

	status, err := s.client.GetVMStatus(id)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"id":     id,
		"status": status,
	}, nil
}

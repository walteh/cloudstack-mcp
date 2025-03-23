package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-task/task/v3/taskfile"
	"github.com/go-task/task/v3/taskfile/ast"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	errors "gitlab.com/tozd/go/errors"
	"gopkg.in/yaml.v3"
)

type TaskRegistry struct {
	server      *server.MCPServer
	taskfile    *ast.Taskfile
	filePath    string
	tasksByName map[string]*ast.Task
	toolNames   map[string]string // Maps task names to tool IDs
	mu          sync.RWMutex
}

func main() {
	// Create context
	ctx := context.Background()

	// Command line flags
	httpMode := flag.Bool("http", false, "Run in HTTP mode instead of stdio")
	httpAddr := flag.String("addr", ":8080", "HTTP server address (only used with -http)")
	taskfilePath := flag.String("taskfile", "", "Path to Taskfile.yaml (default: auto-detect)")
	logFilePath := flag.String("log", "", "Path to log file (default: logs/taskmcp.log)")
	logLevelStr := flag.String("log-level", "info", "Log level (trace, debug, info, warn, error, fatal, panic)")
	flag.Parse()

	// // Immediately suppress stdout for stdio mode
	// // This ensures we don't accidentally break the protocol with debug prints
	// if !*httpMode {
	// 	// In stdio mode, we must not output anything to stdout before starting the server
	// 	// as it would break the protocol. Redirect all early output to stderr.
	// 	os.Stdout = os.Stderr
	// }

	// Set up logging
	logDir := "logs"
	if *logFilePath == "" {
		if _, err := os.Stat(logDir); os.IsNotExist(err) {
			os.Mkdir(logDir, 0755)
		}
		*logFilePath = filepath.Join(logDir, "taskmcp.log")
	} else {
		// Ensure directory exists for custom log path
		logFileDir := filepath.Dir(*logFilePath)
		if _, err := os.Stat(logFileDir); os.IsNotExist(err) {
			os.MkdirAll(logFileDir, 0755)
		}
	}

	// Set log level
	level, err := zerolog.ParseLevel(*logLevelStr)
	if err != nil {
		if *httpMode {
			fmt.Printf("Invalid log level '%s', using 'info'\n", *logLevelStr)
		}
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	// Create log file
	logFile, err := os.Create(*logFilePath)
	if err != nil {
		zlog.Fatal().Err(err).Msg("Failed to create log file")
	}
	defer logFile.Close()

	// Configure logger to write to both console and file or just file based on mode
	var multi zerolog.LevelWriter
	if *httpMode {
		// In HTTP mode, write to both console and file
		consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
		multi = zerolog.MultiLevelWriter(consoleWriter, logFile)
	} else {
		// In stdio mode, write only to file to avoid breaking the stdio protocol
		multi = zerolog.MultiLevelWriter(logFile)
	}

	// Set global logger
	logger := zerolog.New(multi).With().Timestamp().Caller().Logger()
	zlog.Logger = logger

	// Store logger in context
	ctx = logger.WithContext(ctx)

	// Log startup
	logger.Info().
		Str("mode", map[bool]string{true: "http", false: "stdio"}[*httpMode]).
		Str("logFile", *logFilePath).
		Str("logLevel", level.String()).
		Msg("Starting TaskMCP server")

	// Create MCP server
	s := server.NewMCPServer(
		"TaskMCP",
		"1.0.0",
		server.WithToolCapabilities(true), // Enable tool capabilities
		server.WithResourceCapabilities(true, false), // Enable resource capabilities
		server.WithInstructions("TaskMCP allows you to run tasks from Taskfile.yaml as tools"),
	)

	registry := &TaskRegistry{
		server:      s,
		tasksByName: make(map[string]*ast.Task),
		toolNames:   make(map[string]string),
	}

	// Find and load Taskfile
	var taskFileToLoad string
	if *taskfilePath != "" {
		taskFileToLoad = *taskfilePath
	} else {
		// Auto-detect Taskfile
		path, err := taskfile.ExistsWalk(".")
		if err != nil {
			logger.Fatal().Err(err).Msg("Failed to find Taskfile")
		}
		taskFileToLoad = path
	}

	// Load tools from Taskfile
	tools, err := registry.loadTaskfileHandler(ctx, taskFileToLoad, false)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to load Taskfile")
	}

	// Register all the tools
	for taskName, tool := range tools {
		taskNameCopy := taskName // Create a copy to avoid closure-related issues
		s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return registry.executeTaskHandler(ctx, req, taskNameCopy)
		})
		logger.Info().Str("task", taskName).Msg("Registered tool for task")
	}

	logger.Info().
		Int("taskCount", len(tools)).
		Str("taskfile", taskFileToLoad).
		Msg("Loaded tasks from Taskfile")

	// Start server based on mode
	if *httpMode {
		// HTTP mode with SSE
		logger.Info().Str("address", *httpAddr).Msg("Starting HTTP server")

		// Create SSE server
		sseServer := server.NewSSEServer(s)

		// Create a custom HTTP server with our logger middleware
		httpServer := &http.Server{
			Addr:    *httpAddr,
			Handler: loggerMiddleware(sseServer, logger),
		}

		// Start the HTTP server
		logger.Info().Str("address", *httpAddr).Msg("Server is ready to accept connections")
		if err := httpServer.ListenAndServe(); err != nil {
			logger.Fatal().Err(err).Msg("HTTP server error")
		}
	} else {
		// Stdio mode
		logger.Info().Msg("Starting stdio server")

		// We need to direct all errors to our log file only
		// Create a custom io.Writer that writes to our zerolog instance
		errorWriter := &logWriter{
			logger: logger.With().Str("component", "stderr").Logger(),
		}

		// Create a standard library logger that writes to our custom writer
		stdLogger := log.New(errorWriter, "", 0)

		// Set up stdio server options
		stdioOpts := []server.StdioOption{
			server.WithErrorLogger(stdLogger),
		}

		// In stdio mode, make sure we restore stdout to normal before starting
		// This ensures proper stdio communication
		stdoutBackup := os.Stdout
		stderrBackup := os.Stderr

		// IMPORTANT: We need to fully restore stdout for proper stdio mode communication
		// No writing to stdout from this point on except through the server itself
		os.Stdout = stdoutBackup

		// Set a better logging level for debug mode
		if logLevel := os.Getenv("MCP_LOG_LEVEL"); logLevel != "" {
			if level, err := zerolog.ParseLevel(logLevel); err == nil {
				zerolog.SetGlobalLevel(level)
				logger.Info().Str("log_level", level.String()).Msg("Set log level from environment")
			}
		}

		// Start the stdio server - pass a descriptive server name
		logger.Info().Msg("Starting ServeStdio - no more logging to stdout after this point")
		if err := server.ServeStdio(s, stdioOpts...); err != nil {
			// If there's an error, make sure we're logging to stderr only
			os.Stdout = stderrBackup
			logger.Fatal().Err(err).Msg("Server error")
			os.Exit(1)
		}
	}
}

func (r *TaskRegistry) loadTaskfileHandler(ctx context.Context, filepathd string, watch bool) (map[string]mcp.Tool, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("filepath", filepathd).Msg("Loading Taskfile")

	if filepathd == "" {
		path, err := taskfile.ExistsWalk(".")
		if err != nil {
			return nil, errors.Errorf("failed to find Taskfile: %w", err)
		}
		filepathd = path
		logger.Debug().Str("detected_path", path).Msg("Auto-detected Taskfile path")
	}

	// Make sure the file exists
	absPath, err := filepath.Abs(filepathd)
	if err != nil {
		return nil, errors.Errorf("failed to resolve path: %w", err)
	}

	// Check if file exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return nil, errors.Errorf("Taskfile not found at %s", absPath)
	}

	logger.Debug().Str("absolute_path", absPath).Msg("Reading Taskfile")

	// Load the taskfile
	result, err := r.loadTaskfileFromPath(ctx, absPath)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (r *TaskRegistry) loadTaskfileFromPath(ctx context.Context, absPath string) (map[string]mcp.Tool, error) {
	logger := zerolog.Ctx(ctx)

	// Read the taskfile
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, errors.Errorf("reading Taskfile: %w", err)
	}

	logger.Debug().Int("bytes_read", len(data)).Msg("Read Taskfile")

	// Parse the taskfile using raw yaml to extract task names and details
	var taskfileRaw map[string]interface{}
	if err := yaml.Unmarshal(data, &taskfileRaw); err != nil {
		return nil, errors.Errorf("parsing Taskfile: %w", err)
	}

	// Extract tasks from the raw map
	tasksMap, ok := taskfileRaw["tasks"].(map[string]interface{})
	if !ok {
		return nil, errors.New("no tasks found in Taskfile")
	}

	logger.Debug().Int("task_count", len(tasksMap)).Msg("Found tasks in Taskfile")

	// Parse the taskfile to get the AST for detailed task info
	var taskfileData ast.Taskfile
	if err := yaml.Unmarshal(data, &taskfileData); err != nil {
		return nil, errors.Errorf("parsing Taskfile AST: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Store parsed taskfile and path
	r.taskfile = &taskfileData
	r.filePath = absPath

	// Clear and rebuild the task map
	r.tasksByName = make(map[string]*ast.Task)
	r.toolNames = make(map[string]string)

	tools := make(map[string]mcp.Tool)

	// Loop through task names from the raw map
	for taskName := range tasksMap {
		// Try to get task details from AST
		taskData, ok := taskfileData.Tasks.Get(taskName)
		if !ok {
			logger.Warn().Str("task", taskName).Msg("Failed to get AST data for task")
			continue
		}

		// Store task by name
		r.tasksByName[taskName] = taskData

		// Create tool for this task
		tool := r.createTaskAsTool(ctx, taskName, taskData)
		tools[taskName] = tool

		// Store tool ID
		toolID := fmt.Sprintf("task_%s", strings.ReplaceAll(taskName, ":", "_"))
		r.toolNames[taskName] = toolID

		logger.Debug().
			Str("task", taskName).
			Str("tool_id", toolID).
			Str("description", taskData.Desc).
			Msg("Created tool for task")
	}

	logger.Info().
		Int("task_count", len(tools)).
		Str("file_path", absPath).
		Msg("Successfully loaded Taskfile")

	return tools, nil
}

func (r *TaskRegistry) createTaskAsTool(ctx context.Context, taskName string, task *ast.Task) mcp.Tool {
	logger := zerolog.Ctx(ctx)

	// Create a tool for this task
	description := task.Desc
	if description == "" {
		description = fmt.Sprintf("Run task '%s'", taskName)
	}

	toolID := fmt.Sprintf("task_%s", strings.ReplaceAll(taskName, ":", "_")) // Sanitize the task name for MCP

	toolOpts := []mcp.ToolOption{
		mcp.WithDescription(description),
	}

	// Add parameters for vars if any
	if task.Vars != nil && task.Vars.Len() > 0 {
		// Extract vars from the task
		varsFromTask := extractVars(task)

		logger.Debug().
			Str("task", taskName).
			Int("var_count", len(varsFromTask)).
			Msg("Adding variables as parameters")

		for varName := range varsFromTask {
			// Add as optional string parameter
			toolOpts = append(toolOpts, mcp.WithString(
				varName,
				mcp.Description(fmt.Sprintf("Variable '%s' for task '%s'", varName, taskName)),
			))
		}
	}

	// Create the tool with all options
	return mcp.NewTool(toolID, toolOpts...)
}

func (r *TaskRegistry) executeTaskHandler(ctx context.Context, request mcp.CallToolRequest, taskName string) (*mcp.CallToolResult, error) {
	logger := zerolog.Ctx(ctx)

	logger.Info().
		Str("task", taskName).
		Interface("arguments", request.Params.Arguments).
		Msg("Executing task")

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Get variables from request that match task vars
	vars := make(map[string]string)
	task, ok := r.tasksByName[taskName]
	if !ok {
		logger.Error().Str("task", taskName).Msg("Task not found")
		return nil, errors.Errorf("task '%s' not found", taskName)
	}

	// Extract variable values from the request
	varsFromTask := extractVars(task)
	for varName := range varsFromTask {
		if val, ok := request.Params.Arguments[varName].(string); ok {
			vars[varName] = val
			logger.Debug().
				Str("task", taskName).
				Str("var", varName).
				Str("value", val).
				Msg("Found variable for task")
		}
	}

	// Simulate task execution
	varStr := ""
	if len(vars) > 0 {
		varJSON, _ := json.Marshal(vars)
		varStr = fmt.Sprintf(" with variables: %s", string(varJSON))
	}

	result := fmt.Sprintf("Would execute task '%s'%s", taskName, varStr)
	logger.Info().
		Str("task", taskName).
		Int("var_count", len(vars)).
		Msg("Task execution simulated")

	return mcp.NewToolResultText(result), nil
}

func extractCommands(task *ast.Task) []string {
	commands := []string{}
	for _, cmd := range task.Cmds {
		if cmd.Cmd != "" {
			commands = append(commands, cmd.Cmd)
		}
	}
	return commands
}

func extractDeps(task *ast.Task) []string {
	deps := []string{}
	for _, dep := range task.Deps {
		deps = append(deps, dep.Task)
	}
	return deps
}

func extractVars(task *ast.Task) map[string]string {
	if task.Vars == nil {
		return make(map[string]string)
	}

	vars := make(map[string]string)

	// Use custom approach to work around iterator issues
	// Convert to YAML and back to map to get the var names
	data, err := yaml.Marshal(task.Vars)
	if err == nil {
		var varsMap map[string]interface{}
		if err := yaml.Unmarshal(data, &varsMap); err == nil {
			for k, v := range varsMap {
				vars[k] = fmt.Sprintf("%v", v)
			}
		}
	}

	return vars
}

// logWriter implements io.Writer interface for zerolog logging

package main

import (
	"context"
	"flag"
	"os"
	"strconv"
	"time"

	"github.com/rs/zerolog"
	"github.com/walteh/cloudstack-mcp/pkg/cloudstack"
	"github.com/walteh/cloudstack-mcp/pkg/mcp"
)

func main() {
	// Create context
	ctx := context.Background()

	// Configure zerolog
	zerolog.TimeFieldFormat = time.RFC3339
	logLevel := zerolog.InfoLevel
	if lvl, err := zerolog.ParseLevel(os.Getenv("LOG_LEVEL")); err == nil {
		logLevel = lvl
	}
	zerolog.SetGlobalLevel(logLevel)

	// Create console writer
	consoleWriter := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}

	// Set up logger
	logger := zerolog.New(consoleWriter).With().Timestamp().Caller().Logger()
	// Set global logger and update context
	zerolog.DefaultContextLogger = &logger
	ctx = logger.WithContext(ctx)

	// Parse command-line flags
	apiURL := flag.String("api-url", getEnv("CLOUDSTACK_API_URL", "http://localhost:8080/client/api"), "CloudStack API URL")
	apiKey := flag.String("api-key", getEnv("CLOUDSTACK_API_KEY", ""), "CloudStack API Key")
	secretKey := flag.String("secret-key", getEnv("CLOUDSTACK_SECRET_KEY", ""), "CloudStack Secret Key")
	timeoutStr := flag.String("timeout", getEnv("CLOUDSTACK_TIMEOUT", "60"), "CloudStack API Timeout in seconds")
	addr := flag.String("addr", getEnv("MCP_ADDR", ":8250"), "Address to listen on")
	flag.Parse()

	// Parse timeout
	timeout, err := strconv.ParseInt(*timeoutStr, 10, 64)
	if err != nil {
		logger.Fatal().Err(err).Msg("Invalid timeout value")
	}

	// Create CloudStack client config
	config := &cloudstack.Config{
		APIURL:    *apiURL,
		APIKey:    *apiKey,
		SecretKey: *secretKey,
		Timeout:   timeout,
	}

	// Create CloudStack client
	client, err := cloudstack.NewClient(config)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create CloudStack client")
	}

	// Create and start MCP server
	server := mcp.NewServer(ctx, client)
	logger.Info().Str("address", *addr).Msg("Starting CloudStack MCP server")
	if err := server.Start(ctx, *addr); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start server")
	}
}

// getEnv retrieves an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

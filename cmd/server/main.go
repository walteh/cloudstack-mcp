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
	username := flag.String("username", getEnv("CLOUDSTACK_USERNAME", ""), "CloudStack Username (if API keys not provided)")
	password := flag.String("password", getEnv("CLOUDSTACK_PASSWORD", ""), "CloudStack Password (if API keys not provided)")
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

	// Check if we need to obtain API keys using username/password
	if (config.APIKey == "" || config.SecretKey == "") && *username != "" && *password != "" {
		logger.Info().Msg("API keys not provided, attempting to get them using username/password")

		// Get API keys using the CloudStack Go SDK directly
		apiKey, secretKey, err := cloudstack.GetAPICredentials(ctx, *apiURL, *username, *password)
		if err != nil {
			logger.Fatal().Err(err).Msg("Failed to get API credentials")
		}

		logger.Info().Msg("Successfully obtained API keys")
		config.APIKey = apiKey
		config.SecretKey = secretKey
	}

	// Create CloudStack client
	// client, err := cloudstack.NewClient(config)
	// if err != nil {
	// 	logger.Fatal().Err(err).Msgf("Failed to create CloudStack client\n\n")
	// }

	// Create and start MCP server
	server, err := mcp.NewServer(ctx, *username, *password, *apiURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create MCP server")
	}

	tools, err := server.CreateToolForEachApi(ctx)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create tools")
	}

	listToolsStr := []string{}
	for _, tool := range tools {
		listToolsStr = append(listToolsStr, tool.Name)
	}

	logger.Info().Interface("tools", listToolsStr).Msg("Created tools")

	// Log info about the dynamic tools
	logger.Info().
		Str("address", *addr).
		Msg("Starting CloudStack MCP server with dynamic tool support")

	// Start the server
	if err := server.Start(ctx, *addr); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start server")
	}
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}
	return value
}

// getAPICredentials tries to obtain API keys using username/password authentication

// http://localhost:8080/client/api?command=registerUserKeys&id=1952b104-acce-11ef-ae80-0242ac110002&response=json&sessionkey=s2c6DH5nJO-b7s1TbK80w_CCTTk
// http://localhost:8080/client/api?command=registerUserKeys&id=1952b104-acce-11ef-ae80-0242ac110002&sessionkey=52m3oEDfr-6gbbkhHdKC3BtSraI&response=json

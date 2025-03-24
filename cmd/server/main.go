package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
	"github.com/walteh/cloudstack-mcp/pkg/cloudstack"
	"github.com/walteh/cloudstack-mcp/pkg/lmcp"
	"github.com/walteh/cloudstack-mcp/pkg/mcp"
)

func main() {
	// Create context
	ctx := context.Background()

	// Parse command-line flags
	apiURL := flag.String("api-url", getEnv("CLOUDSTACK_API_URL", "http://localhost:8080/client/api"), "CloudStack API URL")
	apiKey := flag.String("api-key", getEnv("CLOUDSTACK_API_KEY", ""), "CloudStack API Key")
	secretKey := flag.String("secret-key", getEnv("CLOUDSTACK_SECRET_KEY", ""), "CloudStack Secret Key")
	username := flag.String("username", getEnv("CLOUDSTACK_USERNAME", "admin"), "CloudStack Username (if API keys not provided)")
	password := flag.String("password", getEnv("CLOUDSTACK_PASSWORD", "password"), "CloudStack Password (if API keys not provided)")
	timeoutStr := flag.String("timeout", getEnv("CLOUDSTACK_TIMEOUT", "60"), "CloudStack API Timeout in seconds")
	addr := flag.String("addr", getEnv("MCP_ADDR", ":8250"), "Address to listen on")
	disableLogFile := flag.Bool("disable-log-file", false, "Disable log file")
	printLogDir := flag.Bool("print-log-dir", false, "Print log directory")
	http := flag.Bool("http", true, "Run in HTTP mode")
	flag.Parse()

	if *printLogDir {
		logdir, err := lmcp.MyLogFileDir()
		if err != nil {
			fmt.Println(err)
		}
		fmt.Println(logdir)
		os.Exit(0)
	}

	// Parse timeout
	timeout, err := strconv.ParseInt(*timeoutStr, 10, 64)
	if err != nil {
		fmt.Println(err)
	}

	// Create CloudStack client config
	config := &cloudstack.Config{
		APIURL:    *apiURL,
		APIKey:    *apiKey,
		SecretKey: *secretKey,
		Timeout:   timeout,
	}

	logfunc, err := lmcp.WrapMCPServerWithLogging(ctx, lmcp.LMCPOpts{
		HTTPMode:       *http,
		HTTPAddr:       *addr,
		DisableLogFile: *disableLogFile,
		LogLevelStr:    "trace",
	})
	if err != nil {
		fmt.Println(err)
	}

	// Start the server
	if err := logfunc(ctx, func(ctx context.Context) (*server.MCPServer, error) {
		server, err := setupServer(ctx, config, username, password, apiURL)
		if err != nil {
			return nil, err
		}
		return server.Server(), nil
	}); err != nil {
		fmt.Println(err)
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

func setupServer(ctx context.Context, config *cloudstack.Config, username, password, apiURL *string) (*mcp.Server, error) {

	logger := zerolog.Ctx(ctx)

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

	// if *username == "" || *password == "" {
	// 	logger.Fatal().Msg("Username and password are required")
	// }

	loggerd := logger.Level(zerolog.ErrorLevel)

	ctx = loggerd.WithContext(ctx)

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

	logger.Trace().Int("tools", len(listToolsStr)).Msg("Created tools")

	return server, nil
}

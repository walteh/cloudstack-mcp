package main

import (
	"flag"
	"log"
	"os"
	"strconv"

	"github.com/walteh/cloudstack-mcp/pkg/cloudstack"
	"github.com/walteh/cloudstack-mcp/pkg/mcp"
)

func main() {
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
		log.Fatalf("Invalid timeout value: %v", err)
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
		log.Fatalf("Failed to create CloudStack client: %v", err)
	}

	// Create and start MCP server
	server := mcp.NewServer(client)
	log.Printf("Starting CloudStack MCP server on %s", *addr)
	if err := server.Start(*addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
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

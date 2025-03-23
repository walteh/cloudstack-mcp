package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	csgo "github.com/apache/cloudstack-go/v2/cloudstack"
	"github.com/rs/zerolog"
	"github.com/walteh/cloudstack-mcp/pkg/cloudstack"
	"github.com/walteh/cloudstack-mcp/pkg/mcp"
	"gitlab.com/tozd/go/errors"
	"moul.io/http2curl"
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
		apiKey, secretKey, err := getAPICredentials(ctx, *apiURL, *username, *password)
		if err != nil {
			logger.Fatal().Err(err).Msg("Failed to get API credentials")
		}

		logger.Info().Msg("Successfully obtained API keys")
		config.APIKey = apiKey
		config.SecretKey = secretKey
	}

	// Create CloudStack client
	client, err := cloudstack.NewClient(config)
	if err != nil {
		logger.Fatal().Err(err).Msgf("Failed to create CloudStack client\n\n")
	}

	// Create and start MCP server
	server := mcp.NewServer(ctx, client)
	logger.Info().Str("address", *addr).Msg("Starting CloudStack MCP server")
	if err := server.Start(ctx, *addr); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start server")
	}
}

// getAPICredentials tries to obtain API keys using username/password authentication
func getAPICredentials(ctx context.Context, apiURL, username, password string) (string, string, error) {
	logger := zerolog.Ctx(ctx)

	// Create a CloudStack client
	// verifySsl := false

	// loginParams := adminClient.Authentication.NewLoginParams(username, password)
	// loginParams.SetDomain("")

	logger.Info().Msgf("Logging in with username: %s, password: %s", username, password)

	// res, err := adminClient.Authentication.Login(loginParams)
	// if err != nil {
	// 	return "", "", errors.Errorf("logging in: %w", err)
	// }

	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", "", errors.Errorf("failed to create cookie jar: %w", err)
	}

	httpClient := &http.Client{
		Timeout: time.Second * 10,
		Jar:     jar,
	}

	// this creates a JSESSIONID cookie that needs to be used for all authenticated requests
	lres, err := makeRawCloudStackRequest[csgo.LoginResponse](ctx, httpClient, apiURL, url.Values{"command": {"login"}, "username": {username}, "password": {password}})
	if err != nil {
		return "", "", errors.Errorf("logging in: %w", err)
	}

	// adminClient := csgo.NewAsyncClient(apiURL, "", "", false)

	// user, err := adminClient.User.ListUsers(res.l)
	// if err != nil {
	// 	return "", "", errors.Errorf("listing users: %w", err)
	// }

	// if len(user.Users) == 0 {
	// 	return "", "", errors.Errorf("no users found")
	// }

	cres, err := makeRawCloudStackRequest[csgo.RegisterUserKeysResponse](ctx, httpClient, apiURL, url.Values{"command": {"registerUserKeys"}, "id": {lres.Userid}, "sessionkey": {lres.Sessionkey}})
	if err != nil {
		return "", "", errors.Errorf("registering user keys: %w", err)
	}

	// User exists, check for API keys
	// if res.Apikey == "" {
	// 	// No API keys, generate them

	// 	return keys.Apikey, keys.Secretkey, nil
	// }

	// Return existing API keys
	return cres.Apikey, cres.Secretkey, nil
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}
	return value
}

func makeRawCloudStackRequest[T any](ctx context.Context, client *http.Client, apiURL string, params url.Values) (*T, error) {

	logger := zerolog.Ctx(ctx)

	adminLoginURL := fmt.Sprintf("%s?response=json&%s", apiURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "POST", adminLoginURL, nil)
	if err != nil {
		return nil, errors.Errorf("failed to create request: %w", err)
	}

	// req.Header.Set("Content-Type", "application/json")
	// req.Header.Set("Accept", "application/json")
	// req.Header.Set("User-Agent", "CloudStack MCP")

	curl, err := http2curl.GetCurlCommand(req)
	if err != nil {
		return nil, errors.Errorf("failed to get curl command: %w", err)
	}

	// get the name of the type with a lower case full name no package

	logger.Info().Msgf("curl: %s", curl.String())
	logger.Info().Msgf("Making request to: %s", adminLoginURL)

	// before cookies
	for _, cookie := range client.Jar.Cookies(req.URL) {
		logger.Info().Msgf("BEFORE Cookie: %s", cookie.String())
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	cookies := resp.Cookies()

	for _, cookie := range cookies {
		logger.Info().Msgf("AFTER Cookie: %s", cookie.String())
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, errors.Errorf("HTTP request failed: %d: %s", resp.StatusCode, string(body))
	}

	logger.Info().Msgf("Response code: %d", resp.StatusCode)
	logger.Info().Msgf("Response body: %s", string(body))

	var res T

	typ := reflect.TypeOf(res)
	typeName := typ.String()
	typeName = strings.TrimPrefix(typeName, "cloudstack.")
	typeName = strings.ToLower(typeName)

	logger.Info().Msgf("Type name: %s", typeName)

	var mapper map[string]any

	err = json.Unmarshal(body, &mapper)
	if err != nil {
		return nil, errors.Errorf("unmarshalling raw response: %w", err)
	}

	data, ok := mapper[typeName]
	if !ok {
		return nil, errors.Errorf("type not found in response: %s", typeName)
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, errors.Errorf("marshalling data: %w", err)
	}

	err = json.Unmarshal(jsonData, &res)
	if err != nil {
		return nil, errors.Errorf("unmarshalling raw response: %w", err)
	}

	return &res, nil

}

// http://localhost:8080/client/api?command=registerUserKeys&id=1952b104-acce-11ef-ae80-0242ac110002&response=json&sessionkey=s2c6DH5nJO-b7s1TbK80w_CCTTk
// http://localhost:8080/client/api?command=registerUserKeys&id=1952b104-acce-11ef-ae80-0242ac110002&sessionkey=52m3oEDfr-6gbbkhHdKC3BtSraI&response=json

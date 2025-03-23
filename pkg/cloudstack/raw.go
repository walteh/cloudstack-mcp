package cloudstack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/apache/cloudstack-go/v2/cloudstack"
	"github.com/rs/zerolog"
	errors "gitlab.com/tozd/go/errors"
	"moul.io/http2curl"
)

func DoTypedCloudStackRequest[T any](ctx context.Context, apiURL string, toolName string, username, password string, params map[string]string) (*T, error) {

	raw, err := DoRawCloudStackRequest(ctx, apiURL, toolName, username, password, params)
	if err != nil {
		return nil, errors.Errorf("error calling %s: %w", toolName, err)
	}

	return extractTypeFromResponse[T](ctx, raw)

}

func DoRawCloudStackRequest(ctx context.Context, apiURL string, toolName string, username, password string, params map[string]string) (json.RawMessage, error) {
	// Convert the params map to url.Values which is what the CloudStack client expects

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, errors.Errorf("failed to create cookie jar: %w", err)
	}

	httpClient := &http.Client{
		Timeout: time.Second * 10,
		Jar:     jar,
	}

	// this creates a JSESSIONID cookie that needs to be used for all authenticated requests
	lres, err := makeTypedCloudStackRequest[cloudstack.LoginResponse](ctx, httpClient, apiURL, url.Values{"command": {"login"}, "username": {username}, "password": {password}})
	if err != nil {
		return nil, errors.Errorf("logging in: %w", err)
	}

	values := url.Values{}
	values.Set("command", toolName)
	for k, v := range params {
		values.Set(k, v)
	}

	values.Set("sessionkey", lres.Sessionkey)

	mres, err := makeRawCloudStackRequest(ctx, httpClient, apiURL, values)
	if err != nil {
		return nil, errors.Errorf("error calling %s: %w", toolName, err)
	}

	return mres, nil
}

func GetAPICredentials(ctx context.Context, apiURL, username, password string) (string, string, error) {
	logger := zerolog.Ctx(ctx)

	logger.Info().Msgf("Logging in with username: %s, password: %s", username, password)

	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", "", errors.Errorf("failed to create cookie jar: %w", err)
	}

	httpClient := &http.Client{
		Timeout: time.Second * 10,
		Jar:     jar,
	}

	// this creates a JSESSIONID cookie that needs to be used for all authenticated requests
	lres, err := makeTypedCloudStackRequest[cloudstack.LoginResponse](ctx, httpClient, apiURL, url.Values{"command": {"login"}, "username": {username}, "password": {password}})
	if err != nil {
		return "", "", errors.Errorf("logging in: %w", err)
	}

	cres, err := makeTypedCloudStackRequest[cloudstack.RegisterUserKeysResponse](ctx, httpClient, apiURL, url.Values{"command": {"registerUserKeys"}, "id": {lres.Userid}, "sessionkey": {lres.Sessionkey}})
	if err != nil {
		return "", "", errors.Errorf("registering user keys: %w", err)
	}

	// Return existing API keys
	return cres.Apikey, cres.Secretkey, nil
}

func extractTypeFromResponse[T any](ctx context.Context, raw json.RawMessage) (*T, error) {
	logger := zerolog.Ctx(ctx)

	var res T

	typ := reflect.TypeOf(res)
	typeName := typ.String()
	typeName = strings.TrimPrefix(typeName, "cloudstack.")
	typeName = strings.ToLower(typeName)

	logger.Info().Msgf("Type name: %s", typeName)

	var mapper map[string]any

	err := json.Unmarshal(raw, &mapper)
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

// getEnv gets an environment variable or returns a default value

func makeTypedCloudStackRequest[T any](ctx context.Context, client *http.Client, apiURL string, params url.Values) (*T, error) {

	raw, err := makeRawCloudStackRequest(ctx, client, apiURL, params)
	if err != nil {
		return nil, errors.Errorf("error calling %s: %w", params.Get("command"), err)
	}

	return extractTypeFromResponse[T](ctx, raw)
}

func makeRawCloudStackRequest(ctx context.Context, client *http.Client, apiURL string, params url.Values) (json.RawMessage, error) {

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
	logger.Trace().Msgf("Response body: %s", string(body))

	return json.RawMessage(body), nil

}

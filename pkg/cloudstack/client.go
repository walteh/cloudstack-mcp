package cloudstack

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/apache/cloudstack-go/v2/cloudstack"
	errors "gitlab.com/tozd/go/errors"
)

//go:mock
type API interface {
}

// Client represents a CloudStack API client
type Client struct {
	cs     *cloudstack.CloudStackClient
	config *Config
	mu     sync.RWMutex
}

// Config holds the CloudStack API connection configuration
type Config struct {
	APIURL    string
	APIKey    string
	SecretKey string
	Timeout   int64
}

// NewClient creates a new CloudStack API client
func NewClient(config *Config) (*Client, error) {
	if config.APIURL == "" {
		return nil, errors.New("CloudStack API URL is required")
	}

	client := &Client{
		config: config,
	}

	verifySsl := false

	// Create the CloudStack API client
	cs := cloudstack.NewAsyncClient(
		config.APIURL,
		config.APIKey,
		config.SecretKey,
		verifySsl,
	)

	// Set the timeout
	if config.Timeout > 0 {
		cs.Timeout(time.Duration(config.Timeout) * time.Second)
	}

	client.cs = cs
	return client, nil
}

// GetAPIClient returns the underlying CloudStack API client
func (c *Client) GetAPIClient() *cloudstack.CloudStackClient {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cs
}

// ListTemplates retrieves a list of templates from CloudStack
func (c *Client) ListTemplates() ([]map[string]string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	p := c.cs.Template.NewListTemplatesParams("featured")
	resp, err := c.cs.Template.ListTemplates(p)
	if err != nil {
		return nil, errors.Errorf("error listing templates: %w", err)
	}

	result := make([]map[string]string, 0, len(resp.Templates))
	for _, t := range resp.Templates {
		template := map[string]string{
			"id":          t.Id,
			"name":        t.Name,
			"displayText": t.Displaytext,
			"status":      t.Status,
		}
		result = append(result, template)
	}

	return result, nil
}

// GetDefaultZone retrieves the default zone from CloudStack
func (c *Client) GetDefaultZone() (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	p := c.cs.Zone.NewListZonesParams()
	resp, err := c.cs.Zone.ListZones(p)
	if err != nil {
		return "", errors.Errorf("error listing zones: %w", err)
	}

	// Return the ID of the first available zone
	if len(resp.Zones) > 0 {
		return resp.Zones[0].Id, nil
	}

	return "", errors.New("no zones found")
}

// DeployVM deploys a new virtual machine in CloudStack
func (c *Client) DeployVM(name, templateID, serviceOfferingID, zoneID string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	p := c.cs.VirtualMachine.NewDeployVirtualMachineParams(serviceOfferingID, templateID, zoneID)
	p.SetName(name)

	resp, err := c.cs.VirtualMachine.DeployVirtualMachine(p)
	if err != nil {
		return "", errors.Errorf("error deploying VM: %w", err)
	}

	return resp.Id, nil
}

// GetVMStatus retrieves the status of a virtual machine
func (c *Client) GetVMStatus(id string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	p := c.cs.VirtualMachine.NewListVirtualMachinesParams()
	p.SetId(id)

	resp, err := c.cs.VirtualMachine.ListVirtualMachines(p)
	if err != nil {
		return "", errors.Errorf("error getting VM status: %w", err)
	}

	if len(resp.VirtualMachines) == 0 {
		return "", errors.Errorf("VM with ID %s not found", id)
	}

	return resp.VirtualMachines[0].State, nil
}

func (c *Client) ListApis(ctx context.Context, params *cloudstack.ListApisParams) ([]*cloudstack.Api, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	resp, err := c.cs.APIDiscovery.ListApis(params)
	if err != nil {
		return nil, errors.Errorf("error listing APIs: %w", err)
	}

	return resp.Apis, nil
}

// Request makes a generic request to the CloudStack API using the available service methods
func (c *Client) Request(apiName string, params map[string]string) (interface{}, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// For now, we'll implement a simpler fallback strategy since we can't
	// directly access the underlying client's request method
	if strings.HasPrefix(apiName, "listTemplates") {
		return c.ListTemplates()
	} else if strings.HasPrefix(apiName, "listZones") {
		return c.ListZones()
	} else if strings.HasPrefix(apiName, "deployVirtualMachine") {
		name := params["name"]
		templateID := params["templateid"]
		serviceOfferingID := params["serviceofferingid"]
		zoneID := params["zoneid"]
		if zoneID == "" {
			var err error
			zoneID, err = c.GetDefaultZone()
			if err != nil {
				return nil, errors.Errorf("getting default zone: %w", err)
			}
		}
		vmID, err := c.DeployVM(name, templateID, serviceOfferingID, zoneID)
		if err != nil {
			return nil, err
		}
		return map[string]string{"id": vmID, "status": "deploying"}, nil
	} else if strings.HasPrefix(apiName, "listVirtualMachines") {
		id := params["id"]
		if id != "" {
			status, err := c.GetVMStatus(id)
			if err != nil {
				return nil, err
			}
			return map[string]string{"id": id, "status": status}, nil
		}
	}

	// For unimplemented APIs, return a meaningful error
	return nil, errors.Errorf("API %s not implemented yet", apiName)
}

// ListZones retrieves all zones from CloudStack
func (c *Client) ListZones() ([]map[string]string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	p := c.cs.Zone.NewListZonesParams()
	resp, err := c.cs.Zone.ListZones(p)
	if err != nil {
		return nil, errors.Errorf("error listing zones: %w", err)
	}

	result := make([]map[string]string, 0, len(resp.Zones))
	for _, z := range resp.Zones {
		zone := map[string]string{
			"id":   z.Id,
			"name": z.Name,
		}
		result = append(result, zone)
	}

	return result, nil
}

// callNewRequest is a helper function to call the unexported newRequest method
// on the CloudStack client using reflection

// encodeURLValues creates a URL encoded string of the given values and signs it
// with the given secret key
func encodeURLValues(values url.Values, apiKey, secretKey string) string {
	// Set the apiKey parameter
	values.Set("apiKey", apiKey)

	// Generate signature for the request
	// Sort the parameters alphabetically
	var keys []string
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Create a string of sorted parameters
	var signatureParams strings.Builder
	for _, k := range keys {
		signatureParams.WriteString(k)
		signatureParams.WriteString("=")
		signatureParams.WriteString(url.QueryEscape(values.Get(k)))
	}

	// Calculate signature
	mac := hmac.New(sha1.New, []byte(secretKey))
	mac.Write([]byte(strings.ToLower(signatureParams.String())))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	// Add the signature to the query parameters
	values.Set("signature", signature)

	return values.Encode()
}

package cloudstack

import (
	"fmt"
	"sync"
	"time"

	"github.com/apache/cloudstack-go/v2/cloudstack"
)

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
		return nil, fmt.Errorf("CloudStack API URL is required")
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
		return nil, fmt.Errorf("error listing templates: %v", err)
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
		return "", fmt.Errorf("error listing zones: %v", err)
	}

	// Return the ID of the first available zone
	if len(resp.Zones) > 0 {
		return resp.Zones[0].Id, nil
	}

	return "", fmt.Errorf("no zones found")
}

// DeployVM deploys a new virtual machine in CloudStack
func (c *Client) DeployVM(name, templateID, serviceOfferingID, zoneID string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	p := c.cs.VirtualMachine.NewDeployVirtualMachineParams(serviceOfferingID, templateID, zoneID)
	p.SetName(name)

	resp, err := c.cs.VirtualMachine.DeployVirtualMachine(p)
	if err != nil {
		return "", fmt.Errorf("error deploying VM: %v", err)
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
		return "", fmt.Errorf("error getting VM status: %v", err)
	}

	if len(resp.VirtualMachines) == 0 {
		return "", fmt.Errorf("VM with ID %s not found", id)
	}

	return resp.VirtualMachines[0].State, nil
}

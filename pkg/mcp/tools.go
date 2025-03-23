package mcp

import (
	"context"
	"encoding/json"

	csgo "github.com/apache/cloudstack-go/v2/cloudstack"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
	"github.com/walteh/cloudstack-mcp/pkg/cloudstack"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	errors "gitlab.com/tozd/go/errors"

	"github.com/invopop/jsonschema"
)

func (me *Server) CreateToolForEachApi(ctx context.Context) ([]*mcp.Tool, error) {

	logger := zerolog.Ctx(ctx)

	listOfApisPtr, err := cloudstack.DoTypedCloudStackRequest[csgo.ListApisResponse](ctx, me.apiURL, "listApis", me.username, me.password, map[string]string{})
	if err != nil {
		return nil, errors.Errorf("getting list of APIs: %w", err)
	}

	listOfApis := listOfApisPtr.Apis

	logger.Info().Msgf("List of APIs: %v", listOfApis)

	tools := make([]*mcp.Tool, 0, len(listOfApis))

	for _, api := range listOfApis {
		sch, err := getToolTypes(ctx, api)
		if err != nil {
			return nil, errors.Errorf("getting tool types: %w", err)
		}

		jsonSchema, err := json.Marshal(sch)
		if err != nil {
			return nil, errors.Errorf("marshalling tool types: %w", err)
		}

		tool := mcp.NewToolWithRawSchema(api.Name, api.Description, jsonSchema)

		tools = append(tools, &tool)
	}

	// fpr each named tool get the type from the csgo package and create a tool for each api

	return tools, nil
}

func getToolTypes(ctx context.Context, api *csgo.Api) (*jsonschema.Schema, error) {

	logger := zerolog.Ctx(ctx)

	sch := &jsonschema.Schema{
		Title:       api.Name + "Params",
		Description: api.Description,
		Required:    []string{},
		Type:        "object",
		Properties:  orderedmap.New[string, *jsonschema.Schema](),
	}

	for _, method := range api.Params {
		prop := &jsonschema.Schema{}
		logger.Info().Msgf("Method: %v", method)
		prop.Type = method.Type
		if method.Required {
			prop.Required = append(prop.Required, method.Name)
		}
		sch.Properties.Set(method.Name, prop)
	}

	// get the type from the csgo package

	// convert the type to raw jsonshcmea

	return sch, nil
}

// func jsonSchemaFromType(ctx context.Context, typ *jsonschema.Schema) (*mcp.ToolInputSchema, error) {
// }

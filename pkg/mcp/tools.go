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
		_, err := getToolOptionsFromType(ctx, api)
		if err != nil {
			return nil, errors.Errorf("getting tool types: %w", err)
		}

		typ, err := getToolTypes(ctx, api)
		if err != nil {
			return nil, errors.Errorf("getting tool types: %w", err)
		}

		jsonSchema, err := json.Marshal(typ)
		if err != nil {
			return nil, errors.Errorf("marshalling tool types: %w", err)
		}

		// tool := mcp.NewTool(api.Name, sch...)
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

// func getToolTypesFromType(ctx context.Context, api *csgo.Api) (*mcp.ToolInputSchema, error) {

// 	// logger := zerolog.Ctx(ctx)

// 	maind := &mcp.ToolInputSchema{
// 		Type:       "object",
// 		Properties: map[string]any{},
// 		Required:   []string{},
// 	}

// 	for _, param := range api.Params {
// 		if param.Type == "object" {
// 			maind.Properties[param.Name] = getToolTypesFromType(ctx, param)
// 		} else {
// 			maind.Properties[param.Name] = param.Type
// 		}
// 		if param.Required {
// 			maind.Required = append(maind.Required, param.Name)
// 		}
// 	}

// 	return maind, nil
// }

// func jsonSchemaFromType(ctx context.Context, typ *jsonschema.Schema) (*mcp.ToolInputSchema, error) {
// }

func getToolOptionsFromType(ctx context.Context, api *csgo.Api) ([]mcp.ToolOption, error) {

	toolOpts := []mcp.ToolOption{}

	for _, param := range api.Params {

		propOpts := []mcp.PropertyOption{}

		if param.Required {
			propOpts = append(propOpts, mcp.Required())
		}

		propOpts = append(propOpts, mcp.Description(param.Description))

		// if param.Length != nil {
		// 	propOpts = append(propOpts, mcp.MaxLength(param.Length))
		// }

		switch param.Type {
		case "string":
			toolOpts = append(toolOpts, mcp.WithString(param.Name, propOpts...))
		case "number", "integer", "long", "short":
			toolOpts = append(toolOpts, mcp.WithNumber(param.Name, propOpts...))
		case "boolean":
			toolOpts = append(toolOpts, mcp.WithBoolean(param.Name, propOpts...))
		case "object", "map":
			propOpts = append(propOpts, mcp.Properties(map[string]any{}))
			toolOpts = append(toolOpts, mcp.WithObject(param.Name, propOpts...))
		case "array", "list":
			propOpts = append(propOpts, mcp.Items(map[string]any{
				"type": "string",
			}))
			toolOpts = append(toolOpts, mcp.WithArray(param.Name, propOpts...))
			// pp.Println(param)
		case "uuid":
			propOpts = append(propOpts, mcp.Pattern("[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}"))
			toolOpts = append(toolOpts, mcp.WithString(param.Name, propOpts...))
		case "date":
			propOpts = append(propOpts, mcp.Pattern("^\\d{4}-\\d{2}-\\d{2}$"))
			toolOpts = append(toolOpts, mcp.WithString(param.Name, propOpts...))
		case "datetime":
			propOpts = append(propOpts, mcp.Pattern("^\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}Z$"))
			toolOpts = append(toolOpts, mcp.WithString(param.Name, propOpts...))
		default:
			return nil, errors.Errorf("unknown type: %s", param.Type)
		}

	}

	return toolOpts, nil
}

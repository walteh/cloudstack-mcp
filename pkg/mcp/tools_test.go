package mcp_test

import (
	"testing"

	csgo "github.com/apache/cloudstack-go/v2/cloudstack"
	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/require"
	"github.com/walteh/cloudstack-mcp/pkg/mcp"
	orderedmap "github.com/wk8/go-ordered-map/v2"
)

func Test_CloudStackApiToJsonSchema(t *testing.T) {
	type args struct {
		api *csgo.Api
	}
	tests := []struct {
		name    string
		args    args
		want    *jsonschema.Schema
		wantErr bool
	}{
		{
			name: "test",
			args: args{
				api: &csgo.Api{
					Name:        "test",
					Description: "test",
					Params: []csgo.ApiParams{
						{
							Name:        "test",
							Description: "test",
							Type:        "string",
						},
					},
				},
			},
			want: &jsonschema.Schema{
				Title:       "testInputParams",
				Description: "test",
				Required:    []string{},
				Type:        "object",
				Properties: orderedmap.New[string, *jsonschema.Schema](orderedmap.WithInitialData(orderedmap.Pair[string, *jsonschema.Schema]{
					Key:   "tests",
					Value: &jsonschema.Schema{Type: "string"},
				})),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mcp.CloudStackApiToJsonSchema(t.Context(), tt.args.api)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

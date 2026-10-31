// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/bgovanlu/terraform-provider-cmdash/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &integrationConnectionsDataSource{}
	_ datasource.DataSourceWithConfigure = &integrationConnectionsDataSource{}
)

func NewIntegrationConnectionsDataSource() datasource.DataSource {
	return &integrationConnectionsDataSource{}
}

type integrationConnectionsDataSource struct {
	client *client.Client
}

type integrationConnectionModel struct {
	ID             types.String `tfsdk:"id"`
	Provider       types.String `tfsdk:"provider_name"`
	Status         types.String `tfsdk:"status"`
	LastSyncAt     types.String `tfsdk:"last_sync_at"`
	LastSyncStatus types.String `tfsdk:"last_sync_status"`
}

type integrationConnectionsDataSourceModel struct {
	ID          types.String                 `tfsdk:"id"`
	Connections []integrationConnectionModel `tfsdk:"connections"`
}

func (d *integrationConnectionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_integration_connections"
}

func (d *integrationConnectionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the tenant's connected integrations (GET /v2/integrations). READ ONLY: this is the roadmap item's `cmdash_integration_connection` concept, but public API v2 has no POST (create) or DELETE route for a connection — only list, per-connection health, and an on-demand sync trigger. A connection can only be created/removed from the authenticated dashboard today. See docs/terraform-provider.md.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Synthetic id for this data source's read (\"integrations\")."},
			"connections": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":               schema.StringAttribute{Computed: true},
						"provider_name":    schema.StringAttribute{Computed: true, Description: "e.g. \"MICROSOFT_DEFENDER\". Named provider_name (not provider) to avoid colliding with Terraform's own provider block keyword in generated docs/examples."},
						"status":           schema.StringAttribute{Computed: true, Description: "One of PENDING, ACTIVE, UNHEALTHY, DISCONNECTED."},
						"last_sync_at":     schema.StringAttribute{Computed: true},
						"last_sync_status": schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *integrationConnectionsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type", fmt.Sprintf("Expected *client.Client, got: %T", req.ProviderData))
		return
	}
	d.client = c
}

func (d *integrationConnectionsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	conns, err := d.client.ListIntegrationConnections(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error reading integration connections", err.Error())
		return
	}

	var state integrationConnectionsDataSourceModel
	state.ID = types.StringValue("integrations")
	for _, c := range conns {
		lastSyncAt := types.StringNull()
		if c.LastSyncAt != nil {
			lastSyncAt = types.StringValue(*c.LastSyncAt)
		}
		lastSyncStatus := types.StringNull()
		if c.LastSyncStatus != nil {
			lastSyncStatus = types.StringValue(*c.LastSyncStatus)
		}
		state.Connections = append(state.Connections, integrationConnectionModel{
			ID:             types.StringValue(c.ID),
			Provider:       types.StringValue(c.Provider),
			Status:         types.StringValue(c.Status),
			LastSyncAt:     lastSyncAt,
			LastSyncStatus: lastSyncStatus,
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

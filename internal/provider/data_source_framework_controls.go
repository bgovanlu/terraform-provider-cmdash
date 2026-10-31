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
	_ datasource.DataSource              = &frameworkControlsDataSource{}
	_ datasource.DataSourceWithConfigure = &frameworkControlsDataSource{}
)

func NewFrameworkControlsDataSource() datasource.DataSource {
	return &frameworkControlsDataSource{}
}

type frameworkControlsDataSource struct {
	client *client.Client
}

type controlStatusModel struct {
	ControlID types.String `tfsdk:"control_id"`
	Domain    types.String `tfsdk:"domain"`
	Title     types.String `tfsdk:"title"`
	Status    types.String `tfsdk:"status"`
	Notes     types.String `tfsdk:"notes"`
}

type frameworkControlsDataSourceModel struct {
	Slug     types.String         `tfsdk:"slug"`
	Controls []controlStatusModel `tfsdk:"controls"`
}

func (d *frameworkControlsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_framework_controls"
}

func (d *frameworkControlsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Control statuses for one compliance framework (GET /v2/compliance/frameworks/:slug/controls). READ ONLY — assessment status is written from evidence/observations, not from Terraform.",
		Attributes: map[string]schema.Attribute{
			"slug": schema.StringAttribute{
				Required:    true,
				Description: "Framework slug, e.g. \"cmmc-l2\". An unknown slug fails the read with a 404 (v2 behavior; v1 would instead have returned 200 with an error body — see docs/developers/public-api-versioning.md).",
			},
			"controls": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"control_id": schema.StringAttribute{Computed: true},
						"domain":     schema.StringAttribute{Computed: true},
						"title":      schema.StringAttribute{Computed: true},
						"status":     schema.StringAttribute{Computed: true},
						"notes":      schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *frameworkControlsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *frameworkControlsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config frameworkControlsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	controls, err := d.client.GetFrameworkControls(ctx, config.Slug.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading framework controls", err.Error())
		return
	}

	state := frameworkControlsDataSourceModel{Slug: config.Slug}
	for _, c := range controls {
		notes := types.StringNull()
		if c.Notes != nil {
			notes = types.StringValue(*c.Notes)
		}
		state.Controls = append(state.Controls, controlStatusModel{
			ControlID: types.StringValue(c.ControlID),
			Domain:    types.StringValue(c.Domain),
			Title:     types.StringValue(c.Title),
			Status:    types.StringValue(c.Status),
			Notes:     notes,
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

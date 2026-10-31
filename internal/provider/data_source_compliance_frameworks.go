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
	_ datasource.DataSource              = &complianceFrameworksDataSource{}
	_ datasource.DataSourceWithConfigure = &complianceFrameworksDataSource{}
)

func NewComplianceFrameworksDataSource() datasource.DataSource {
	return &complianceFrameworksDataSource{}
}

type complianceFrameworksDataSource struct {
	client *client.Client
}

type frameworkModel struct {
	ID            types.String `tfsdk:"id"`
	Slug          types.String `tfsdk:"slug"`
	Name          types.String `tfsdk:"name"`
	Version       types.String `tfsdk:"version"`
	TotalControls types.Int64  `tfsdk:"total_controls"`
}

type complianceFrameworksDataSourceModel struct {
	ID         types.String     `tfsdk:"id"`
	Frameworks []frameworkModel `tfsdk:"frameworks"`
}

func (d *complianceFrameworksDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_compliance_frameworks"
}

func (d *complianceFrameworksDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the tenant's enabled compliance frameworks (GET /v2/compliance/frameworks). READ ONLY: this is the roadmap item's `cmdash_framework_scope` concept, but public API v2 has no route to enable/disable a framework for a tenant, so it can only be read here, not managed — see docs/terraform-provider.md.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Synthetic id for this data source's read (\"frameworks\").",
			},
			"frameworks": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":             schema.StringAttribute{Computed: true},
						"slug":           schema.StringAttribute{Computed: true, Description: "URL-safe framework identifier, e.g. \"cmmc-l2\"."},
						"name":           schema.StringAttribute{Computed: true},
						"version":        schema.StringAttribute{Computed: true},
						"total_controls": schema.Int64Attribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *complianceFrameworksDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *complianceFrameworksDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	frameworks, err := d.client.ListFrameworks(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error reading compliance frameworks", err.Error())
		return
	}

	var state complianceFrameworksDataSourceModel
	state.ID = types.StringValue("frameworks")
	for _, f := range frameworks {
		state.Frameworks = append(state.Frameworks, frameworkModel{
			ID:            types.StringValue(f.ID),
			Slug:          types.StringValue(f.Slug),
			Name:          types.StringValue(f.Name),
			Version:       types.StringValue(f.Version),
			TotalControls: types.Int64Value(int64(f.TotalControls)),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

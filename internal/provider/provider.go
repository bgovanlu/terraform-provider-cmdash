// SPDX-License-Identifier: Apache-2.0

// Package provider implements the `cmdash` Terraform provider.
// ROADMAP-V70 E31 — see /home/brian/cmdash/docs/terraform-provider.md for
// the full resource/data-source rationale, including which of the
// roadmap's originally-requested resources turned out to be data-source-
// only (or entirely unavailable) once the public API v2 surface was
// checked at source.
package provider

import (
	"context"
	"os"

	"github.com/bgovanlu/terraform-provider-cmdash/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure the implementation satisfies the expected interfaces.
var _ provider.Provider = &CmdashProvider{}

type CmdashProvider struct {
	// version is set by the build (see main.go); "dev" for local builds.
	version string
}

// cmdashProviderModel maps the provider block's schema.
type cmdashProviderModel struct {
	APIKey  types.String `tfsdk:"api_key"`
	BaseURL types.String `tfsdk:"base_url"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &CmdashProvider{version: version}
	}
}

func (p *CmdashProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "cmdash"
	resp.Version = p.version
}

func (p *CmdashProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages CMDash tenant configuration (frameworks, webhooks, integrations) as code, against the public API v2.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "CMDash public API key (`cmdash_ek_...`, minted from the tenant dashboard: Settings > API Keys, OWNER/ADMIN + Enterprise PUBLIC_API entitlement required to mint one). Falls back to the CMDASH_API_KEY environment variable. Required either way.",
			},
			"base_url": schema.StringAttribute{
				Optional:    true,
				Description: "Base URL of the CMDash API, WITHOUT a trailing /v2 (e.g. \"https://app.cmdash.com/api\"). Falls back to the CMDASH_BASE_URL environment variable, then \"https://app.cmdash.com/api\".",
			},
		},
	}
}

func (p *CmdashProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data cmdashProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiKey := data.APIKey.ValueString()
	if apiKey == "" {
		apiKey = os.Getenv("CMDASH_API_KEY")
	}
	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Missing CMDash API Key",
			"Set the api_key attribute or the CMDASH_API_KEY environment variable. "+
				"Mint a key from the tenant dashboard (Settings > API Keys) — this requires the Enterprise PUBLIC_API entitlement and OWNER/ADMIN role (apps/api/src/public-api/api-key.controller.ts).",
		)
	}

	baseURL := data.BaseURL.ValueString()
	if baseURL == "" {
		baseURL = os.Getenv("CMDASH_BASE_URL")
	}

	c := client.New(baseURL, apiKey)
	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *CmdashProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewWebhookRegistrationResource,
	}
}

func (p *CmdashProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewComplianceFrameworksDataSource,
		NewFrameworkControlsDataSource,
		NewIntegrationConnectionsDataSource,
	}
}

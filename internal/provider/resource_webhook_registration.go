// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/bgovanlu/terraform-provider-cmdash/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &webhookRegistrationResource{}
	_ resource.ResourceWithConfigure   = &webhookRegistrationResource{}
	_ resource.ResourceWithImportState = &webhookRegistrationResource{}
)

func NewWebhookRegistrationResource() resource.Resource {
	return &webhookRegistrationResource{}
}

type webhookRegistrationResource struct {
	client *client.Client
}

// webhookRegistrationModel maps 1:1 to WebhookListItemDto/WebhookCreateResultDto
// (apps/api/src/public-api/dto/openapi-schemas.ts) plus `secret`, which the
// API returns exactly once (create, and again on a rotate — not exposed as
// a resource operation here; rotation is a deliberate follow-up, see
// docs/terraform-provider.md).
type webhookRegistrationModel struct {
	ID        types.String `tfsdk:"id"`
	URL       types.String `tfsdk:"url"`
	Events    types.List   `tfsdk:"events"`
	IsActive  types.Bool   `tfsdk:"is_active"`
	CreatedAt types.String `tfsdk:"created_at"`
	Secret    types.String `tfsdk:"secret"`
}

func (r *webhookRegistrationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook_registration"
}

func (r *webhookRegistrationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A tenant webhook registration (POST/PUT/DELETE /v2/webhooks — apps/api/src/public-api/v2/public-webhooks-v2.controller.ts). Full CRUD; the only one of the roadmap item's six requested resources that public API v2 actually supports end to end.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Webhook registration UUID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"url": schema.StringAttribute{
				Required:    true,
				Description: "Delivery endpoint URL (1-2048 chars). Validated server-side for SSRF-safety (common/ssrf.ts) — a private/loopback/link-local target is rejected with a 400, which surfaces as an apply error, not a silent skip.",
			},
			"events": schema.ListAttribute{
				Required:    true,
				ElementType: types.StringType,
				Description: "Event names to subscribe to (1-50 entries). Must be one of VALID_WEBHOOK_EVENTS (webhook/webhook.service.ts); an unknown event name is rejected server-side with a 400.",
			},
			"is_active": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether deliveries are enabled. Defaults to true server-side on create (Computed since the create request has no isActive field — see CreateWebhookRequestSchema).",
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				Description:   "RFC3339 creation timestamp.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"secret": schema.StringAttribute{
				Computed:      true,
				Sensitive:     true,
				Description:   "HMAC signing secret, returned in PLAINTEXT exactly once at creation. Terraform stores it in state (protect your state file / use a remote backend with encryption). There is no GET that returns it again: if it drops out of state (e.g. a corrupted/hand-edited state file) Terraform CANNOT recover it — only `terraform apply -replace` (destroy + recreate, which also invalidates the old secret) gets you a new one. Rotation (POST :id/rotate-secret) is NOT wired to this attribute; see docs/terraform-provider.md.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *webhookRegistrationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("Expected *client.Client, got: %T", req.ProviderData))
		return
	}
	r.client = c
}

func (r *webhookRegistrationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan webhookRegistrationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var events []string
	resp.Diagnostics.Append(plan.Events.ElementsAs(ctx, &events, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	idemKey := client.NewIdempotencyKey("webhook-create")
	result, err := r.client.CreateWebhook(ctx, idemKey, plan.URL.ValueString(), events)
	if err != nil {
		resp.Diagnostics.AddError("Error creating webhook registration", err.Error())
		return
	}

	// The create response has no isActive field to omit — a fresh
	// registration is active by construction — so this is not itself
	// evidence of a server default; keep the schema Computed anyway since
	// WebhookService.create's contract isn't pinned by this provider.
	plan.ID = types.StringValue(result.ID)
	plan.URL = types.StringValue(result.URL)
	eventsList, diags := types.ListValueFrom(ctx, types.StringType, result.Events)
	resp.Diagnostics.Append(diags...)
	plan.Events = eventsList
	plan.IsActive = types.BoolValue(result.IsActive)
	plan.CreatedAt = types.StringValue(result.CreatedAt)
	plan.Secret = types.StringValue(result.Secret)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *webhookRegistrationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state webhookRegistrationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	wh, err := r.client.GetWebhook(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading webhook registration", err.Error())
		return
	}

	state.URL = types.StringValue(wh.URL)
	eventsList, diags := types.ListValueFrom(ctx, types.StringType, wh.Events)
	resp.Diagnostics.Append(diags...)
	state.Events = eventsList
	state.IsActive = types.BoolValue(wh.IsActive)
	state.CreatedAt = types.StringValue(wh.CreatedAt)
	// state.Secret is left untouched: the list/read path never returns it
	// (see GetWebhook's doc comment) — only Create/Import can populate it.

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *webhookRegistrationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state webhookRegistrationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var events []string
	resp.Diagnostics.Append(plan.Events.ElementsAs(ctx, &events, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	url := plan.URL.ValueString()
	isActive := plan.IsActive.ValueBool()
	idemKey := client.NewIdempotencyKey("webhook-update-" + state.ID.ValueString())
	updated, err := r.client.UpdateWebhook(ctx, idemKey, state.ID.ValueString(), &url, &events, &isActive)
	if err != nil {
		resp.Diagnostics.AddError("Error updating webhook registration", err.Error())
		return
	}

	plan.URL = types.StringValue(updated.URL)
	eventsList, diags := types.ListValueFrom(ctx, types.StringType, updated.Events)
	resp.Diagnostics.Append(diags...)
	plan.Events = eventsList
	plan.IsActive = types.BoolValue(updated.IsActive)
	plan.CreatedAt = state.CreatedAt
	plan.Secret = state.Secret // never returned by update; carry forward

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *webhookRegistrationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state webhookRegistrationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	idemKey := client.NewIdempotencyKey("webhook-delete-" + state.ID.ValueString())
	if err := r.client.DeleteWebhook(ctx, idemKey, state.ID.ValueString()); err != nil {
		if !client.IsNotFound(err) {
			resp.Diagnostics.AddError("Error deleting webhook registration", err.Error())
		}
	}
}

// ImportState imports by webhook UUID: `terraform import cmdash_webhook_registration.example <uuid>`.
// The imported secret is left unset (types.StringNull would show a diff
// forever, since the API never returns it again — see the schema
// description); Read() runs immediately after import and populates every
// other field, so the only manual step an operator needs is
// `terraform apply -replace` if they also want state to hold the secret
// (which mints a NEW one, invalidating the old).
func (r *webhookRegistrationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

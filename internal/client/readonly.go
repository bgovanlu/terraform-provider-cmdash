// SPDX-License-Identifier: Apache-2.0

package client

import "context"

// This file covers every v2 resource the roadmap item asked for that
// turned out to be READ-ONLY (or entirely absent) on the public API,
// hence data sources rather than resources — see docs/terraform-provider.md
// for the full per-resource rationale table.

// Framework mirrors ComplianceFrameworkDto.
type Framework struct {
	ID            string `json:"id"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Version       string `json:"version"`
	TotalControls int    `json:"totalControls"`
}

// ListFrameworks — GET /v2/compliance/frameworks. Requires `compliance:read`.
// No write route exists anywhere on the public API (v1 or v2) to enable or
// disable a framework for a tenant — verified against public-api.module.ts's
// full controller list and every method in public-compliance*.controller.ts.
// "Enabled frameworks" (the roadmap item's cmdash_framework_scope) is
// therefore a data source only, not a resource: Terraform can read which
// frameworks are active, but cannot change that set through this API today.
func (c *Client) ListFrameworks(ctx context.Context) ([]Framework, error) {
	var out []Framework
	if err := c.Do(ctx, "GET", "/v2/compliance/frameworks", "", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ControlStatus mirrors ControlStatusDto (one row of FrameworkControlsResponseDto.controls).
type ControlStatus struct {
	ControlID string  `json:"controlId"`
	Domain    string  `json:"domain"`
	Title     string  `json:"title"`
	Status    string  `json:"status"`
	Notes     *string `json:"notes"`
}

type frameworkControlsResponse struct {
	Framework struct {
		Slug    string `json:"slug"`
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"framework"`
	Controls []ControlStatus `json:"controls"`
}

// GetFrameworkControls — GET /v2/compliance/frameworks/:slug/controls.
// Requires `compliance:read`. Returns a real 404 for an unknown slug (v2's
// fix over v1's `200 {error}` — docs/developers/public-api-versioning.md).
func (c *Client) GetFrameworkControls(ctx context.Context, slug string) ([]ControlStatus, error) {
	var out frameworkControlsResponse
	if err := c.Do(ctx, "GET", "/v2/compliance/frameworks/"+slug+"/controls", "", nil, &out); err != nil {
		return nil, err
	}
	return out.Controls, nil
}

// IntegrationConnection mirrors IntegrationConnectionDto.
type IntegrationConnection struct {
	ID             string  `json:"id"`
	Provider       string  `json:"provider"`
	Status         string  `json:"status"`
	LastSyncAt     *string `json:"lastSyncAt"`
	LastSyncStatus *string `json:"lastSyncStatus"`
}

// ListIntegrationConnections — GET /v2/integrations. Requires
// `integrations:read`. There is no POST (create) or DELETE route — a
// connection can only be created/removed from the authenticated dashboard
// (apps/api/src/integration/integration.controller.ts, JWT-only), never
// through the public API. cmdash_integration_connection is therefore a
// data source only; the roadmap's "custom connectors / connector
// templates" framing does not exist as public-API-manageable state at all
// today (see docs/terraform-provider.md).
func (c *Client) ListIntegrationConnections(ctx context.Context) ([]IntegrationConnection, error) {
	var out []IntegrationConnection
	if err := c.Do(ctx, "GET", "/v2/integrations", "", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
